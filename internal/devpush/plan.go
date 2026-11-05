package devpush

import (
	"context"

	"github.com/cloudogu/cldk/internal/dogu"
	"github.com/cloudogu/cldk/internal/kube"
	"github.com/cloudogu/cldk/internal/oci"
)

// Plan describes what a dev push would build, push and apply, without doing it.
type Plan struct {
	// Version is the dev version of this push, e.g. "3.96.4-3-dev.20261103143012".
	Version string
	// Images are the images that would be built and pushed, with their push references, ordered by name.
	Images []PushedImage
	// ReplacedImages are the images that would be written into the chart with a given reference instead of being
	// built, ordered by name.
	ReplacedImages []ReplacedImage
	// SkippedImages are the names of the images that would be neither built nor written into the chart, ordered by
	// name.
	SkippedImages []string
	// ChartReference is the full reference the chart would be pushed to without scheme.
	ChartReference string
	// DevChart is the dev chart the dogu would be applied with, see dogu.Object for the applied dogu resource.
	DevChart dogu.DevChart
	// Cluster is the cluster and Kubernetes namespace the dogu would be applied in.
	Cluster kube.Info
}

// devPushPlan is a dev push with everything needed to run it.
type devPushPlan struct {
	version     string
	images      imagePlan
	builder     Builder
	doguClient  DoguClient
	cluster     kube.Info
	chartPusher ChartPusher
	// archive is the packed chart with the dev version and the rewritten images.
	archive  []byte
	chartRef string
	devChart dogu.DevChart
}

// newDevPushPlan loads the project and its chart, checks the builder, connects to the cluster, rewrites and lints the
// chart, without building, pushing or applying anything.
func (p *Pusher) newDevPushPlan(ctx context.Context, opts Options) (*devPushPlan, error) {
	project, target, err := loadProject(opts)
	if err != nil {
		return nil, err
	}

	c, version, err := p.loadChart(opts.Dir, project)
	if err != nil {
		return nil, err
	}

	clusterTarget := oci.Target{Registry: opts.ClusterRegistry, Namespace: target.Namespace}
	images := planImages(opts, project, target, clusterTarget, version)

	builder, err := p.createBuilder(ctx, opts.Builder, images.toBuild())
	if err != nil {
		return nil, err
	}

	doguClient, cluster, err := p.connectCluster(opts.Kube)
	if err != nil {
		return nil, err
	}

	archive, err := prepareChart(c, version, images.images)
	if err != nil {
		return nil, err
	}

	chartPusher, err := p.newChartPusher(target)
	if err != nil {
		return nil, err
	}

	return &devPushPlan{
		version:     version,
		images:      images,
		builder:     builder,
		doguClient:  doguClient,
		cluster:     cluster,
		chartPusher: chartPusher,
		archive:     archive,
		chartRef:    target.ChartRepository(c.Name()) + ":" + version,
		devChart: dogu.DevChart{
			Name:      c.Name(),
			Namespace: target.Namespace,
			Version:   version,
			URL:       "oci://" + clusterTarget.ChartRepository(c.Name()),
			Insecure:  opts.PlainHTTP || opts.Insecure,
		},
	}, nil
}

// summary returns the plan as seen from outside.
func (dp *devPushPlan) summary() Plan {
	toBuild := dp.images.toBuild()
	images := make([]PushedImage, 0, len(toBuild))

	for _, img := range toBuild {
		images = append(images, PushedImage{Name: img.name, Reference: img.request.Tag})
	}

	return Plan{
		Version:        dp.version,
		Images:         images,
		ReplacedImages: dp.images.replaced(),
		SkippedImages:  dp.images.skipped,
		ChartReference: dp.chartRef,
		DevChart:       dp.devChart,
		Cluster:        dp.cluster,
	}
}
