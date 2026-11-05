// Package devpush pushes a dogu v3 under development from its project directory to a dev registry.
package devpush

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/cloudogu/cldk/internal/build"
	"github.com/cloudogu/cldk/internal/cldkfile"
	"github.com/cloudogu/cldk/internal/dogu"
	"github.com/cloudogu/cldk/internal/kube"
	"github.com/cloudogu/cldk/internal/oci"
)

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
	// Kube selects the cluster and Kubernetes namespace the dogu is applied in.
	Kube kube.Options
	// Builder is the build CLI the images are built and pushed with, e.g. "docker".
	Builder string
	// Platform is the platform the images are built for, e.g. "linux/arm64". If empty, the builder chooses.
	Platform string
	// BuildArgs are build arguments for all images as "KEY=VALUE" or "KEY", which takes the value from the
	// environment. They override the build arguments of the same key in cldk.yaml.
	BuildArgs []string
	// ReplaceImages are images of cldk.yaml by name that are not built but written into the chart with the given
	// reference, e.g. "registry.example.com/foo:1.0".
	ReplaceImages map[string]string
	// SkipImages are images of cldk.yaml by name that are neither built nor written into the chart.
	SkipImages []string
}

// Result describes what has been pushed.
type Result struct {
	// Version is the dev version of this push, e.g. "3.96.4-3-dev.20261103143012".
	Version string
	// Images are the built and pushed images, ordered by name.
	Images []PushedImage
	// ReplacedImages are the images written into the chart with a given reference instead of being built, ordered by
	// name.
	ReplacedImages []ReplacedImage
	// SkippedImages are the names of the images that have been neither built nor written into the chart, ordered by
	// name.
	SkippedImages []string
	// ChartReference is the full reference of the pushed chart without scheme, e.g.
	// "localhost:5001/official/dogu/v3/charts/nexus:3.96.4-3-dev.20261103143012".
	ChartReference string
	// ChartDigest is the manifest digest of the pushed chart.
	ChartDigest string
	// Dogu is the name of the applied dogu, e.g. "nexus".
	Dogu string
	// Cluster is the cluster and Kubernetes namespace the dogu has been applied in.
	Cluster kube.Info
}

// ChartPusher pushes a chart archive to a registry and returns the digest of its manifest, see oci.Client.
type ChartPusher interface {
	PushChart(ctx context.Context, archive []byte, ref string) (digest string, err error)
}

// DoguClient applies the dogu resource in the cluster, see dogu.Client.
type DoguClient interface {
	Apply(ctx context.Context, chart dogu.DevChart) error
}

// Builder builds and pushes images, see build.Builder.
type Builder interface {
	Check(ctx context.Context) error
	Build(ctx context.Context, req build.BuildRequest) error
	Push(ctx context.Context, ref string) error
}

// Pusher pushes dogus under development.
type Pusher struct {
	// newChartPusher creates the client for the dev registry.
	newChartPusher func(target oci.Target) (ChartPusher, error)
	// newDoguClient connects to the cluster selected by the options.
	newDoguClient func(opts kube.Options) (DoguClient, kube.Info, error)
	// newBuilder creates the builder for the build CLI.
	newBuilder func(binary string) Builder
	// now is the clock the dev version is taken from.
	now func() time.Time
}

// NewPusher creates a Pusher that pushes to real registries.
func NewPusher() *Pusher {
	return &Pusher{
		newChartPusher: func(target oci.Target) (ChartPusher, error) {
			return oci.NewClient(target)
		},
		newDoguClient: func(opts kube.Options) (DoguClient, kube.Info, error) {
			cluster, err := kube.Connect(opts)
			if err != nil {
				return nil, kube.Info{}, err
			}

			return dogu.NewClient(cluster.Dynamic, cluster.Info.Namespace), cluster.Info, nil
		},
		newBuilder: func(binary string) Builder {
			return build.New(binary)
		},
		now: time.Now,
	}
}

// Push builds and pushes the images of the project directory, pushes a copy of its chart with the dev version and
// the built images, and applies the dogu with the dev chart in the cluster.
func (p *Pusher) Push(ctx context.Context, opts Options) (Result, error) {
	project, target, err := loadProject(opts)
	if err != nil {
		return Result{}, err
	}

	c, version, err := p.loadChart(opts.Dir, project)
	if err != nil {
		return Result{}, err
	}

	clusterTarget := oci.Target{Registry: opts.ClusterRegistry, Namespace: target.Namespace}
	images := planImages(opts, project, target, clusterTarget, version)

	builder, err := p.createBuilder(ctx, opts.Builder, images.toBuild())
	if err != nil {
		return Result{}, err
	}

	doguClient, cluster, err := p.connectCluster(opts.Kube)
	if err != nil {
		return Result{}, err
	}

	archive, err := prepareChart(c, version, images.images)
	if err != nil {
		return Result{}, err
	}

	client, err := p.newChartPusher(target)
	if err != nil {
		return Result{}, err
	}

	pushed, err := buildImages(ctx, builder, images.toBuild())
	if err != nil {
		return Result{}, err
	}

	ref := target.ChartRepository(c.Name()) + ":" + version
	slog.Info("pushing chart", "target", ref)

	digest, err := client.PushChart(ctx, archive, ref)
	if err != nil {
		return Result{}, err
	}

	devChart := dogu.DevChart{
		Name:      c.Name(),
		Namespace: target.Namespace,
		Version:   version,
		URL:       "oci://" + clusterTarget.ChartRepository(c.Name()),
		Insecure:  opts.PlainHTTP || opts.Insecure,
	}

	if err := applyDogu(ctx, doguClient, devChart); err != nil {
		return Result{}, fmt.Errorf("pushed chart %s, but %w", ref, err)
	}

	return Result{
		Version:        version,
		Images:         pushed,
		ReplacedImages: images.replaced(),
		SkippedImages:  images.skipped,
		ChartReference: ref,
		ChartDigest:    digest,
		Dogu:           devChart.Name,
		Cluster:        cluster,
	}, nil
}

// loadProject reads the project file and returns it with the target in the dev registry.
func loadProject(opts Options) (cldkfile.ProjectFile, oci.Target, error) {
	slog.Info("loading project", "dir", opts.Dir)

	project, err := cldkfile.Load(opts.Dir)
	if err != nil {
		return cldkfile.ProjectFile{}, oci.Target{}, err
	}

	if err := checkImageNames(opts, project); err != nil {
		return cldkfile.ProjectFile{}, oci.Target{}, err
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

	return project, target, nil
}
