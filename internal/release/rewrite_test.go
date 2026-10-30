package release

import (
	"bytes"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/chart/common"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"sigs.k8s.io/yaml"
)

// chartFile returns the content of the file with the given name inside a packed chart archive.
func chartFile(t *testing.T, archive []byte, name string) []byte {
	t.Helper()

	c, err := loader.LoadArchive(bytes.NewReader(archive))
	require.NoError(t, err)

	for _, f := range c.Raw {
		if f.Name == name {
			return f.Data
		}
	}

	require.Failf(t, "file not found in chart", "%s", name)

	return nil
}

// chartYAML returns the parsed content of the YAML file with the given name inside a packed chart archive.
func chartYAML(t *testing.T, archive []byte, name string) map[string]any {
	t.Helper()

	var content map[string]any
	require.NoError(t, yaml.Unmarshal(chartFile(t, archive, name), &content))

	return content
}

// packChartWith packs the test chart testdata/charts/<name> after modifying it and returns the archive.
func packChartWith(t *testing.T, name string, modify func(c *chart.Chart)) []byte {
	t.Helper()

	c, err := loader.LoadDir(filepath.Join("testdata", "charts", name))
	require.NoError(t, err)

	modify(c)

	archive, err := packArchive(c)
	require.NoError(t, err)

	return archive
}

// withMirrorPatchTemplate replaces the mirror patch template of a chart.
func withMirrorPatchTemplate(content string) func(c *chart.Chart) {
	return func(c *chart.Chart) {
		findFile(c.Files, mirrorPatchTemplateFile).Data = []byte(content)
	}
}

func TestRewriteChart(t *testing.T) {
	const nexusTarget = "registry.example.com/testing/dogu/v3/images/nexus:3.96.4-3"

	t.Run("writes the target reference into the image list of the mirror patch template", func(t *testing.T) {
		_, archive := packChart(t, "nexus")

		rewritten, _, err := rewriteChart(archive, map[string]string{"nexus": nexusTarget})

		require.NoError(t, err)
		assert.Equal(t, map[string]any{
			"nexus":          nexusTarget,
			"nexusSaManager": "registry.cloudogu.com/k8s/service-account-producer-sidecar:0.1.3",
			"postgresql":     "docker.io/library/postgres:14.18",
		}, chartYAML(t, rewritten, mirrorPatchTemplateFile)["values"].(map[string]any)["images"])
	})

	t.Run("renders the patches with the target reference into values.yaml", func(t *testing.T) {
		_, archive := packChart(t, "nexus")

		rewritten, _, err := rewriteChart(archive, map[string]string{"nexus": nexusTarget})

		require.NoError(t, err)
		values := chartYAML(t, rewritten, valuesFile)
		assert.Equal(t, map[string]any{
			"replicas": float64(1),
			"image": map[string]any{
				"registry":   "registry.example.com",
				"repository": "testing/dogu/v3/images/nexus",
				"tag":        "3.96.4-3",
			},
			"saManager": map[string]any{
				"image": map[string]any{
					"registry":   "registry.cloudogu.com",
					"repository": "k8s/service-account-producer-sidecar",
					"tag":        "0.1.3",
				},
			},
		}, values["nexus"])
		// Like in ces-mirror, the patches of images that are not released are rendered too, with normalized references.
		assert.Equal(t, map[string]any{
			"replicas": float64(1),
			"image": map[string]any{
				"registry":   "index.docker.io",
				"repository": "library/postgres",
				"tag":        "14.18",
			},
		}, values["postgresql"])
	})

	t.Run("rewrites several images", func(t *testing.T) {
		_, archive := packChart(t, "nexus")
		postgresTarget := "registry.example.com/testing/dogu/v3/images/postgres:14.18"

		rewritten, changes, err := rewriteChart(archive, map[string]string{"nexus": nexusTarget, "postgres": postgresTarget})

		require.NoError(t, err)
		assert.Equal(t, []imageChange{
			{Key: "nexus", Old: "registry.cloudogu.com/official/nexus:3.96.4-3", New: nexusTarget},
			{Key: "postgresql", Old: "docker.io/library/postgres:14.18", New: postgresTarget},
		}, changes)

		values := chartYAML(t, rewritten, valuesFile)
		assert.Equal(t, "testing/dogu/v3/images/postgres", values["postgresql"].(map[string]any)["image"].(map[string]any)["repository"])
	})

	t.Run("returns the changed entries of values.images", func(t *testing.T) {
		_, archive := packChart(t, "nexus")

		_, changes, err := rewriteChart(archive, map[string]string{"nexus": nexusTarget})

		require.NoError(t, err)
		assert.Equal(t, []imageChange{{Key: "nexus", Old: "registry.cloudogu.com/official/nexus:3.96.4-3", New: nexusTarget}}, changes)
	})

	t.Run("keeps the patches of the mirror patch template", func(t *testing.T) {
		_, archive := packChart(t, "nexus")

		rewritten, _, err := rewriteChart(archive, map[string]string{"nexus": nexusTarget})

		require.NoError(t, err)

		var original map[string]any
		require.NoError(t, yaml.Unmarshal(chartFile(t, archive, mirrorPatchTemplateFile), &original))

		tpl := chartYAML(t, rewritten, mirrorPatchTemplateFile)
		assert.Equal(t, "v1", tpl["apiVersion"])
		assert.Equal(t, original["patches"], tpl["patches"])
	})

	t.Run("keeps all other files unchanged", func(t *testing.T) {
		_, archive := packChart(t, "nexus")

		rewritten, _, err := rewriteChart(archive, map[string]string{"nexus": nexusTarget})

		require.NoError(t, err)

		for _, name := range []string{"runtime-patch-tpl.yaml", "templates/statefulset.yaml", "Chart.yaml"} {
			assert.Equal(t, string(chartFile(t, archive, name)), string(chartFile(t, rewritten, name)), name)
		}
	})

	t.Run("patches a template of the chart", func(t *testing.T) {
		archive := packChartWith(t, "nexus", func(c *chart.Chart) {
			c.Templates = append(c.Templates, &common.File{Name: "templates/images.yaml", Data: []byte("nexus:\n  image: old\n  keep: true\n")})
			withMirrorPatchTemplate(`apiVersion: v1
values:
  images:
    nexus: registry.cloudogu.com/official/nexus:3.96.4-3
patches:
  templates/images.yaml:
    nexus:
      image: "{{ .images.nexus }}"
`)(c)
		})

		rewritten, _, err := rewriteChart(archive, map[string]string{"nexus": nexusTarget})

		require.NoError(t, err)
		assert.Equal(t, map[string]any{"nexus": map[string]any{"image": nexusTarget, "keep": true}}, chartYAML(t, rewritten, "templates/images.yaml"))
	})

	t.Run("fails", func(t *testing.T) {
		tests := []struct {
			name    string
			modify  func(c *chart.Chart)
			targets map[string]string
			wantErr string
		}{
			{
				name: "without mirror patch template",
				modify: func(c *chart.Chart) {
					c.Files = slices.DeleteFunc(c.Files, func(f *common.File) bool { return f.Name == mirrorPatchTemplateFile })
				},
				wantErr: "chart has no mirror-patch-tpl.yaml",
			},
			{
				name:    "if no entry of values.images has the target name",
				targets: map[string]string{"usermgt": "registry.example.com/testing/dogu/v3/images/usermgt:1.0.0"},
				wantErr: `no entry in values.images of mirror-patch-tpl.yaml has the repository name "usermgt"`,
			},
			{
				name: "if several entries of values.images have the target name",
				modify: withMirrorPatchTemplate(`apiVersion: v1
values:
  images:
    nexus: registry.cloudogu.com/official/nexus:3.96.4-3
    nexusOld: registry.cloudogu.com/official/nexus:3.70.0-1
`),
				wantErr: `several entries in values.images of mirror-patch-tpl.yaml have the repository name "nexus" of image registry.example.com/testing/dogu/v3/images/nexus:3.96.4-3: nexus, nexusOld`,
			},
			{
				name:    "without values.images",
				modify:  withMirrorPatchTemplate("apiVersion: v1\npatches: {}\n"),
				wantErr: "mirror-patch-tpl.yaml has no map values.images",
			},
			{
				name:    "with an unsupported apiVersion",
				modify:  withMirrorPatchTemplate("apiVersion: v2\nvalues:\n  images:\n    nexus: registry.cloudogu.com/official/nexus:3.96.4-3\n"),
				wantErr: `unsupported apiVersion "v2" in mirror-patch-tpl.yaml`,
			},
			{
				name: "if a patch refers to an unknown image",
				modify: withMirrorPatchTemplate(`apiVersion: v1
values:
  images:
    nexus: registry.cloudogu.com/official/nexus:3.96.4-3
patches:
  values.yaml:
    image: "{{ registryFrom .images.unknown }}"
`),
				wantErr: "failed to render patches of mirror-patch-tpl.yaml",
			},
			{
				name: "if a patch refers to a file that does not exist",
				modify: withMirrorPatchTemplate(`apiVersion: v1
values:
  images:
    nexus: registry.cloudogu.com/official/nexus:3.96.4-3
patches:
  templates/missing.yaml:
    image: "{{ .images.nexus }}"
`),
				wantErr: "failed to patch templates/missing.yaml: file not found in chart",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				modify := tt.modify
				if modify == nil {
					modify = func(*chart.Chart) {}
				}

				targets := tt.targets
				if targets == nil {
					targets = map[string]string{"nexus": nexusTarget}
				}

				_, _, err := rewriteChart(packChartWith(t, "nexus", modify), targets)

				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			})
		}
	})
}
