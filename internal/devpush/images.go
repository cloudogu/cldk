package devpush

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cloudogu/cldk/internal/build"
	"github.com/cloudogu/cldk/internal/cldkfile"
	"github.com/cloudogu/cldk/internal/oci"
)

// PushedImage is a built and pushed image.
type PushedImage struct {
	// Name is the image name in cldk.yaml, e.g. "nexus".
	Name string
	// Reference is the reference the image has been pushed to, e.g.
	// "localhost:5001/official/dogu/v3/images/nexus:3.96.4-3-dev.20261103143012".
	Reference string
}

// plannedImage is an image to build, push and write into the chart.
type plannedImage struct {
	// name is the image name in cldk.yaml.
	name string
	// key is the explicit key of the image in values.images of the mirror patch template, if any.
	key string
	// request builds the image tagged with its push reference.
	request build.BuildRequest
	// clusterRef is the reference of the image as seen from the cluster, which is written into the chart.
	clusterRef string
}

// planImages returns the images of the project file to build, ordered by name, with their paths resolved against
// the project directory and the build arguments of the options merged into those of the project file.
func planImages(opts Options, project cldkfile.ProjectFile, target, clusterTarget oci.Target, version string) []plannedImage {
	images := make([]plannedImage, 0, len(project.Images))

	for _, imageName := range slices.Sorted(maps.Keys(project.Images)) {
		img := project.Images[imageName]
		images = append(images, plannedImage{
			name: imageName,
			key:  img.Key,
			request: build.BuildRequest{
				Context:    filepath.Join(opts.Dir, img.Context),
				Dockerfile: filepath.Join(opts.Dir, img.Dockerfile),
				Tag:        target.ImageRepository(imageName) + ":" + version,
				Target:     img.Target,
				BuildArgs:  mergeBuildArgs(img.BuildArgs, opts.BuildArgs),
				Platform:   opts.Platform,
			},
			clusterRef: clusterTarget.ImageRepository(imageName) + ":" + version,
		})
	}

	return images
}

// mergeBuildArgs returns the static build arguments of the project file, overridden by the build arguments of the
// options of the same key, as "KEY=VALUE" or "KEY", ordered by key.
func mergeBuildArgs(static map[string]string, overrides []string) []string {
	merged := make(map[string]string, len(static)+len(overrides))

	for key, value := range static {
		merged[key] = key + "=" + value
	}

	for _, arg := range overrides {
		key, _, _ := strings.Cut(arg, "=")
		merged[key] = arg
	}

	if len(merged) == 0 {
		return nil
	}

	args := make([]string, 0, len(merged))

	for _, key := range slices.Sorted(maps.Keys(merged)) {
		args = append(args, merged[key])
	}

	return args
}

// createBuilder creates the builder and checks that its build CLI can be found.
// Without images, no builder is needed and nil is returned.
func (p *Pusher) createBuilder(ctx context.Context, binary string, images []plannedImage) (Builder, error) {
	if len(images) == 0 {
		return nil, nil
	}

	builder := p.newBuilder(binary)

	if err := builder.Check(ctx); err != nil {
		return nil, err
	}

	return builder, nil
}

// buildImages builds the images one after another and pushes each right after its build. The first failure aborts.
func buildImages(ctx context.Context, builder Builder, images []plannedImage) ([]PushedImage, error) {
	pushed := make([]PushedImage, 0, len(images))

	for _, img := range images {
		ref := img.request.Tag
		slog.Info("building image", "name", img.name, "tag", ref)

		if err := builder.Build(ctx, img.request); err != nil {
			return nil, fmt.Errorf("failed to build image %s: %w", img.name, err)
		}

		slog.Info("pushing image", "name", img.name, "target", ref)

		if err := builder.Push(ctx, ref); err != nil {
			return nil, fmt.Errorf("failed to push image %s: %w", img.name, err)
		}

		pushed = append(pushed, PushedImage{Name: img.name, Reference: ref})
	}

	return pushed, nil
}
