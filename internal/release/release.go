// Package release publishes dogu v3 helm charts.
package release

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
)

// Options are the settings of a release besides the target.
type Options struct {
	// Images are the images to release together with the chart, each given as "<source>" or "<source>=<target name>:<tag>",
	// e.g. "ci-registry.example.com/nexus:3.96.4-3" or "localhost:5001/local-dev/nexus/main:3.96.4-3=nexus:3.96.4-3".
	// Each image is copied to the image repository of the target with the given name and tag,
	// by default the last path segment of its source repository and its source tag.
	Images []string
	// ImageKeys assign images explicitly to entries of values.images in the mirror patch template of the chart, each
	// given as "<target name>=<key>", e.g. "nexus=nexus".
	// An image without an explicit key is assigned to the entry whose repository has its target name.
	ImageKeys []string
	// Force overwrites image tags with another digest and an already released chart version in the target.
	// Without it, both are conflicts that abort the release before anything is pushed.
	Force bool
}

// Status is the state of an image or chart in the target.
type Status int

const (
	// StatusPushed means the target did not exist, so that it is pushed.
	StatusPushed Status = iota
	// StatusSkipped means the target already has the image with the same digest, so that it is not pushed again.
	StatusSkipped
	// StatusOverwritten means the target already exists with other content and is overwritten (Options.Force).
	StatusOverwritten
	// StatusConflict means the target already exists with other content, so that it cannot be released without
	// Options.Force. Only a Plan has it.
	StatusConflict
)

// Plan describes what a release would push, without pushing anything.
type Plan struct {
	// Images are the planned images in the order of Options.Images.
	Images []PlannedImage
	// ChartReference is the full reference of the chart without scheme, e.g.
	// "registry.cloudogu.com/official/dogu/v3/charts/nexus:3.96.4-3".
	ChartReference string
	// ChartStatus is the state of the chart in the target. A chart is never skipped.
	ChartStatus Status
}

// PlannedImage is an image as it would be released.
type PlannedImage struct {
	// Source is the source reference of the image as given in Options.Images, without an explicit target.
	Source string
	// Reference is the full reference of the image in the target, e.g.
	// "registry.cloudogu.com/official/dogu/v3/images/nexus:3.96.4-3".
	Reference string
	// Digest is the digest of the manifest or index of the source.
	Digest string
	// ExistingDigest is the digest the target already has, empty if the target does not exist yet.
	ExistingDigest string
	// Status is the state of the image in the target.
	Status Status
}

// Conflicts returns an error for each image and the chart that cannot be released without Options.Force,
// or nil if there are none.
func (p Plan) Conflicts() error {
	var errs []error

	for _, img := range p.Images {
		if img.Status == StatusConflict {
			errs = append(errs, fmt.Errorf("image %s already exists with digest %s instead of %s of %s, use --force to overwrite it",
				img.Reference, img.ExistingDigest, img.Digest, img.Source))
		}
	}

	if p.ChartStatus == StatusConflict {
		errs = append(errs, fmt.Errorf("chart %s is already released, use --force to overwrite it", p.ChartReference))
	}

	return errors.Join(errs...)
}

// Result describes what has been released.
type Result struct {
	// Images are the released images in the order of Options.Images.
	Images []ReleasedImage
	// ChartReference is the full reference of the released chart without scheme, e.g.
	// "registry.cloudogu.com/official/dogu/v3/charts/nexus:3.96.4-3".
	ChartReference string
	// ChartStatus is the state of the chart in the target. A chart is never skipped.
	ChartStatus Status
	// ChartDigest is the manifest digest of the released chart.
	ChartDigest string
}

// Releaser publishes dogu releases.
type Releaser struct{}

// NewReleaser creates a Releaser.
func NewReleaser() *Releaser {
	return &Releaser{}
}

// Plan computes the release of the packed chart archive (.tgz) at chartPath to the target including all checks of the
// sources and targets, but pushes nothing. Conflicts in the target are part of the plan, see Plan.Conflicts.
func (r *Releaser) Plan(ctx context.Context, chartPath string, target Target, opts Options) (Plan, error) {
	p, err := newReleasePlan(ctx, chartPath, target, opts)
	if err != nil {
		return Plan{}, err
	}

	return p.summary(), nil
}

// Release publishes the packed chart archive (.tgz) at chartPath to the target.
// It pushes nothing if the plan has conflicts. The images are released before the chart, so that a released chart
// never refers to missing images.
func (r *Releaser) Release(ctx context.Context, chartPath string, target Target, opts Options) (Result, error) {
	p, err := newReleasePlan(ctx, chartPath, target, opts)
	if err != nil {
		return Result{}, err
	}

	// All targets are checked before the first push, so that a conflict does not leave a partial release.
	if err = p.summary().Conflicts(); err != nil {
		return Result{}, err
	}

	return p.push(ctx)
}

// releasePlan is a computed release with everything needed to push it.
type releasePlan struct {
	publisher *publisher
	images    []checkedImage
	// archive is the chart archive to push, rewritten to the targets of the images.
	archive     []byte
	chartRef    string
	chartStatus Status
}

// newReleasePlan reads and rewrites the chart, resolves the images and checks all targets without pushing anything.
func newReleasePlan(ctx context.Context, chartPath string, target Target, opts Options) (*releasePlan, error) {
	archive, err := os.ReadFile(chartPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read chart: %w", err)
	}

	meta, err := loadChartMetadata(archive)
	if err != nil {
		return nil, fmt.Errorf("%s is not a valid chart archive: %w", chartPath, err)
	}

	specs, err := parseImages(opts.Images, target)
	if err != nil {
		return nil, err
	}

	keys, err := parseImageKeys(opts.ImageKeys, specs)
	if err != nil {
		return nil, err
	}

	resolved, err := resolveImages(ctx, specs)
	if err != nil {
		return nil, err
	}

	if len(specs) > 0 {
		archive, err = rewriteChartForImages(archive, specs, keys)
		if err != nil {
			return nil, fmt.Errorf("failed to rewrite chart %s: %w", chartPath, err)
		}
	}

	pub, err := newPublisher(target)
	if err != nil {
		return nil, err
	}

	ref := target.chartRepository(meta.Name) + ":" + meta.Version

	images, err := pub.checkImages(ctx, resolved, opts.Force)
	if err != nil {
		return nil, err
	}

	chartStatus, err := pub.checkChart(ref, opts.Force)
	if err != nil {
		return nil, err
	}

	return &releasePlan{
		publisher:   pub,
		images:      images,
		archive:     archive,
		chartRef:    ref,
		chartStatus: chartStatus,
	}, nil
}

// summary returns the plan as seen from outside.
func (p *releasePlan) summary() Plan {
	planned := make([]PlannedImage, 0, len(p.images))

	for _, img := range p.images {
		planned = append(planned, img.planned())
	}

	return Plan{Images: planned, ChartReference: p.chartRef, ChartStatus: p.chartStatus}
}

// push pushes the images in their order and then the chart.
func (p *releasePlan) push(ctx context.Context) (Result, error) {
	result := Result{ChartReference: p.chartRef, ChartStatus: p.chartStatus}

	for _, img := range p.images {
		released, err := p.publisher.pushImage(ctx, img)
		if err != nil {
			return Result{}, err
		}

		result.Images = append(result.Images, released)
	}

	digest, err := p.publisher.pushChart(p.archive, p.chartRef)
	if err != nil {
		return Result{}, err
	}

	result.ChartDigest = digest

	return result, nil
}

// rewriteChartForImages points the chart archive to the target references of the images.
func rewriteChartForImages(archive []byte, images []imageSpec, keys map[string]string) ([]byte, error) {
	targets := make(map[string]string, len(images))

	for _, img := range images {
		targets[img.targetName] = img.target.String()
	}

	rewritten, changes, err := rewriteChart(archive, targets, keys)
	if err != nil {
		return nil, err
	}

	for _, change := range changes {
		slog.Debug("rewriting image of chart", "key", change.Key, "old", change.Old, "new", change.New)
	}

	return rewritten, nil
}
