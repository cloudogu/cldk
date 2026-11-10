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
	// "localhost:5001/official/dogu/v3/images/nexus:1.2.3-dev.20261103143012".
	Reference string
}

// ReplacedImage is an image written into the chart with a given reference instead of being built.
type ReplacedImage struct {
	// Name is the image name in cldk.yaml, e.g. "sidecar".
	Name string
	// Reference is the reference written into the chart, e.g. "registry.example.com/foo:1.0".
	Reference string
}

// plannedImage is an image to write into the chart, either built and pushed or replaced by a given reference.
type plannedImage struct {
	// name is the image name in cldk.yaml.
	name string
	// key is the explicit key of the image in values.images of the mirror patch template, if any.
	key string
	// replaced tells that the image is not built, but written into the chart with clusterRef as given.
	replaced bool
	// request builds the image tagged with its push reference; unused for a replaced image.
	request build.BuildRequest
	// clusterRef is the reference written into the chart: the push reference on the cluster address for a built
	// image, the given reference for a replaced image.
	clusterRef string
}

// imagePlan is what happens to the images of the project file.
type imagePlan struct {
	// images are the images to write into the chart, ordered by name.
	images []plannedImage
	// skipped are the names of the images that are neither built nor written into the chart, ordered by name.
	skipped []string
}

// toBuild returns the images to build and push, ordered by name.
func (p imagePlan) toBuild() []plannedImage {
	var toBuild []plannedImage

	for _, img := range p.images {
		if !img.replaced {
			toBuild = append(toBuild, img)
		}
	}

	return toBuild
}

// replaced returns the images replaced by a given reference, ordered by name.
func (p imagePlan) replaced() []ReplacedImage {
	var replaced []ReplacedImage

	for _, img := range p.images {
		if img.replaced {
			replaced = append(replaced, ReplacedImage{Name: img.name, Reference: img.clusterRef})
		}
	}

	return replaced
}

// checkImageNames checks that the images to replace and to skip are images of the project file.
func checkImageNames(opts Options, project cldkfile.ProjectFile) error {
	known := strings.Join(slices.Sorted(maps.Keys(project.Images)), ", ")

	for _, imageName := range slices.Sorted(maps.Keys(opts.ReplaceImages)) {
		if _, ok := project.Images[imageName]; !ok {
			return fmt.Errorf("unknown image %q of --replace-image, cldk.yaml has the images %s", imageName, known)
		}
	}

	for _, imageName := range opts.SkipImages {
		if _, ok := project.Images[imageName]; !ok {
			return fmt.Errorf("unknown image %q of --skip-image, cldk.yaml has the images %s", imageName, known)
		}
	}

	return nil
}

// planImages returns what happens to the images of the project file, ordered by name:
//   - skipped images are left out
//   - replaced images keep their given reference
//   - other images are built
func planImages(opts Options, project cldkfile.ProjectFile, target, clusterTarget oci.Target, version string) imagePlan {
	plan := imagePlan{images: make([]plannedImage, 0, len(project.Images))}

	for _, imageName := range slices.Sorted(maps.Keys(project.Images)) {
		img := project.Images[imageName]

		if slices.Contains(opts.SkipImages, imageName) {
			slog.Info("skipping image", "name", imageName)
			plan.skipped = append(plan.skipped, imageName)

			continue
		}

		if ref, ok := opts.ReplaceImages[imageName]; ok {
			slog.Info("replacing image", "name", imageName, "reference", ref)
			plan.images = append(plan.images, plannedImage{name: imageName, key: img.Key, replaced: true, clusterRef: ref})

			continue
		}

		plan.images = append(plan.images, plannedImage{
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

	return plan
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
// Without images to build, no builder is needed and nil is returned.
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
