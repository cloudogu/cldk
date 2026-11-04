// Package chart loads, rewrites and packs dogu v3 helm charts.
package chart

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	"helm.sh/helm/v4/pkg/action"
	ci "helm.sh/helm/v4/pkg/chart"
	helmchart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/chart/v2/lint/support"
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

// LoadDir loads the chart in the directory like "helm package" does, without the files ignored by .helmignore.
func LoadDir(dir string) (*helmchart.Chart, error) {
	c, err := loader.LoadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to load chart %s: %w", dir, err)
	}

	if deps := c.Metadata.Dependencies; len(deps) > 0 {
		reqs := make([]ci.Dependency, 0, len(deps))
		for _, dep := range deps {
			reqs = append(reqs, dep)
		}

		if err = action.CheckDependencies(c, reqs); err != nil {
			return nil, fmt.Errorf("invalid dependencies of chart %s: %w; run \"helm dependency build %s\"", dir, err, dir)
		}
	}

	return c, nil
}

// Pack packs the chart like "helm package" does and returns the archive.
func Pack(c *helmchart.Chart) ([]byte, error) {
	var archive []byte

	err := withArchiveFile(c, func(path string) error {
		var err error
		archive, err = os.ReadFile(path)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("failed to pack chart: %w", err)
	}

	return archive, nil
}

// Lint checks the chart like "helm lint" does, including values.schema.json. It fails on errors and returns the
// warnings.
func Lint(c *helmchart.Chart) (warnings []string, err error) {
	var result *action.LintResult

	err = withArchiveFile(c, func(path string) error {
		result = action.NewLint().Run([]string{path}, map[string]any{})

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to lint chart: %w", err)
	}

	for _, msg := range result.Messages {
		if msg.Severity == support.WarningSev {
			warnings = append(warnings, msg.Error())
		}
	}

	if len(result.Errors) > 0 {
		return warnings, fmt.Errorf("chart %s has lint errors: %w", c.Name(), errors.Join(result.Errors...))
	}

	return warnings, nil
}

// withArchiveFile packs the chart into a temporary archive file outside the working directory and calls fn with its
// path. The file is removed afterwards.
func withArchiveFile(c *helmchart.Chart, fn func(path string) error) (err error) {
	dir, err := os.MkdirTemp("", "cldk-chart-")
	if err != nil {
		return err
	}

	defer func() {
		err = errors.Join(err, os.RemoveAll(dir))
	}()

	path, err := chartutil.Save(c, dir)
	if err != nil {
		return err
	}

	return fn(path)
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
