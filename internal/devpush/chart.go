package devpush

import (
	"fmt"
	"time"

	"github.com/Masterminds/semver/v3"
	helmchart "helm.sh/helm/v4/pkg/chart/v2"

	"github.com/cloudogu/cldk/internal/chart"
	"github.com/cloudogu/cldk/internal/cldkfile"
	"github.com/cloudogu/cldk/internal/project"
)

// devTimestampLayout is the UTC timestamp of a dev version, e.g. "20261103143012".
const devTimestampLayout = "20060102150405"

// loadChart loads the chart of the project and returns it with a new dev version from its version.
func (p *Pusher) loadChart(dir string, file cldkfile.ProjectFile) (*helmchart.Chart, string, error) {
	c, err := project.LoadChart(dir, file)
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
func prepareChart(c *helmchart.Chart, version string, images imagePlan, values map[string]any) ([]byte, error) {
	chart.SetVersion(c, version)

	if err := rewriteImages(c, images.chartRefs, images.keys); err != nil {
		return nil, err
	}

	if err := project.Lint(c, values); err != nil {
		return nil, err
	}

	return chart.Pack(c)
}

// rewriteImages points the images of the chart to the references by image name. Without images, the chart is not
// rewritten and needs no mirror patch template.
func rewriteImages(c *helmchart.Chart, refs, keys map[string]string) error {
	if len(refs) == 0 {
		return nil
	}

	return project.RewriteImages(c, refs, keys)
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
