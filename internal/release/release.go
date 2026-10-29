// Package release publishes dogu v3 helm charts.
package release

import (
	"context"
	"fmt"
	"log/slog"
	"os"
)

// Target is the registry namespace a release is published to, including how to access the registry.
type Target struct {
	// Registry is the registry host, e.g. "registry.cloudogu.com".
	Registry string
	// Namespace is the namespace inside the registry, e.g. "official".
	Namespace string
	// User and Password authenticate against the registry. They are only kept in memory.
	// If both are empty, the stored credentials of helm and docker are used.
	User     string
	Password string
	// PlainHTTP allows unencrypted HTTP connections to the registry.
	PlainHTTP bool
	// InsecureSkipTLSVerify allows TLS connections without certificate verification, e.g. for self-signed certificates.
	InsecureSkipTLSVerify bool
}

// Result describes what has been released.
type Result struct {
	// ChartReference is the full reference of the released chart without scheme, e.g.
	// "registry.cloudogu.com/official/dogu/v3/charts/nexus:3.96.4-3".
	ChartReference string
	// ChartDigest is the manifest digest of the released chart.
	ChartDigest string
}

// Releaser publishes dogu releases.
type Releaser struct{}

// NewReleaser creates a Releaser.
func NewReleaser() *Releaser {
	return &Releaser{}
}

// Release publishes the packed chart archive (.tgz) at chartPath to the target.
func (r *Releaser) Release(_ context.Context, chartPath string, target Target) (Result, error) {
	archive, err := os.ReadFile(chartPath)
	if err != nil {
		return Result{}, fmt.Errorf("failed to read chart: %w", err)
	}

	meta, err := loadChartMetadata(archive)
	if err != nil {
		return Result{}, fmt.Errorf("%s is not a valid chart archive: %w", chartPath, err)
	}

	client, err := newChartClient(target)
	if err != nil {
		return Result{}, err
	}

	ref := target.chartRepository(meta.Name) + ":" + meta.Version
	slog.Debug("pushing chart", "target", ref)

	pushed, err := client.Push(archive, ref)
	if err != nil {
		return Result{}, fmt.Errorf("failed to push chart to %s: %w", ref, err)
	}

	return Result{ChartReference: ref, ChartDigest: pushed.Manifest.Digest}, nil
}
