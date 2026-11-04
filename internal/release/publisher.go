package release

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"helm.sh/helm/v4/pkg/registry"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry/remote/retry"
)

// publisher checks and pushes images and charts in the registry of a target.
// Images and charts share one transport and the same credentials, because they go to the same registry.
type publisher struct {
	// puller and pusher access the images, so that their connections and tokens are reused across all images.
	puller *remote.Puller
	pusher *remote.Pusher
	// chartClient accesses the charts like helm does.
	chartClient *registry.Client
}

// newPublisher creates a publisher for the registry of the target.
// Without the credentials of the target it uses the stored credentials of helm and docker (e.g. from "docker login"),
// but never writes any.
func newPublisher(target Target) (*publisher, error) {
	t := newTransport(target)

	auth := remote.WithAuthFromKeychain(authn.DefaultKeychain)

	if target.User != "" {
		auth = remote.WithAuth(&authn.Basic{Username: target.User, Password: target.Password})
	}

	opts := []remote.Option{remote.WithTransport(t), auth}

	puller, err := remote.NewPuller(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create image client: %w", err)
	}

	pusher, err := remote.NewPusher(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create image client: %w", err)
	}

	chartClient, err := newChartClient(target, t)
	if err != nil {
		return nil, err
	}

	return &publisher{puller: puller, pusher: pusher, chartClient: chartClient}, nil
}

// newChartClient creates a helm registry client for the registry of the target.
func newChartClient(target Target, t http.RoundTripper) (*registry.Client, error) {
	opts := []registry.ClientOption{
		registry.ClientOptHTTPClient(&http.Client{Transport: retry.NewTransport(t)}),
	}

	if target.User != "" {
		opts = append(opts, registry.ClientOptBasicAuth(target.User, target.Password))
	}

	if target.PlainHTTP {
		opts = append(opts, registry.ClientOptPlainHTTP())
	}

	client, err := registry.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create registry client: %w", err)
	}

	return client, nil
}

// newTransport creates the HTTP transport for all connections to the registry of the target.
func newTransport(target Target) *http.Transport {
	t := &http.Transport{Proxy: http.ProxyFromEnvironment}

	if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok {
		t = defaultTransport.Clone()
	}

	if target.InsecureSkipTLSVerify {
		t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicitly requested with --insecure
	}

	return t
}

// checkImages checks the targets of all images and returns the images with their status.
func (p *publisher) checkImages(ctx context.Context, images []resolvedImage, force bool) ([]checkedImage, error) {
	checked := make([]checkedImage, 0, len(images))

	for _, img := range images {
		c, err := p.checkImage(ctx, img, force)
		if err != nil {
			return nil, err
		}

		checked = append(checked, c)
	}

	return checked, nil
}

// checkImage checks whether the target tag of the image already exists and returns the image with its status.
// An existing tag with another digest is a conflict unless force allows to overwrite it.
func (p *publisher) checkImage(ctx context.Context, img resolvedImage, force bool) (checkedImage, error) {
	slog.Debug("checking image target", "target", img.target.String())

	existing, err := p.puller.Head(ctx, img.target)
	if isNotFound(err) {
		return checkedImage{resolvedImage: img, status: StatusPushed}, nil
	}

	if err != nil {
		return checkedImage{}, fmt.Errorf("failed to check image %s: %w", img.target, err)
	}

	checked := checkedImage{resolvedImage: img, existingDigest: existing.Digest.String()}

	switch {
	case existing.Digest == img.desc.Digest:
		checked.status = StatusSkipped
	case !force:
		checked.status = StatusConflict
	default:
		slog.Debug("overwriting image", "target", img.target.String(), "digest", existing.Digest.String())
		checked.status = StatusOverwritten
	}

	return checked, nil
}

// pushImage copies the manifest or index of the image unchanged to its target, unless the target already has it.
func (p *publisher) pushImage(ctx context.Context, img checkedImage) (ReleasedImage, error) {
	released := img.released()

	if released.Status == StatusSkipped {
		slog.Debug("skipping already released image", "source", img.source.String(), "target", img.target.String())

		return released, nil
	}

	slog.Debug("pushing image", "source", img.source.String(), "target", img.target.String())

	if err := p.pusher.Push(ctx, img.target, img.desc); err != nil {
		return ReleasedImage{}, fmt.Errorf("failed to push image %s to %s: %w", img.source, img.target, err)
	}

	return released, nil
}

// checkChart returns the status the chart gets in the target.
// An already released chart version is a conflict unless force allows to overwrite it.
// The digests are not compared, because rewriting and packing the chart changes its digest.
func (p *publisher) checkChart(ref string, force bool) (Status, error) {
	slog.Debug("checking chart target", "target", ref)

	existing, err := p.chartClient.Resolve(ref)
	if errors.Is(err, errdef.ErrNotFound) {
		return StatusPushed, nil
	}

	if err != nil {
		return StatusPushed, fmt.Errorf("failed to check chart %s: %w", ref, err)
	}

	if !force {
		return StatusConflict, nil
	}

	slog.Debug("overwriting chart", "target", ref, "digest", existing.Digest.String())

	return StatusOverwritten, nil
}

// pushChart pushes the chart archive to ref and returns the digest of its manifest.
func (p *publisher) pushChart(archive []byte, ref string) (string, error) {
	slog.Debug("pushing chart", "target", ref)

	pushed, err := p.chartClient.Push(archive, ref)
	if err != nil {
		return "", fmt.Errorf("failed to push chart to %s: %w", ref, err)
	}

	return pushed.Manifest.Digest, nil
}

// isNotFound reports whether err is the response of a registry to a missing manifest.
func isNotFound(err error) bool {
	var transportErr *transport.Error

	return errors.As(err, &transportErr) && transportErr.StatusCode == http.StatusNotFound
}
