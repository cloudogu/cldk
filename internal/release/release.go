// Package release publishes dogu v3 helm charts.
package release

import (
	"context"
	"fmt"
	"log/slog"
	"os"
)

// Options are the settings of a release besides the target.
type Options struct {
	// Images are the references of the images to release together with the chart, e.g.
	// "ci-registry.example.com/nexus:3.96.4-3". Each image is copied to the image repository of the target named after
	// the last path segment of its repository, keeping its tag. The chart is rewritten to refer to the copied images.
	Images []string
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

	images, err := resolveImages(ctx, opts.Images, target)
	if err != nil {
		return Result{}, err
	}

	if len(images) > 0 {
		archive, err = rewriteChartForImages(archive, images)
		if err != nil {
			return Result{}, fmt.Errorf("failed to rewrite chart %s: %w", chartPath, err)
		}
	}

	client, err := newChartClient(target)
	if err != nil {
		return Result{}, err
	}

	var result Result

	for _, img := range images {
		released, err := img.push(ctx, target)
		if err != nil {
			return Result{}, err
		}

		result.Images = append(result.Images, released)
	}

	ref := target.chartRepository(meta.Name) + ":" + meta.Version
	slog.Debug("pushing chart", "target", ref)

	pushed, err := client.Push(archive, ref)
	if err != nil {
		return Result{}, fmt.Errorf("failed to push chart to %s: %w", ref, err)
	}

	result.ChartReference = ref
	result.ChartDigest = pushed.Manifest.Digest

	return result, nil
}

// rewriteChartForImages points the chart archive to the target references of the images.
func rewriteChartForImages(archive []byte, images []*image) ([]byte, error) {
	targets := make(map[string]string, len(images))

	for _, img := range images {
		targets[img.targetName] = img.target.String()
	}

	rewritten, changes, err := rewriteChart(archive, targets)
	if err != nil {
		return nil, err
	}

	for _, change := range changes {
		slog.Debug("rewriting image of chart", "key", change.Key, "old", change.Old, "new", change.New)
	}

	return rewritten, nil
}
