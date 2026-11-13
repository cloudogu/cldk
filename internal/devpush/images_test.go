package devpush

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudogu/cldk/internal/build"
	"github.com/cloudogu/cldk/internal/project"
)

// twoImagesYAML is a project file with two images, one with a custom build and one with the defaults.
const twoImagesYAML = `namespace: official
images:
  sidecar:
    context: sidecar
    dockerfile: build/Dockerfile.sidecar
    key: nexusSaManager
    target: runtime
    buildArgs:
      FOO: bar
      BAZ: qux
  nexus: {}
`

func TestPush_images(t *testing.T) {
	const (
		reg        = "localhost:5001"
		clusterReg = "k3d-registry-proxy.localhost:5000"
		nexusRef   = reg + "/official/dogu/v3/images/nexus:" + wantDevVersion
		sidecarRef = reg + "/official/dogu/v3/images/sidecar:" + wantDevVersion
		chartRef   = reg + "/official/dogu/v3/charts/nexus:" + wantDevVersion
	)

	t.Run("builds all images by name, then pushes them before pushing the chart and applying the dogu", func(t *testing.T) {
		pusher := newMockedPusher()
		dir := writeProject(t, twoImagesYAML, nil)

		result, err := pusher.Push(context.Background(), Options{
			Dir: dir, Registry: reg, ClusterRegistry: clusterReg, Build: project.BuildOptions{Builder: "podman"},
		})

		require.NoError(t, err)
		assert.Equal(t, []string{
			"build " + nexusRef, "build " + sidecarRef,
			"push " + nexusRef, "push " + sidecarRef,
			"push " + chartRef, "apply nexus",
		}, pusher.steps)
		assert.Equal(t, []string{"podman"}, pusher.builderBinaries)
		assert.Len(t, pusher.builder.CheckCalls(), 1)
		assert.Equal(t, []PushedImage{{Name: "nexus", Reference: nexusRef}, {Name: "sidecar", Reference: sidecarRef}}, result.Images)
	})

	t.Run("builds with the build settings of the options", func(t *testing.T) {
		pusher := newMockedPusher()
		dir := writeProject(t, twoImagesYAML, nil)

		_, err := pusher.Push(context.Background(), Options{
			Dir: dir, Registry: reg,
			Build: project.BuildOptions{Platform: "linux/arm64", NoCache: true, BuildArgs: []string{"FOO=override", "HTTP_PROXY"}},
		})

		require.NoError(t, err)
		require.Len(t, pusher.builder.BuildCalls(), 2)
		assert.Equal(t, build.BuildRequest{
			Context:    dir,
			Dockerfile: filepath.Join(dir, "Dockerfile"),
			Tag:        nexusRef,
			BuildArgs:  []string{"FOO=override", "HTTP_PROXY"},
			Platform:   "linux/arm64",
			NoCache:    true,
		}, pusher.builder.BuildCalls()[0].Req)
	})

	t.Run("tags the images with the dev version and ignores the release tags of cldk.yaml", func(t *testing.T) {
		pusher := newMockedPusher()
		dir := writeProject(t, "namespace: official\nimages:\n  nexus:\n    tag: 3.96.4-1\n", nil)

		result, err := pusher.Push(context.Background(), Options{Dir: dir, Registry: reg})

		require.NoError(t, err)
		assert.Equal(t, []PushedImage{{Name: "nexus", Reference: nexusRef}}, result.Images)
	})

	t.Run("does not check the builder without images", func(t *testing.T) {
		pusher := newMockedPusher()
		dir := writeProject(t, "namespace: official\n", nil)

		result, err := pusher.Push(context.Background(), Options{Dir: dir, Registry: reg})

		require.NoError(t, err)
		assert.Empty(t, pusher.builder.CheckCalls())
		assert.Empty(t, pusher.builder.BuildCalls())
		assert.Empty(t, result.Images)
	})

	t.Run("aborts before connecting to the cluster on a missing builder", func(t *testing.T) {
		pusher := newMockedPusher()
		pusher.builder.CheckFunc = func(context.Context) error {
			return errors.New(`builder "docker" not found`)
		}
		dir := writeProject(t, twoImagesYAML, nil)

		_, err := pusher.Push(context.Background(), Options{Dir: dir, Registry: reg, Build: project.BuildOptions{Builder: "docker"}})

		require.ErrorContains(t, err, `builder "docker" not found`)
		assert.Empty(t, pusher.kubeOptions)
		assert.Empty(t, pusher.steps)
	})

	rewriteFailures := []struct {
		name       string
		cldkYAML   string
		chartFiles map[string]string
		wantErr    string
	}{
		{
			name:     "an image without an entry in the mirror patch template",
			cldkYAML: "namespace: official\nimages:\n  unknown: {}\n",
			wantErr:  `repository name "unknown"`,
		},
		{
			name:     "an unknown key",
			cldkYAML: "namespace: official\nimages:\n  nexus:\n    key: missing\n",
			wantErr:  `no entry "missing"`,
		},
		{
			name:       "a missing mirror patch template",
			cldkYAML:   "namespace: official\nimages:\n  nexus: {}\n",
			chartFiles: map[string]string{"mirror-patch-tpl.yaml": ""},
			wantErr:    "mirror-patch-tpl.yaml",
		},
		{
			name:       "lint errors",
			cldkYAML:   "namespace: official\nimages:\n  nexus: {}\n",
			chartFiles: map[string]string{"values.schema.json": `{"type": "object", "required": ["missingValue"]}`},
			wantErr:    "missingValue",
		},
	}

	for _, tt := range rewriteFailures {
		t.Run("aborts before the first build on "+tt.name, func(t *testing.T) {
			pusher := newMockedPusher()
			dir := writeProject(t, tt.cldkYAML, tt.chartFiles)

			if content, ok := tt.chartFiles["mirror-patch-tpl.yaml"]; ok && content == "" {
				require.NoError(t, os.Remove(filepath.Join(dir, "k8s", "helm", "mirror-patch-tpl.yaml")))
			}

			_, err := pusher.Push(context.Background(), Options{Dir: dir, Registry: reg})

			require.ErrorContains(t, err, tt.wantErr)
			assert.Empty(t, pusher.steps)
		})
	}

	t.Run("aborts on the first failed build without pushing anything and names the image", func(t *testing.T) {
		pusher := newMockedPusher()
		pusher.builder.BuildFunc = func(_ context.Context, req build.BuildRequest) error {
			pusher.steps = append(pusher.steps, "build "+req.Tag)

			return errors.New("build failed with exit code 2 of docker")
		}
		dir := writeProject(t, twoImagesYAML, nil)

		_, err := pusher.Push(context.Background(), Options{Dir: dir, Registry: reg})

		require.ErrorContains(t, err, "failed to build image nexus: build failed with exit code 2 of docker")
		assert.Equal(t, []string{"build " + nexusRef}, pusher.steps)
	})

	t.Run("aborts on the first failed image push after building all images and names the image", func(t *testing.T) {
		pusher := newMockedPusher()
		pusher.builder.PushFunc = func(_ context.Context, ref string) error {
			pusher.steps = append(pusher.steps, "push "+ref)

			return errors.New("push failed with exit code 1 of docker")
		}
		dir := writeProject(t, twoImagesYAML, nil)

		_, err := pusher.Push(context.Background(), Options{Dir: dir, Registry: reg})

		require.ErrorContains(t, err, "failed to push image nexus: push failed with exit code 1 of docker")
		assert.Equal(t, []string{"build " + nexusRef, "build " + sidecarRef, "push " + nexusRef}, pusher.steps)
	})

	t.Run("pushes the chart with the built images on the cluster address and keeps the other images", func(t *testing.T) {
		reg := newTestRegistry(t)
		dir := writeProject(t, twoImagesYAML, nil)

		result, err := newTestPusher().Push(context.Background(), Options{
			Dir: dir, Registry: reg, ClusterRegistry: clusterReg, PlainHTTP: true,
		})

		require.NoError(t, err)
		pushed := pullChart(t, result.ChartReference)
		values := pushed.Values
		assert.Equal(t, map[string]any{
			"registry": clusterReg, "repository": "official/dogu/v3/images/nexus", "tag": wantDevVersion,
		}, values["nexus"].(map[string]any)["image"])
		assert.Equal(t, map[string]any{
			"registry": clusterReg, "repository": "official/dogu/v3/images/sidecar", "tag": wantDevVersion,
		}, values["nexus"].(map[string]any)["saManager"].(map[string]any)["image"])
		// Like in release, the patches of the other images are rendered too, with normalized references.
		assert.Equal(t, map[string]any{
			"registry": "index.docker.io", "repository": "library/postgres", "tag": "14.18",
		}, values["postgresql"].(map[string]any)["image"])

		mirrorPatch := fileContents(pushed.Files)["mirror-patch-tpl.yaml"]
		assert.Contains(t, mirrorPatch, "nexus: "+clusterReg+"/official/dogu/v3/images/nexus:"+wantDevVersion)
		assert.Contains(t, mirrorPatch, "nexusSaManager: "+clusterReg+"/official/dogu/v3/images/sidecar:"+wantDevVersion)
		assert.Contains(t, mirrorPatch, "postgresql: docker.io/library/postgres:14.18")
	})
}

func TestPush_replaceAndSkipImages(t *testing.T) {
	const (
		reg         = "localhost:5001"
		clusterReg  = "k3d-registry-proxy.localhost:5000"
		replacedRef = "registry.example.com/foo/sidecar:1.0"
	)

	t.Run("writes a replaced image unchanged into the chart without building it", func(t *testing.T) {
		reg := newTestRegistry(t)
		pusher := newTestPusher()
		builder := newTestBuilder()
		pusher.newBuilder = func(string) Builder {
			return builder
		}
		dir := writeProject(t, twoImagesYAML, nil)

		result, err := pusher.Push(context.Background(), Options{
			Dir: dir, Registry: reg, ClusterRegistry: clusterReg, PlainHTTP: true,
			ReplaceImages: map[string]string{"sidecar": replacedRef},
		})

		require.NoError(t, err)
		require.Len(t, builder.BuildCalls(), 1)
		assert.Equal(t, reg+"/official/dogu/v3/images/nexus:"+wantDevVersion, builder.BuildCalls()[0].Req.Tag)
		assert.Equal(t, []ReplacedImage{{Name: "sidecar", Reference: replacedRef}}, result.ReplacedImages)

		values := pullChart(t, result.ChartReference).Values
		assert.Equal(t, map[string]any{
			"registry": "registry.example.com", "repository": "foo/sidecar", "tag": "1.0",
		}, values["nexus"].(map[string]any)["saManager"].(map[string]any)["image"])
		assert.Equal(t, map[string]any{
			"registry": clusterReg, "repository": "official/dogu/v3/images/nexus", "tag": wantDevVersion,
		}, values["nexus"].(map[string]any)["image"])
	})

	t.Run("neither builds nor rewrites a skipped image", func(t *testing.T) {
		reg := newTestRegistry(t)
		pusher := newTestPusher()
		builder := newTestBuilder()
		pusher.newBuilder = func(string) Builder {
			return builder
		}
		dir := writeProject(t, twoImagesYAML, nil)
		unchanged := loadTestChart(t).Values["nexus"].(map[string]any)["saManager"].(map[string]any)["image"]

		result, err := pusher.Push(context.Background(), Options{
			Dir: dir, Registry: reg, ClusterRegistry: clusterReg, PlainHTTP: true, SkipImages: []string{"sidecar"},
		})

		require.NoError(t, err)
		require.Len(t, builder.BuildCalls(), 1)
		assert.Equal(t, []string{"sidecar"}, result.SkippedImages)
		assert.Equal(t, []PushedImage{{Name: "nexus", Reference: reg + "/official/dogu/v3/images/nexus:" + wantDevVersion}}, result.Images)

		values := pullChart(t, result.ChartReference).Values
		assert.Equal(t, unchanged, values["nexus"].(map[string]any)["saManager"].(map[string]any)["image"])
	})

	t.Run("neither checks the builder nor builds when all images are replaced or skipped", func(t *testing.T) {
		pusher := newMockedPusher()
		dir := writeProject(t, twoImagesYAML, nil)

		result, err := pusher.Push(context.Background(), Options{
			Dir: dir, Registry: reg,
			ReplaceImages: map[string]string{"sidecar": replacedRef}, SkipImages: []string{"nexus"},
		})

		require.NoError(t, err)
		assert.Empty(t, pusher.builder.CheckCalls())
		assert.Empty(t, pusher.builderBinaries)
		assert.Equal(t, []string{"push " + reg + "/official/dogu/v3/charts/nexus:" + wantDevVersion, "apply nexus"}, pusher.steps)
		assert.Empty(t, result.Images)
	})

	t.Run("neither checks the builder nor builds when all images are replaced", func(t *testing.T) {
		pusher := newMockedPusher()
		dir := writeProject(t, twoImagesYAML, nil)

		result, err := pusher.Push(context.Background(), Options{
			Dir: dir, Registry: reg,
			ReplaceImages: map[string]string{"nexus": "registry.example.com/foo/nexus:1.0", "sidecar": replacedRef},
		})

		require.NoError(t, err)
		assert.Empty(t, pusher.builderBinaries)
		assert.Empty(t, pusher.builder.BuildCalls())
		assert.Equal(t, []ReplacedImage{
			{Name: "nexus", Reference: "registry.example.com/foo/nexus:1.0"}, {Name: "sidecar", Reference: replacedRef},
		}, result.ReplacedImages)
	})

	t.Run("needs no mirror patch template when all images are skipped", func(t *testing.T) {
		pusher := newMockedPusher()
		dir := writeProject(t, twoImagesYAML, nil)
		require.NoError(t, os.Remove(filepath.Join(dir, "k8s", "helm", "mirror-patch-tpl.yaml")))

		_, err := pusher.Push(context.Background(), Options{Dir: dir, Registry: reg, SkipImages: []string{"nexus", "sidecar"}})

		require.NoError(t, err)
		assert.Empty(t, pusher.builder.CheckCalls())
	})

	t.Run("needs the mirror patch template for a replaced image", func(t *testing.T) {
		pusher := newMockedPusher()
		dir := writeProject(t, twoImagesYAML, nil)
		require.NoError(t, os.Remove(filepath.Join(dir, "k8s", "helm", "mirror-patch-tpl.yaml")))

		_, err := pusher.Push(context.Background(), Options{
			Dir: dir, Registry: reg, ReplaceImages: map[string]string{"sidecar": replacedRef}, SkipImages: []string{"nexus"},
		})

		require.ErrorContains(t, err, "mirror-patch-tpl.yaml")
		assert.Empty(t, pusher.steps)
	})

	unknown := []struct {
		name    string
		opts    Options
		wantErr string
	}{
		{
			name:    "an unknown replaced image",
			opts:    Options{ReplaceImages: map[string]string{"postgres": "docker.io/library/postgres:14"}},
			wantErr: `unknown image "postgres" of --replace-image, cldk.yaml has the images nexus, sidecar`,
		},
		{
			name:    "an unknown skipped image",
			opts:    Options{SkipImages: []string{"nexus", "postgres"}},
			wantErr: `unknown image "postgres" of --skip-image, cldk.yaml has the images nexus, sidecar`,
		},
	}

	for _, tt := range unknown {
		t.Run("aborts before loading the chart on "+tt.name, func(t *testing.T) {
			pusher := newMockedPusher()
			dir := writeProject(t, twoImagesYAML, map[string]string{"Chart.yaml": "invalid: ["})
			tt.opts.Dir = dir
			tt.opts.Registry = reg

			_, err := pusher.Push(context.Background(), tt.opts)

			require.ErrorContains(t, err, tt.wantErr)
			assert.Empty(t, pusher.builderBinaries)
			assert.Empty(t, pusher.steps)
		})
	}
}
