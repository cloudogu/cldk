package release

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/chart/v2/loader"

	"github.com/cloudogu/cldk/internal/build"
	"github.com/cloudogu/cldk/internal/project"
)

// projectBuilder is a mocked builder that records its builds and pushes and writes a new random image to the
// registry on each push, like a real builder that has built the image.
type projectBuilder struct {
	*MockBuilder
	// binaries are the build CLIs the builder has been created for.
	binaries []string
	// steps are the builds and pushes in their order.
	steps []string
	// digests are the digests of the pushed images by reference.
	digests map[string]string
}

// newProjectBuilder returns a mocked builder that builds every image and pushes a random image for it.
func newProjectBuilder(t *testing.T) *projectBuilder {
	t.Helper()

	b := &projectBuilder{digests: map[string]string{}}
	b.MockBuilder = &MockBuilder{
		CheckFunc: func(context.Context) error {
			return nil
		},
		BuildFunc: func(_ context.Context, req build.BuildRequest) error {
			b.steps = append(b.steps, "build "+req.Tag)

			return nil
		},
		PushFunc: func(_ context.Context, ref string) error {
			b.steps = append(b.steps, "push "+ref)
			b.digests[ref] = pushImage(t, ref)

			return nil
		},
	}

	return b
}

// newProjectReleaser returns a releaser that builds with the mocked builder.
func newProjectReleaser(b *projectBuilder) *Releaser {
	r := NewReleaser()
	r.newBuilder = func(binary string) Builder {
		b.binaries = append(b.binaries, binary)

		return b
	}

	return r
}

// writeProject creates a project directory with the given cldk.yaml and a copy of the test chart nexus in k8s/helm,
// overwriting the given files of the chart, and returns the project directory.
func writeProject(t *testing.T, cldkYAML string, chartFiles map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cldk.yaml"), []byte(cldkYAML), 0o600))

	chartDir := filepath.Join(dir, "k8s", "helm")
	require.NoError(t, os.CopyFS(chartDir, os.DirFS(filepath.Join("testdata", "charts", "nexus"))))

	for file, content := range chartFiles {
		require.NoError(t, os.WriteFile(filepath.Join(chartDir, file), []byte(content), 0o600))
	}

	return dir
}

// nexusProject is a project file with the image nexus and its release tag.
const nexusProject = "namespace: testing\nimages:\n  nexus:\n    tag: 3.96.4-1\n"

func TestReleaseProject(t *testing.T) {
	t.Run("builds and pushes the image with its release tag and releases the chart rewritten to it", func(t *testing.T) {
		reg := newTargetRegistry(t)
		builder := newProjectBuilder(t)
		dir := writeProject(t, nexusProject, nil)
		nexusRef := reg + "/testing/dogu/v3/images/nexus:3.96.4-1"

		result, err := newProjectReleaser(builder).ReleaseProject(context.Background(), testTarget(reg),
			ProjectOptions{Dir: dir, Build: project.BuildOptions{Builder: "podman"}})

		require.NoError(t, err)
		assert.Equal(t, []string{"podman"}, builder.binaries)
		assert.Equal(t, []string{"build " + nexusRef, "push " + nexusRef}, builder.steps)
		assert.Equal(t, []BuiltImage{{Name: "nexus", Reference: nexusRef, Digest: builder.digests[nexusRef]}}, result.BuiltImages)
		assert.Equal(t, reg+chartReference, result.ChartReference)
		assert.Equal(t, imageDigest(t, reg+chartReference), result.ChartDigest)

		images := chartYAML(t, pullReleasedChart(t, reg), mirrorPatchTemplateFile)["values"].(map[string]any)["images"]
		assert.Equal(t, nexusRef, images.(map[string]any)["nexus"])
	})
}

// twoImagesProject is a project file with the images nexus and sidecar, the sidecar with an explicit key.
const twoImagesProject = `namespace: testing
images:
  nexus:
    tag: 3.96.4-1
  sidecar:
    dockerfile: sidecar/Dockerfile
    key: nexusSaManager
    tag: 1.2.0
`

func TestReleaseProject_flow(t *testing.T) {
	t.Run("builds all images, then pushes them, then releases the chart", func(t *testing.T) {
		reg := newTargetRegistry(t)
		builder := newProjectBuilder(t)
		dir := writeProject(t, twoImagesProject, nil)
		nexusRef := reg + "/testing/dogu/v3/images/nexus:3.96.4-1"
		sidecarRef := reg + "/testing/dogu/v3/images/sidecar:1.2.0"

		result, err := newProjectReleaser(builder).ReleaseProject(context.Background(), testTarget(reg), ProjectOptions{Dir: dir})

		require.NoError(t, err)
		assert.Equal(t, []string{"build " + nexusRef, "build " + sidecarRef, "push " + nexusRef, "push " + sidecarRef}, builder.steps)
		assert.Equal(t, []BuiltImage{
			{Name: "nexus", Reference: nexusRef, Digest: builder.digests[nexusRef]},
			{Name: "sidecar", Reference: sidecarRef, Digest: builder.digests[sidecarRef]},
		}, result.BuiltImages)
		assert.Equal(t, filepath.Join(dir, "sidecar", "Dockerfile"), builder.BuildCalls()[1].Req.Dockerfile)

		images := chartYAML(t, pullReleasedChart(t, reg), mirrorPatchTemplateFile)["values"].(map[string]any)["images"]
		assert.Equal(t, map[string]any{
			"nexus":          nexusRef,
			"nexusSaManager": sidecarRef,
			"postgresql":     "docker.io/library/postgres:14.18",
		}, images)
	})

	t.Run("releases the chart with the version of Chart.yaml without changing the project directory", func(t *testing.T) {
		reg := newTargetRegistry(t)
		dir := writeProject(t, nexusProject, nil)
		before := snapshotDir(t, dir)

		result, err := newProjectReleaser(newProjectBuilder(t)).ReleaseProject(context.Background(), testTarget(reg), ProjectOptions{Dir: dir})

		require.NoError(t, err)
		assert.Equal(t, reg+chartReference, result.ChartReference)
		c, err := loader.LoadArchive(bytes.NewReader(pullReleasedChart(t, reg)))
		require.NoError(t, err)
		assert.Equal(t, "1.2.3", c.Metadata.Version)
		assert.Equal(t, "3.96.4", c.Metadata.AppVersion)
		assert.Equal(t, before, snapshotDir(t, dir), "the project directory should be unchanged")
	})

	t.Run("releases to the namespace of cldk.yaml without a namespace in the target", func(t *testing.T) {
		reg := newTargetRegistry(t)
		dir := writeProject(t, "namespace: official\nimages:\n  nexus:\n    tag: 3.96.4-1\n", nil)
		target := testTarget(reg)
		target.Namespace = ""

		result, err := newProjectReleaser(newProjectBuilder(t)).ReleaseProject(context.Background(), target, ProjectOptions{Dir: dir})

		require.NoError(t, err)
		assert.Equal(t, reg+"/official/dogu/v3/images/nexus:3.96.4-1", result.BuiltImages[0].Reference)
		assert.Equal(t, reg+"/official/dogu/v3/charts/nexus:1.2.3", result.ChartReference)
	})

	t.Run("releases to the namespace of the target instead of cldk.yaml", func(t *testing.T) {
		reg := newTargetRegistry(t)
		dir := writeProject(t, "namespace: official\nimages:\n  nexus:\n    tag: 3.96.4-1\n", nil)

		result, err := newProjectReleaser(newProjectBuilder(t)).ReleaseProject(context.Background(), testTarget(reg), ProjectOptions{Dir: dir})

		require.NoError(t, err)
		assert.Equal(t, reg+chartReference, result.ChartReference)
	})

	t.Run("releases only the chart without images", func(t *testing.T) {
		reg := newTargetRegistry(t)
		builder := newProjectBuilder(t)
		dir := writeProject(t, "namespace: testing\n", nil)
		require.NoError(t, os.Remove(filepath.Join(dir, "k8s", "helm", mirrorPatchTemplateFile)))

		result, err := newProjectReleaser(builder).ReleaseProject(context.Background(), testTarget(reg), ProjectOptions{Dir: dir})

		require.NoError(t, err)
		assert.Empty(t, builder.binaries, "no builder is needed")
		assert.Empty(t, result.BuiltImages)
		assert.Equal(t, imageDigest(t, reg+chartReference), result.ChartDigest)
	})
}

func TestReleaseProject_abortsBeforeBuilding(t *testing.T) {
	tests := []struct {
		name       string
		cldkYAML   string
		chartFiles map[string]string
		opts       ProjectOptions
		wantErr    string
	}{
		{name: "a missing project file", wantErr: "cldk.yaml not found"},
		{
			name:     "a missing chart",
			cldkYAML: "namespace: testing\nchart: helm\n",
			wantErr:  "failed to load chart",
		},
		{
			name:     "images without release tag",
			cldkYAML: "namespace: testing\nimages:\n  nexus: {}\n  sidecar:\n    tag: 1.2.0\n  postgres: {}\n",
			wantErr:  "no release tag for the images nexus, postgres, give it with tag: in cldk.yaml or with --tag <name>=<tag>",
		},
		{
			name:       "lint errors",
			cldkYAML:   nexusProject,
			chartFiles: map[string]string{"values.schema.json": `{"type": "object", "required": ["missingValue"]}`},
			wantErr:    "missingValue",
		},
		{
			name:       "a missing mirror patch template",
			cldkYAML:   nexusProject,
			chartFiles: map[string]string{mirrorPatchTemplateFile: ""},
			wantErr:    "chart has no mirror-patch-tpl.yaml",
		},
		{
			name:     "an image without an entry in the mirror patch template",
			cldkYAML: "namespace: testing\nimages:\n  unknown:\n    tag: \"1.0\"\n",
			wantErr:  `repository name "unknown"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := newTargetRegistry(t)
			builder := newProjectBuilder(t)
			dir := writeProject(t, tt.cldkYAML, tt.chartFiles)

			if tt.cldkYAML == "" {
				require.NoError(t, os.Remove(filepath.Join(dir, "cldk.yaml")))
			}

			if content, ok := tt.chartFiles[mirrorPatchTemplateFile]; ok && content == "" {
				require.NoError(t, os.Remove(filepath.Join(dir, "k8s", "helm", mirrorPatchTemplateFile)))
			}

			tt.opts.Dir = dir

			_, err := newProjectReleaser(builder).ReleaseProject(context.Background(), testTarget(reg), tt.opts)

			require.ErrorContains(t, err, tt.wantErr)
			assert.Empty(t, builder.steps)
			assertRegistryEmpty(t, reg)
		})
	}

	t.Run("fails before building on a missing builder", func(t *testing.T) {
		reg := newTargetRegistry(t)
		builder := newProjectBuilder(t)
		builder.CheckFunc = func(context.Context) error {
			return errors.New(`builder "podman" not found`)
		}
		dir := writeProject(t, nexusProject, nil)

		_, err := newProjectReleaser(builder).ReleaseProject(context.Background(), testTarget(reg), ProjectOptions{Dir: dir})

		require.ErrorContains(t, err, `builder "podman" not found`)
		assert.Empty(t, builder.steps)
	})
}

func TestReleaseProject_existingTargets(t *testing.T) {
	t.Run("fails before building with all conflicts if release tags or the chart version already exist", func(t *testing.T) {
		reg := newTargetRegistry(t)
		dir := writeProject(t, twoImagesProject, nil)
		_, err := newProjectReleaser(newProjectBuilder(t)).ReleaseProject(context.Background(), testTarget(reg), ProjectOptions{Dir: dir})
		require.NoError(t, err)
		builder := newProjectBuilder(t)

		_, err = newProjectReleaser(builder).ReleaseProject(context.Background(), testTarget(reg), ProjectOptions{Dir: dir})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "image "+reg+"/testing/dogu/v3/images/nexus:3.96.4-1 is already released, use --force to overwrite it")
		assert.Contains(t, err.Error(), "image "+reg+"/testing/dogu/v3/images/sidecar:1.2.0 is already released")
		assert.Contains(t, err.Error(), "chart "+reg+chartReference+" is already released")
		assert.Empty(t, builder.steps)
	})

	t.Run("fails before building if only the chart version already exists", func(t *testing.T) {
		reg := newTargetRegistry(t)
		chartPath, _ := packChart(t, "nexus")
		_, err := NewReleaser().Release(context.Background(), chartPath, testTarget(reg), ArchiveOptions{})
		require.NoError(t, err)
		builder := newProjectBuilder(t)
		dir := writeProject(t, nexusProject, nil)

		_, err = newProjectReleaser(builder).ReleaseProject(context.Background(), testTarget(reg), ProjectOptions{Dir: dir})

		require.ErrorContains(t, err, "chart "+reg+chartReference+" is already released")
		assert.Empty(t, builder.steps)
		assertNotInRegistry(t, reg+"/testing/dogu/v3/images/nexus:3.96.4-1")
	})
}

func TestPlanProject(t *testing.T) {
	t.Run("checks everything but builds and pushes nothing", func(t *testing.T) {
		reg := newTargetRegistry(t)
		builder := newProjectBuilder(t)
		dir := writeProject(t, twoImagesProject, nil)

		plan, err := newProjectReleaser(builder).PlanProject(context.Background(), testTarget(reg),
			ProjectOptions{Dir: dir, Build: project.BuildOptions{Builder: "podman"}})

		require.NoError(t, err)
		assert.Equal(t, Plan{
			BuiltImages: []BuiltImage{
				{Name: "nexus", Reference: reg + "/testing/dogu/v3/images/nexus:3.96.4-1"},
				{Name: "sidecar", Reference: reg + "/testing/dogu/v3/images/sidecar:1.2.0"},
			},
			Images:         []PlannedImage{},
			ChartReference: reg + chartReference,
		}, plan)
		assert.Equal(t, []string{"podman"}, builder.binaries)
		assert.Len(t, builder.CheckCalls(), 1, "the builder should be checked")
		assert.Empty(t, builder.steps)
		assertRegistryEmpty(t, reg)
	})

	t.Run("returns the plan with all conflicts", func(t *testing.T) {
		reg := newTargetRegistry(t)
		dir := writeProject(t, twoImagesProject, nil)
		pushImage(t, reg+"/testing/dogu/v3/images/sidecar:1.2.0")
		builder := newProjectBuilder(t)

		plan, err := newProjectReleaser(builder).PlanProject(context.Background(), testTarget(reg), ProjectOptions{Dir: dir})

		require.NoError(t, err)
		assert.Equal(t, []BuiltImage{
			{Name: "nexus", Reference: reg + "/testing/dogu/v3/images/nexus:3.96.4-1", Status: StatusPushed},
			{Name: "sidecar", Reference: reg + "/testing/dogu/v3/images/sidecar:1.2.0", Status: StatusConflict},
		}, plan.BuiltImages)
		require.ErrorContains(t, plan.Conflicts(), "image "+reg+"/testing/dogu/v3/images/sidecar:1.2.0 is already released")
		assert.Empty(t, builder.steps)
	})

	t.Run("fails on lint errors", func(t *testing.T) {
		reg := newTargetRegistry(t)
		dir := writeProject(t, nexusProject, map[string]string{"values.schema.json": `{"type": "object", "required": ["missingValue"]}`})

		_, err := newProjectReleaser(newProjectBuilder(t)).PlanProject(context.Background(), testTarget(reg), ProjectOptions{Dir: dir})

		require.ErrorContains(t, err, "missingValue")
	})
}

// snapshotDir returns the content of all files below dir by their relative path.
func snapshotDir(t *testing.T, dir string) map[string]string {
	t.Helper()

	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		files[rel] = string(content)

		return nil
	})
	require.NoError(t, err)

	return files
}

func TestReleaseProject_images(t *testing.T) {
	t.Run("copies an image given with -i instead of building the image of the same name", func(t *testing.T) {
		src := newTargetRegistry(t)
		reg := newTargetRegistry(t)
		builder := newProjectBuilder(t)
		dir := writeProject(t, twoImagesProject, nil)
		sidecar, sidecarDigest := newSourceImage(t, src, "ci/sidecar:1.2.0-rc.1")
		nexusRef := reg + "/testing/dogu/v3/images/nexus:3.96.4-1"
		sidecarRef := reg + "/testing/dogu/v3/images/sidecar:1.2.0-rc.1"

		result, err := newProjectReleaser(builder).ReleaseProject(context.Background(), testTarget(reg),
			ProjectOptions{Dir: dir, Images: []string{sidecar}})

		require.NoError(t, err)
		assert.Equal(t, []string{"build " + nexusRef, "push " + nexusRef}, builder.steps)
		assert.Equal(t, []BuiltImage{{Name: "nexus", Reference: nexusRef, Digest: builder.digests[nexusRef]}}, result.BuiltImages)
		assert.Equal(t, []ReleasedImage{{Source: sidecar, Reference: sidecarRef, Digest: sidecarDigest}}, result.Images)
		assert.Equal(t, sidecarDigest, imageDigest(t, sidecarRef))

		images := chartYAML(t, pullReleasedChart(t, reg), mirrorPatchTemplateFile)["values"].(map[string]any)["images"]
		assert.Equal(t, map[string]any{
			"nexus":          nexusRef,
			"nexusSaManager": sidecarRef,
			"postgresql":     "docker.io/library/postgres:14.18",
		}, images, "the copied image keeps the key of cldk.yaml")
	})

	t.Run("releases an image given with -i that is not in cldk.yaml with its image key", func(t *testing.T) {
		src := newTargetRegistry(t)
		reg := newTargetRegistry(t)
		dir := writeProject(t, nexusProject, nil)
		postgres, postgresDigest := newSourceImage(t, src, "ci/pg:14.18")
		postgresRef := reg + "/testing/dogu/v3/images/pg:14.18"

		result, err := newProjectReleaser(newProjectBuilder(t)).ReleaseProject(context.Background(), testTarget(reg),
			ProjectOptions{Dir: dir, Images: []string{postgres}, ImageKeys: []string{"pg=postgresql"}})

		require.NoError(t, err)
		assert.Len(t, result.BuiltImages, 1)
		assert.Equal(t, []ReleasedImage{{Source: postgres, Reference: postgresRef, Digest: postgresDigest}}, result.Images)

		images := chartYAML(t, pullReleasedChart(t, reg), mirrorPatchTemplateFile)["values"].(map[string]any)["images"]
		assert.Equal(t, postgresRef, images.(map[string]any)["postgresql"])
	})

	t.Run("needs no builder if all images are given with -i", func(t *testing.T) {
		src := newTargetRegistry(t)
		reg := newTargetRegistry(t)
		builder := newProjectBuilder(t)
		dir := writeProject(t, "namespace: testing\nimages:\n  nexus: {}\n", nil)
		nexus, _ := newSourceImage(t, src, "ci/nexus:3.96.4-1")

		result, err := newProjectReleaser(builder).ReleaseProject(context.Background(), testTarget(reg),
			ProjectOptions{Dir: dir, Images: []string{nexus}})

		require.NoError(t, err)
		assert.Empty(t, builder.binaries)
		assert.Empty(t, result.BuiltImages)
		assert.Len(t, result.Images, 1)
	})

	t.Run("skips a copied image that is already released and plans it", func(t *testing.T) {
		src := newTargetRegistry(t)
		reg := newTargetRegistry(t)
		dir := writeProject(t, twoImagesProject, nil)
		sidecar, _ := newSourceImage(t, src, "ci/sidecar:1.2.0")
		copyImage(t, sidecar, reg+"/testing/dogu/v3/images/sidecar:1.2.0")

		plan, err := newProjectReleaser(newProjectBuilder(t)).PlanProject(context.Background(), testTarget(reg),
			ProjectOptions{Dir: dir, Images: []string{sidecar}})

		require.NoError(t, err)
		require.Len(t, plan.Images, 1)
		assert.Equal(t, StatusSkipped, plan.Images[0].Status)
		assert.Len(t, plan.BuiltImages, 1)
	})
}

func TestReleaseProject_tags(t *testing.T) {
	t.Run("takes the release tag of --tag over cldk.yaml", func(t *testing.T) {
		reg := newTargetRegistry(t)
		builder := newProjectBuilder(t)
		dir := writeProject(t, "namespace: testing\nimages:\n  nexus:\n    tag: 3.96.4-1\n  sidecar:\n    key: nexusSaManager\n", nil)

		result, err := newProjectReleaser(builder).ReleaseProject(context.Background(), testTarget(reg),
			ProjectOptions{Dir: dir, Tags: []string{"nexus=3.96.4-2", "sidecar=1.2.0"}})

		require.NoError(t, err)
		require.Len(t, result.BuiltImages, 2)
		assert.Equal(t, reg+"/testing/dogu/v3/images/nexus:3.96.4-2", result.BuiltImages[0].Reference)
		assert.Equal(t, reg+"/testing/dogu/v3/images/sidecar:1.2.0", result.BuiltImages[1].Reference)
	})

	invalid := []struct {
		name    string
		tags    []string
		images  []string
		wantErr string
	}{
		{name: "an unknown image", tags: []string{"postgres=14"}, wantErr: `release tag "postgres=14" of --tag is for no image of cldk.yaml (images: nexus, sidecar)`},
		{name: "an image given with -i", tags: []string{"sidecar=1.3.0"}, images: []string{"registry.example.com/ci/sidecar:1.2.0"}, wantErr: `release tag "sidecar=1.3.0" of --tag is for image sidecar, which is given with -i and not built`},
		{name: "an image given twice", tags: []string{"nexus=1", "nexus=2"}, wantErr: "image nexus is given more than once in --tag"},
		{name: "an invalid tag", tags: []string{"nexus=3.96/4"}, wantErr: `invalid release tag "nexus=3.96/4" of --tag: invalid tag "3.96/4"`},
		{name: "a missing tag", tags: []string{"nexus="}, wantErr: `invalid release tag "nexus=" of --tag: must be given as <name>=<tag>`},
		{name: "a missing name", tags: []string{"3.96.4"}, wantErr: `invalid release tag "3.96.4" of --tag`},
	}

	for _, tt := range invalid {
		t.Run("fails before building on "+tt.name, func(t *testing.T) {
			reg := newTargetRegistry(t)
			builder := newProjectBuilder(t)
			dir := writeProject(t, twoImagesProject, nil)

			_, err := newProjectReleaser(builder).ReleaseProject(context.Background(), testTarget(reg),
				ProjectOptions{Dir: dir, Tags: tt.tags, Images: tt.images})

			require.ErrorContains(t, err, tt.wantErr)
			assert.Empty(t, builder.binaries)
			assertRegistryEmpty(t, reg)
		})
	}

	t.Run("fails before building on an image key for a built image", func(t *testing.T) {
		reg := newTargetRegistry(t)
		builder := newProjectBuilder(t)
		dir := writeProject(t, twoImagesProject, nil)

		_, err := newProjectReleaser(builder).ReleaseProject(context.Background(), testTarget(reg),
			ProjectOptions{Dir: dir, ImageKeys: []string{"nexus=nexus"}})

		require.ErrorContains(t, err, "--image-key needs -i")
		assert.Empty(t, builder.binaries)
	})
}

func TestReleaseProject_force(t *testing.T) {
	t.Run("overwrites existing release tags and the chart version", func(t *testing.T) {
		reg := newTargetRegistry(t)
		dir := writeProject(t, nexusProject, nil)
		nexusRef := reg + "/testing/dogu/v3/images/nexus:3.96.4-1"
		_, err := newProjectReleaser(newProjectBuilder(t)).ReleaseProject(context.Background(), testTarget(reg), ProjectOptions{Dir: dir})
		require.NoError(t, err)
		builder := newProjectBuilder(t)
		opts := ProjectOptions{Dir: dir, Force: true}

		plan, err := newProjectReleaser(builder).PlanProject(context.Background(), testTarget(reg), opts)
		require.NoError(t, err)
		assert.Equal(t, []BuiltImage{{Name: "nexus", Reference: nexusRef, Status: StatusOverwritten}}, plan.BuiltImages)
		assert.Equal(t, StatusOverwritten, plan.ChartStatus)

		result, err := newProjectReleaser(builder).ReleaseProject(context.Background(), testTarget(reg), opts)

		require.NoError(t, err)
		assert.Equal(t, []BuiltImage{{Name: "nexus", Reference: nexusRef, Digest: builder.digests[nexusRef], Status: StatusOverwritten}},
			result.BuiltImages)
		assert.Equal(t, builder.digests[nexusRef], imageDigest(t, nexusRef))
		assert.Equal(t, StatusOverwritten, result.ChartStatus)
		assert.Equal(t, imageDigest(t, reg+chartReference), result.ChartDigest)
	})
}

func TestReleaseProject_failures(t *testing.T) {
	t.Run("leaves nothing in the target on a failed build", func(t *testing.T) {
		reg := newTargetRegistry(t)
		builder := newProjectBuilder(t)
		builder.BuildFunc = func(_ context.Context, req build.BuildRequest) error {
			builder.steps = append(builder.steps, "build "+req.Tag)

			if strings.Contains(req.Tag, "/sidecar:") {
				return errors.New("build failed with exit code 2 of docker")
			}

			return nil
		}
		dir := writeProject(t, twoImagesProject, nil)

		result, err := newProjectReleaser(builder).ReleaseProject(context.Background(), testTarget(reg), ProjectOptions{Dir: dir})

		require.ErrorContains(t, err, "failed to build image sidecar: build failed with exit code 2 of docker")
		assert.Empty(t, result.BuiltImages)
		assert.Empty(t, result.ChartDigest)
		assertRegistryEmpty(t, reg)
	})

	t.Run("returns the images pushed before a failed push of a built image, names the builder and releases no chart", func(t *testing.T) {
		reg := newTargetRegistry(t)
		builder := newProjectBuilder(t)
		pushFunc := builder.PushFunc
		builder.PushFunc = func(ctx context.Context, ref string) error {
			if strings.Contains(ref, "/sidecar:") {
				return errors.New("push failed with exit code 1 of podman")
			}

			return pushFunc(ctx, ref)
		}
		dir := writeProject(t, twoImagesProject, nil)
		nexusRef := reg + "/testing/dogu/v3/images/nexus:3.96.4-1"

		result, err := newProjectReleaser(builder).ReleaseProject(context.Background(), testTarget(reg),
			ProjectOptions{Dir: dir, Build: project.BuildOptions{Builder: "podman"}})

		require.ErrorContains(t, err, "failed to push image sidecar: push failed with exit code 1 of podman")
		assert.Contains(t, err.Error(), `check it with "podman login `+reg+`"`)
		assert.Equal(t, []BuiltImage{{Name: "nexus", Reference: nexusRef, Digest: builder.digests[nexusRef]}}, result.BuiltImages)
		assert.Empty(t, result.ChartDigest)
		assertNotInRegistry(t, reg+chartReference)
	})

	t.Run("returns the built images after a failed copy of an image given with -i", func(t *testing.T) {
		src := newTargetRegistry(t)
		handler := newRegistryHandler()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodHead && r.Method != http.MethodGet && strings.Contains(r.URL.Path, "/dogu/v3/images/postgres/") {
				w.WriteHeader(http.StatusForbidden)

				return
			}

			handler.ServeHTTP(w, r)
		}))
		t.Cleanup(srv.Close)
		reg := strings.TrimPrefix(srv.URL, "http://")
		builder := newProjectBuilder(t)
		dir := writeProject(t, nexusProject, nil)
		postgres, _ := newSourceImage(t, src, "ci/postgres:14.18")

		result, err := newProjectReleaser(builder).ReleaseProject(context.Background(), testTarget(reg),
			ProjectOptions{Dir: dir, Images: []string{postgres}})

		require.ErrorContains(t, err, "failed to push image "+postgres)
		assert.Len(t, result.BuiltImages, 1)
		assert.Empty(t, result.Images)
		assert.Empty(t, result.ChartDigest)
		assertNotInRegistry(t, reg+chartReference)
	})
}
