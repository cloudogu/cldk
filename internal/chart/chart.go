// Package chart loads, rewrites and packs dogu v3 helm charts.
package chart

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	helmchart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	chartutil "helm.sh/helm/v4/pkg/chart/v2/util"
)

// LoadArchive loads a packed chart archive (.tgz).
func LoadArchive(archive []byte) (*helmchart.Chart, error) {
	c, err := loader.LoadArchive(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("failed to load chart archive: %w", err)
	}

	return c, nil
}

// SetVersion sets the version of the chart. The appVersion stays as it is.
func SetVersion(c *helmchart.Chart, version string) {
	c.Metadata.Version = version
}

// Pack packs the chart like "helm package" does and returns the archive.
func Pack(c *helmchart.Chart) (archive []byte, err error) {
	dir, err := os.MkdirTemp("", "cldk-chart-")
	if err != nil {
		return nil, fmt.Errorf("failed to pack chart: %w", err)
	}

	defer func() {
		err = errors.Join(err, os.RemoveAll(dir))
	}()

	archivePath, err := chartutil.Save(c, dir)
	if err != nil {
		return nil, fmt.Errorf("failed to pack chart: %w", err)
	}

	archive, err = os.ReadFile(archivePath)
	if err != nil {
		return nil, fmt.Errorf("failed to pack chart: %w", err)
	}

	return archive, nil
}

// ValidateImageRef checks that the image reference is valid and starts with a registry.
// Without a registry, the image would silently be pulled from docker.io, which hardly ever is the intended source.
// Like docker, it takes the first path segment as registry if it contains a "." or ":" or is "localhost".
func ValidateImageRef(ref string) error {
	parsed, err := name.ParseReference(ref)
	if err != nil {
		return fmt.Errorf("invalid image reference %s: %w", ref, err)
	}

	first, _, hasPath := strings.Cut(ref, "/")
	if hasPath && (strings.ContainsAny(first, ".:") || first == "localhost") {
		return nil
	}

	separator := ":"
	if _, ok := parsed.(name.Digest); ok {
		separator = "@"
	}

	return fmt.Errorf("image %s has no registry, give it explicitly, e.g. docker.io/%s%s%s",
		ref, parsed.Context().RepositoryStr(), separator, parsed.Identifier())
}
