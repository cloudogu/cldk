package release

import (
	"bytes"
	"fmt"
	"net/http"

	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/registry"
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
