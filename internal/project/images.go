package project

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

// BuildOptions are the settings of all builds of a project.
type BuildOptions struct {
	// Builder is the build CLI the images are built and pushed with, e.g. "docker".
	Builder string
	// Platform is the platform the images are built for, e.g. "linux/arm64". If empty, the builder chooses.
	Platform string
	// NoCache builds the images without the cache of the builder.
	NoCache bool
	// BuildArgs are build arguments for all images as "KEY=VALUE" or "KEY", which takes the value from the
	// environment. They override the build arguments of the same key in cldk.yaml.
	BuildArgs []string
}

// Image is an image of the project file to build and push.
type Image struct {
	// Name is the image name in cldk.yaml, e.g. "nexus".
	Name string
	// Key is the explicit key of the image in values.images of the mirror patch template, if any.
	Key string
	// Request builds the image tagged with its push reference.
	Request build.BuildRequest
}

// Reference returns the reference the image is tagged with and pushed to.
func (img Image) Reference() string {
	return img.Request.Tag
}

// Builder builds and pushes images, see build.Builder.
type Builder interface {
	Check(ctx context.Context) error
	Build(ctx context.Context, req build.BuildRequest) error
	Push(ctx context.Context, ref string) error
}

// PlanBuilds returns the images of the project file to build, ordered by name, without the images in excluded.
// Each image is tagged with its repository in the target and its tag in tags by image name.
func PlanBuilds(dir string, file cldkfile.ProjectFile, target oci.Target, tags map[string]string, excluded []string, opts BuildOptions) []Image {
	images := make([]Image, 0, len(file.Images))

	for _, imageName := range slices.Sorted(maps.Keys(file.Images)) {
		if slices.Contains(excluded, imageName) {
			continue
		}

		img := file.Images[imageName]

		images = append(images, Image{
			Name: imageName,
			Key:  img.Key,
			Request: build.BuildRequest{
				Context:    filepath.Join(dir, img.Context),
				Dockerfile: filepath.Join(dir, img.Dockerfile),
				Tag:        target.ImageRepository(imageName) + ":" + tags[imageName],
				Target:     img.Target,
				BuildArgs:  mergeBuildArgs(img.BuildArgs, opts.BuildArgs),
				Platform:   opts.Platform,
				NoCache:    opts.NoCache,
			},
		})
	}

	return images
}

// mergeBuildArgs merges the build arguments of the project file with the given ones, which win on the same key.
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

// CheckBuilder creates the builder for the build CLI with newBuilder and checks that the build CLI can be found.
func CheckBuilder(ctx context.Context, newBuilder func(binary string) Builder, binary string, images []Image) (Builder, error) {
	if len(images) == 0 {
		return nil, nil
	}

	builder := newBuilder(binary)

	if err := builder.Check(ctx); err != nil {
		return nil, err
	}

	return builder, nil
}

// BuildImages builds the images one after another. The first failure aborts.
func BuildImages(ctx context.Context, builder Builder, images []Image) error {
	for _, img := range images {
		slog.Info("building image", "name", img.Name, "tag", img.Reference())

		if err := builder.Build(ctx, img.Request); err != nil {
			return fmt.Errorf("failed to build image %s: %w", img.Name, err)
		}
	}

	return nil
}

// PushImages pushes the built images one after another. The first failure aborts.
func PushImages(ctx context.Context, builder Builder, images []Image) ([]Image, error) {
	pushed := make([]Image, 0, len(images))

	for _, img := range images {
		slog.Info("pushing image", "name", img.Name, "target", img.Reference())

		if err := builder.Push(ctx, img.Reference()); err != nil {
			return pushed, fmt.Errorf("failed to push image %s: %w", img.Name, err)
		}

		pushed = append(pushed, img)
	}

	return pushed, nil
}
