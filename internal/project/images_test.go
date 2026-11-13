package project

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudogu/cldk/internal/build"
	"github.com/cloudogu/cldk/internal/cldkfile"
	"github.com/cloudogu/cldk/internal/oci"
)

// twoImages is a project file with two images, one with a custom build and one with the defaults applied by
// cldkfile.Load.
var twoImages = cldkfile.ProjectFile{
	Namespace: "official",
	Images: map[string]cldkfile.Image{
		"sidecar": {
			Context:    "sidecar",
			Dockerfile: "build/Dockerfile.sidecar",
			Key:        "nexusSaManager",
			Target:     "runtime",
			BuildArgs:  map[string]string{"FOO": "bar", "BAZ": "qux"},
		},
		"nexus": {Context: ".", Dockerfile: "Dockerfile"},
	},
}

var testTarget = oci.Target{Registry: "registry.example.com", Namespace: "testing"}

const (
	nexusRef   = "registry.example.com/testing/dogu/v3/images/nexus:3.96.4-1"
	sidecarRef = "registry.example.com/testing/dogu/v3/images/sidecar:1.2.0"
)

var testTags = map[string]string{"nexus": "3.96.4-1", "sidecar": "1.2.0"}

// recordingBuilder returns a mocked builder that records each build and push in steps.
func recordingBuilder(steps *[]string) *MockBuilder {
	return &MockBuilder{
		CheckFunc: func(context.Context) error {
			return nil
		},
		BuildFunc: func(_ context.Context, req build.BuildRequest) error {
			*steps = append(*steps, "build "+req.Tag)

			return nil
		},
		PushFunc: func(_ context.Context, ref string) error {
			*steps = append(*steps, "push "+ref)

			return nil
		},
	}
}

func TestPlanBuilds(t *testing.T) {
	t.Run("plans each image by name with its tag, the paths relative to the project directory and the merged build arguments", func(t *testing.T) {
		images := PlanBuilds("/work/nexus", twoImages, testTarget, testTags, nil, BuildOptions{
			Platform: "linux/arm64", NoCache: true, BuildArgs: []string{"FOO=override", "HTTP_PROXY"},
		})

		assert.Equal(t, []Image{
			{
				Name: "nexus",
				Request: build.BuildRequest{
					Context:    "/work/nexus",
					Dockerfile: filepath.Join("/work/nexus", "Dockerfile"),
					Tag:        nexusRef,
					BuildArgs:  []string{"FOO=override", "HTTP_PROXY"},
					Platform:   "linux/arm64",
					NoCache:    true,
				},
			},
			{
				Name: "sidecar",
				Key:  "nexusSaManager",
				Request: build.BuildRequest{
					Context:    filepath.Join("/work/nexus", "sidecar"),
					Dockerfile: filepath.Join("/work/nexus", "build", "Dockerfile.sidecar"),
					Tag:        sidecarRef,
					Target:     "runtime",
					BuildArgs:  []string{"BAZ=qux", "FOO=override", "HTTP_PROXY"},
					Platform:   "linux/arm64",
					NoCache:    true,
				},
			},
		}, images)
		assert.Equal(t, nexusRef, images[0].Reference())
	})

	t.Run("leaves out the excluded images", func(t *testing.T) {
		images := PlanBuilds("/work/nexus", twoImages, testTarget, testTags, []string{"nexus"}, BuildOptions{})

		require.Len(t, images, 1)
		assert.Equal(t, "sidecar", images[0].Name)
		assert.Equal(t, []string{"BAZ=qux", "FOO=bar"}, images[0].Request.BuildArgs)
	})

	t.Run("plans nothing without images", func(t *testing.T) {
		images := PlanBuilds("/work/nexus", cldkfile.ProjectFile{Namespace: "official"}, testTarget, nil, nil, BuildOptions{})

		assert.Empty(t, images)
	})
}

func TestCheckBuilder(t *testing.T) {
	t.Run("creates the builder for the build CLI and checks it", func(t *testing.T) {
		var binaries []string
		builder := recordingBuilder(new([]string))
		images := PlanBuilds("/work/nexus", twoImages, testTarget, testTags, nil, BuildOptions{})

		checked, err := CheckBuilder(context.Background(), func(binary string) Builder {
			binaries = append(binaries, binary)

			return builder
		}, "podman", images)

		require.NoError(t, err)
		assert.Same(t, builder, checked)
		assert.Equal(t, []string{"podman"}, binaries)
		assert.Len(t, builder.CheckCalls(), 1)
	})

	t.Run("needs no builder without images to build", func(t *testing.T) {
		checked, err := CheckBuilder(context.Background(), func(string) Builder {
			require.Fail(t, "no builder should be created")

			return nil
		}, "docker", nil)

		require.NoError(t, err)
		assert.Nil(t, checked)
	})

	t.Run("fails on a missing build CLI", func(t *testing.T) {
		builder := recordingBuilder(new([]string))
		builder.CheckFunc = func(context.Context) error {
			return errors.New(`builder "podman" not found`)
		}
		images := PlanBuilds("/work/nexus", twoImages, testTarget, testTags, nil, BuildOptions{})

		_, err := CheckBuilder(context.Background(), func(string) Builder { return builder }, "podman", images)

		require.ErrorContains(t, err, `builder "podman" not found`)
	})
}

func TestBuildAndPushImages(t *testing.T) {
	images := PlanBuilds("/work/nexus", twoImages, testTarget, testTags, nil, BuildOptions{})

	t.Run("builds all images, then pushes them in their order", func(t *testing.T) {
		var steps []string
		builder := recordingBuilder(&steps)

		require.NoError(t, BuildImages(context.Background(), builder, images))
		pushed, err := PushImages(context.Background(), builder, images)

		require.NoError(t, err)
		assert.Equal(t, images, pushed)
		assert.Equal(t, []string{"build " + nexusRef, "build " + sidecarRef, "push " + nexusRef, "push " + sidecarRef}, steps)
	})

	t.Run("aborts on the first failed build and names the image", func(t *testing.T) {
		var steps []string
		builder := recordingBuilder(&steps)
		builder.BuildFunc = func(_ context.Context, req build.BuildRequest) error {
			steps = append(steps, "build "+req.Tag)

			return errors.New("build failed with exit code 2 of docker")
		}

		err := BuildImages(context.Background(), builder, images)

		require.ErrorContains(t, err, "failed to build image nexus: build failed with exit code 2 of docker")
		assert.Equal(t, []string{"build " + nexusRef}, steps)
	})

	t.Run("aborts on the first failed push, names the image and returns the images pushed before", func(t *testing.T) {
		var steps []string
		builder := recordingBuilder(&steps)
		builder.PushFunc = func(_ context.Context, ref string) error {
			steps = append(steps, "push "+ref)

			if ref == sidecarRef {
				return errors.New("push failed with exit code 1 of docker")
			}

			return nil
		}

		pushed, err := PushImages(context.Background(), builder, images)

		require.ErrorContains(t, err, "failed to push image sidecar: push failed with exit code 1 of docker")
		assert.Equal(t, images[:1], pushed)
		assert.Equal(t, []string{"push " + nexusRef, "push " + sidecarRef}, steps)
	})
}
