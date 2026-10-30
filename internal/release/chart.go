package release

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/registry"
	"oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry/remote/retry"
)

// loadChartMetadata reads the Chart.yaml of a packed chart archive.
func loadChartMetadata(archive []byte) (*chart.Metadata, error) {
	c, err := loader.LoadArchive(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("failed to load chart archive: %w", err)
	}

	return c.Metadata, nil
}

// newChartClient creates a helm registry client for the registry of the target.
// Without the credentials of the target it uses the stored credentials of helm and docker (e.g. from "docker login"), but never writes any.
func newChartClient(target Target) (*registry.Client, error) {
	opts := []registry.ClientOption{
		registry.ClientOptHTTPClient(&http.Client{Transport: retry.NewTransport(newTransport(target))}),
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

// checkChartTarget returns the status the chart gets in the target.
// An already released chart version is a conflict unless force allows to overwrite it.
// The digests are not compared, because rewriting and packing the chart changes its digest.
func checkChartTarget(client *registry.Client, ref string, force bool) (Status, error) {
	slog.Debug("checking chart target", "target", ref)

	existing, err := client.Resolve(ref)
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
