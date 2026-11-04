// Package devpush pushes a dogu v3 under development from its project directory to a dev registry.
package devpush

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/Masterminds/semver/v3"

	"github.com/cloudogu/cldk/internal/chart"
	"github.com/cloudogu/cldk/internal/cldkfile"
	"github.com/cloudogu/cldk/internal/oci"
)

// devTimestampLayout is the UTC timestamp of a dev version, e.g. "20261103143012".
const devTimestampLayout = "20060102150405"

// Options are the settings of a dev push, fully resolved from flags and environment.
type Options struct {
	// Dir is the project directory with cldk.yaml.
	Dir string
	// Registry is the address of the dev registry to push to, as host[:port], e.g. "localhost:5001".
	Registry string
	// ClusterRegistry is the address of the dev registry as seen from the cluster, as host[:port].
	ClusterRegistry string
	// Namespace is the dogu namespace in the registry. If empty, the namespace of cldk.yaml is used.
	Namespace string
	// PlainHTTP allows unencrypted HTTP connections to the dev registry.
	PlainHTTP bool
	// Insecure allows TLS connections to the dev registry without certificate verification.
	Insecure bool
}

// Result describes what has been pushed.
type Result struct {
	// Version is the dev version of this push, e.g. "3.96.4-3-dev.20261103143012".
	Version string
	// ChartReference is the full reference of the pushed chart without scheme, e.g.
	// "localhost:5001/official/dogu/v3/charts/nexus:3.96.4-3-dev.20261103143012".
	ChartReference string
	// ChartDigest is the manifest digest of the pushed chart.
	ChartDigest string
}

// ChartPusher pushes a chart archive to a registry and returns the digest of its manifest, see oci.Client.
type ChartPusher interface {
	PushChart(ctx context.Context, archive []byte, ref string) (digest string, err error)
}

// Pusher pushes dogus under development.
type Pusher struct {
	// newChartPusher creates the client for the dev registry.
	newChartPusher func(target oci.Target) (ChartPusher, error)
	// now is the clock the dev version is taken from.
	now func() time.Time
}

// NewPusher creates a Pusher that pushes to real registries.
func NewPusher() *Pusher {
	return &Pusher{
		newChartPusher: func(target oci.Target) (ChartPusher, error) {
			return oci.NewClient(target)
		},
		now: time.Now,
	}
}

// Push loads the chart of the project directory, sets a new dev version in a copy of it, lints it and pushes it to the dev registry.
func (p *Pusher) Push(ctx context.Context, opts Options) (Result, error) {
	slog.Info("loading project", "dir", opts.Dir)

	project, err := cldkfile.Load(opts.Dir)
	if err != nil {
		return Result{}, err
	}

	if len(project.Images) > 0 {
		return Result{}, fmt.Errorf("images in %s are not supported yet, remove them to push only the chart", cldkfile.FileName)
	}

	target := oci.Target{
		Registry:              opts.Registry,
		Namespace:             opts.Namespace,
		PlainHTTP:             opts.PlainHTTP,
		InsecureSkipTLSVerify: opts.Insecure,
	}

	if target.Namespace == "" {
		target.Namespace = project.Namespace
		slog.Debug("using the namespace of the project file", "namespace", target.Namespace)
	}

	chartDir := filepath.Join(opts.Dir, project.Chart)
	slog.Info("loading chart", "dir", chartDir)

	c, err := chart.LoadDir(chartDir)
	if err != nil {
		return Result{}, err
	}

	version, err := devVersion(c.Metadata.Version, c.Name(), p.now())
	if err != nil {
		return Result{}, err
	}

	slog.Info("linting chart", "name", c.Name(), "version", version)
	chart.SetVersion(c, version)

	warnings, err := chart.Lint(c)
	logLintWarnings(warnings)

	if err != nil {
		return Result{}, err
	}

	archive, err := chart.Pack(c)
	if err != nil {
		return Result{}, err
	}

	client, err := p.newChartPusher(target)
	if err != nil {
		return Result{}, err
	}

	ref := target.ChartRepository(c.Name()) + ":" + version
	slog.Info("pushing chart", "target", ref)

	digest, err := client.PushChart(ctx, archive, ref)
	if err != nil {
		return Result{}, err
	}

	return Result{Version: version, ChartReference: ref, ChartDigest: digest}, nil
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
