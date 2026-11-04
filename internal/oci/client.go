package oci

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"helm.sh/helm/v4/pkg/registry"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry/remote/retry"
)

// Client checks and pushes images and charts in the registry of a target.
// Images and charts share one transport and the same credentials, because they go to the same registry.
type Client struct {
	// puller and pusher access the images, so that their connections and tokens are reused across all images.
	puller *remote.Puller
	pusher *remote.Pusher
	// chartClient accesses the charts like helm does.
	chartClient *registry.Client
}

// NewClient creates a Client for the registry of the target.
// Without the credentials of the target it uses the stored credentials of helm and docker (e.g. from "docker login"),
// but never writes any.
func NewClient(target Target) (*Client, error) {
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

	return &Client{puller: puller, pusher: pusher, chartClient: chartClient}, nil
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

// ImageExists returns whether an image exists at ref and, if so, the digest of its manifest or index.
func (c *Client) ImageExists(ctx context.Context, ref name.Reference) (digest string, exists bool, err error) {
	desc, err := c.puller.Head(ctx, ref)
	if isNotFound(err) {
		return "", false, nil
	}

	if err != nil {
		return "", false, fmt.Errorf("failed to check image %s: %w", ref, err)
	}

	return desc.Digest.String(), true, nil
}

// PushImage pushes the manifest or index of img unchanged to ref.
func (c *Client) PushImage(ctx context.Context, ref name.Reference, img remote.Taggable) error {
	return c.pusher.Push(ctx, ref, img)
}

// ChartExists returns whether a chart exists at ref and, if so, the digest of its manifest.
// The helm registry client cannot be canceled, so that ctx has no effect yet.
func (c *Client) ChartExists(_ context.Context, ref string) (digest string, exists bool, err error) {
	existing, err := c.chartClient.Resolve(ref)
	if errors.Is(err, errdef.ErrNotFound) {
		return "", false, nil
	}

	if err != nil {
		return "", false, fmt.Errorf("failed to check chart %s: %w", ref, err)
	}

	return existing.Digest.String(), true, nil
}

// PushChart pushes the chart archive to ref and returns the digest of its manifest.
// The helm registry client cannot be canceled, so that ctx has no effect yet.
func (c *Client) PushChart(_ context.Context, archive []byte, ref string) (string, error) {
	slog.Debug("pushing chart", "target", ref)

	pushed, err := c.chartClient.Push(archive, ref)
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
