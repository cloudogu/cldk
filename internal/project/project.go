// Package project reads a dogu v3 project directory and builds its images.
package project

import (
	"log/slog"
	"path/filepath"

	helmchart "helm.sh/helm/v4/pkg/chart/v2"

	"github.com/cloudogu/cldk/internal/chart"
	"github.com/cloudogu/cldk/internal/cldkfile"
)

// LoadFile reads the project file cldk.yaml in the project directory.
func LoadFile(dir string) (cldkfile.ProjectFile, error) {
	slog.Info("loading project", "dir", dir)

	return cldkfile.Load(dir)
}

// LoadChart loads the chart directory of the project file, relative to the project directory.
func LoadChart(dir string, file cldkfile.ProjectFile) (*helmchart.Chart, error) {
	chartDir := filepath.Join(dir, file.Chart)
	slog.Info("loading chart", "dir", chartDir)

	return chart.LoadDir(chartDir)
}

// RewriteImages points the images of the chart in place to the references by image name, see chart.RewriteImages.
func RewriteImages(c *helmchart.Chart, refs, keys map[string]string) error {
	changes, err := chart.RewriteImages(c, refs, keys)
	if err != nil {
		return err
	}

	for _, change := range changes {
		slog.Debug("rewriting image of chart", "key", change.Key, "old", change.Old, "new", change.New)
	}

	return nil
}

// Lint lints the chart with the values like "helm lint --values" does.
func Lint(c *helmchart.Chart, values map[string]any) error {
	slog.Info("linting chart", "name", c.Name(), "version", c.Metadata.Version)

	warnings, err := chart.Lint(c, values)

	for _, warning := range warnings {
		slog.Warn("chart lint", "warning", warning)
	}

	return err
}
