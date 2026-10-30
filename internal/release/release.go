// Package release publishes dogu v3 helm charts.
package release

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"helm.sh/helm/v4/pkg/registry"
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
	// Without it, both abort the release before anything is pushed.
	Force bool
}

// Result describes what has been released.
type Result struct {
	// Images are the released images in the order of Options.Images.
	Images []ReleasedImage
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
// The images of the options are released before the chart, so that a released chart never refers to missing images.
func (r *Releaser) Release(ctx context.Context, chartPath string, target Target, opts Options) (Result, error) {
	archive, err := os.ReadFile(chartPath)
	if err != nil {
		return Result{}, fmt.Errorf("failed to read chart: %w", err)
	}

	meta, err := loadChartMetadata(archive)
	if err != nil {
		return Result{}, fmt.Errorf("%s is not a valid chart archive: %w", chartPath, err)
	}

	images, err := parseImages(opts.Images, target)
	if err != nil {
		return Result{}, err
	}

	keys, err := parseImageKeys(opts.ImageKeys, images)
	if err != nil {
		return Result{}, err
	}

	if err = resolveImages(ctx, images); err != nil {
		return Result{}, err
	}

	if len(images) > 0 {
		archive, err = rewriteChartForImages(archive, images, keys)
		if err != nil {
			return Result{}, fmt.Errorf("failed to rewrite chart %s: %w", chartPath, err)
		}
	}

	client, err := newChartClient(target)
	if err != nil {
		return Result{}, err
	}

	ref := target.chartRepository(meta.Name) + ":" + meta.Version

	// All targets are checked before the first push, so that a conflict does not leave a partial release.
	if err = checkTargets(ctx, images, client, ref, target, opts.Force); err != nil {
		return Result{}, err
	}

	var result Result

	result.Images, err = pushImages(ctx, images, target)
	if err != nil {
		return Result{}, err
	}

	slog.Debug("pushing chart", "target", ref)

	pushed, err := client.Push(archive, ref)
	if err != nil {
		return Result{}, fmt.Errorf("failed to push chart to %s: %w", ref, err)
	}

	result.ChartReference = ref
	result.ChartDigest = pushed.Manifest.Digest

	return result, nil
}

// checkTargets checks the targets of all images and of the chart.
func checkTargets(ctx context.Context, images []*image, client *registry.Client, chartRef string, target Target, force bool) error {
	for _, img := range images {
		if err := img.checkTarget(ctx, target, force); err != nil {
			return err
		}
	}

	return checkChartTarget(client, chartRef, force)
}

// pushImages pushes the images in their order and returns them as released.
func pushImages(ctx context.Context, images []*image, target Target) ([]ReleasedImage, error) {
	var released []ReleasedImage

	for _, img := range images {
		r, err := img.push(ctx, target)
		if err != nil {
			return nil, err
		}

		released = append(released, r)
	}

	return released, nil
}

// rewriteChartForImages points the chart archive to the target references of the images.
func rewriteChartForImages(archive []byte, images []*image, keys map[string]string) ([]byte, error) {
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
