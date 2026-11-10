package devpush

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/Masterminds/semver/v3"
	helmchart "helm.sh/helm/v4/pkg/chart/v2"

	"github.com/cloudogu/cldk/internal/chart"
	"github.com/cloudogu/cldk/internal/cldkfile"
)

// devTimestampLayout is the UTC timestamp of a dev version, e.g. "20261103143012".
const devTimestampLayout = "20060102150405"

// loadChart loads the chart of the project and returns it with a new dev version from its version.
func (p *Pusher) loadChart(dir string, project cldkfile.ProjectFile) (*helmchart.Chart, string, error) {
	chartDir := filepath.Join(dir, project.Chart)
	slog.Info("loading chart", "dir", chartDir)

	c, err := chart.LoadDir(chartDir)
	if err != nil {
		return nil, "", err
	}

	version, err := devVersion(c.Metadata.Version, c.Name(), p.now())
	if err != nil {
		return nil, "", err
	}

	return c, version, nil
}

// prepareChart sets the dev version in the chart, points it to the cluster references of the built images and to the
// references of the replaced images, lints it with the values and returns the packed chart.
func prepareChart(c *helmchart.Chart, version string, images []plannedImage, values map[string]any) ([]byte, error) {
	chart.SetVersion(c, version)

	if err := rewriteImages(c, images); err != nil {
		return nil, err
	}

	slog.Info("linting chart", "name", c.Name(), "version", version)

	warnings, err := chart.Lint(c, values)
	logLintWarnings(warnings)

	if err != nil {
		return nil, err
	}

	return chart.Pack(c)
}

// rewriteImages points the images of the chart to the cluster references of the built images and to the references
// of the replaced images. Without such images, the chart is not rewritten and needs no mirror patch template.
func rewriteImages(c *helmchart.Chart, images []plannedImage) error {
	if len(images) == 0 {
		return nil
	}

	targets := make(map[string]string, len(images))
	keys := map[string]string{}

	for _, img := range images {
		targets[img.name] = img.clusterRef

		if img.key != "" {
			keys[img.name] = img.key
		}
	}

	changes, err := chart.RewriteImages(c, targets, keys)
	if err != nil {
		return err
	}

	for _, change := range changes {
		slog.Debug("rewriting image of chart", "key", change.Key, "old", change.Old, "new", change.New)
	}

	return nil
}

// logLintWarnings logs the warnings of helm lint, which do not abort a dev push.
func logLintWarnings(warnings []string) {
	for _, warning := range warnings {
		slog.Warn("chart lint", "warning", warning)
	}
}

// devVersion returns the dev version "<base>-dev.<UTC timestamp>" of the chart version base.
func devVersion(base, chartName string, now time.Time) (string, error) {
	parsed, err := semver.StrictNewVersion(base)
	if err != nil {
		return "", fmt.Errorf("version %q of chart %s is no valid semantic version: %w", base, chartName, err)
	}

	if parsed.Metadata() != "" {
		return "", fmt.Errorf("version %q of chart %s has build metadata (+%s), which a dev version cannot keep",
			base, chartName, parsed.Metadata())
	}

	return base + "-dev." + now.UTC().Format(devTimestampLayout), nil
}
