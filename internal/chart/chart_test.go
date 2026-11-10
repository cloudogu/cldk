package chart

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	chartutil "helm.sh/helm/v4/pkg/chart/v2/util"
)

// packChart packs the test chart testdata/charts/<name> like "helm package" does and returns the path and content of
// the archive.
func packChart(t *testing.T, name string) (path string, content []byte) {
	t.Helper()

	c, err := loader.LoadDir(filepath.Join("testdata", "charts", name))
	require.NoError(t, err)

	path, err = chartutil.Save(c, t.TempDir())
	require.NoError(t, err)

	content, err = os.ReadFile(path)
	require.NoError(t, err)

	return path, content
}

func TestLoadArchive(t *testing.T) {
	packed := func(chart string) func(t *testing.T) []byte {
		return func(t *testing.T) []byte {
			_, content := packChart(t, chart)

			return content
		}
	}

	static := func(file string) func(t *testing.T) []byte {
		return func(t *testing.T) []byte {
			content, err := os.ReadFile(filepath.Join("testdata", file))
			require.NoError(t, err)

			return content
		}
	}

	tests := []struct {
		name        string
		archive     func(t *testing.T) []byte
		wantName    string
		wantVersion string
		wantErr     string
	}{
		{name: "release version", archive: packed("nexus"), wantName: "nexus", wantVersion: "1.2.3"},
		{name: "prerelease version", archive: packed("redmine-prerelease"), wantName: "redmine", wantVersion: "6.1.2-dev.1"},
		{name: "archive without Chart.yaml", archive: static("without-chart-yaml.tgz"), wantErr: "Chart.yaml file is missing"},
		{name: "no gzipped tar archive", archive: static("not-a-chart.tgz"), wantErr: "failed to load chart archive"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := LoadArchive(tt.archive(t))

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantName, c.Metadata.Name)
			assert.Equal(t, tt.wantVersion, c.Metadata.Version)
		})
	}
}

func TestSetVersion(t *testing.T) {
	_, archive := packChart(t, "nexus")
	c, err := LoadArchive(archive)
	require.NoError(t, err)

	SetVersion(c, "1.2.3-dev.20261104120000")

	packed, err := Pack(c)
	require.NoError(t, err)

	reloaded, err := LoadArchive(packed)
	require.NoError(t, err)
	assert.Equal(t, "1.2.3-dev.20261104120000", reloaded.Metadata.Version)
	assert.Equal(t, c.Metadata.AppVersion, reloaded.Metadata.AppVersion, "appVersion should be kept")
}

func TestValidateImageRef(t *testing.T) {
	tests := []struct {
		name    string
		ref     string
		wantErr string
	}{
		{name: "registry with a dot", ref: "registry.cloudogu.com/official/nexus:3.96.4"},
		{name: "registry with a port", ref: "localhost:5001/nexus:3.96.4"},
		{name: "localhost", ref: "localhost/nexus:3.96.4"},
		{name: "digest", ref: "registry.cloudogu.com/official/nexus@sha256:" + strings.Repeat("a", 64)},
		{
			name:    "without registry",
			ref:     "usermgt:3.4.5",
			wantErr: `image "usermgt:3.4.5" must include a registry hostname, e.g. docker.io/library/usermgt:3.4.5`,
		},
		{
			name:    "with a path but without registry",
			ref:     "cloudogu/usermgt:3.4.5",
			wantErr: `image "cloudogu/usermgt:3.4.5" must include a registry hostname, e.g. docker.io/cloudogu/usermgt:3.4.5`,
		},
		{
			name:    "digest without registry",
			ref:     "cloudogu/usermgt@sha256:" + strings.Repeat("a", 64),
			wantErr: "e.g. docker.io/cloudogu/usermgt@sha256:" + strings.Repeat("a", 64),
		},
		{name: "invalid reference", ref: "registry.cloudogu.com/Nexus:3", wantErr: "invalid image reference registry.cloudogu.com/Nexus:3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateImageRef(tt.ref)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)

				return
			}

			require.NoError(t, err)
		})
	}
}

// copyChart copies the test chart testdata/charts/<name> into a new temporary directory, writes the given additional
// files into it and returns the chart directory.
func copyChart(t *testing.T, name string, files map[string]string) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.CopyFS(dir, os.DirFS(filepath.Join("testdata", "charts", name))))

	for file, content := range files {
		path := filepath.Join(dir, file)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}

	return dir
}

const chartYAMLWithDependency = `apiVersion: v2
name: nexus
version: 1.2.3
dependencies:
  - name: postgresql
    version: 14.18.0
    repository: oci://registry.example.com/charts
`

func TestLoadDir(t *testing.T) {
	t.Run("loads the chart without the files ignored by .helmignore", func(t *testing.T) {
		dir := copyChart(t, "nexus", map[string]string{"README.md": "# Nexus"})

		c, err := LoadDir(dir)

		require.NoError(t, err)
		assert.Equal(t, "nexus", c.Metadata.Name)
		assert.Equal(t, "1.2.3", c.Metadata.Version)
		assert.Nil(t, findFile(c.Files, "README.md"), "README.md is ignored by .helmignore")
		assert.NotNil(t, findFile(c.Files, mirrorPatchTemplateFile))
	})

	t.Run("loads a chart whose dependencies are in charts/", func(t *testing.T) {
		dir := copyChart(t, "nexus", map[string]string{
			"Chart.yaml":                           chartYAMLWithDependency,
			"charts/postgresql/Chart.yaml":         "apiVersion: v2\nname: postgresql\nversion: 14.18.0\n",
			"charts/postgresql/values.yaml":        "",
			"charts/postgresql/templates/.gitkeep": "",
		})

		c, err := LoadDir(dir)

		require.NoError(t, err)
		assert.Len(t, c.Dependencies(), 1)
	})

	t.Run("aborts with a hint to helm dependency build if a dependency is missing", func(t *testing.T) {
		dir := copyChart(t, "nexus", map[string]string{"Chart.yaml": chartYAMLWithDependency})

		_, err := LoadDir(dir)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "postgresql")
		assert.Contains(t, err.Error(), "helm dependency build "+dir)
	})

	t.Run("aborts if the directory is no chart", func(t *testing.T) {
		dir := t.TempDir()

		_, err := LoadDir(dir)

		require.Error(t, err)
		assert.Contains(t, err.Error(), dir)
	})
}

func TestLint(t *testing.T) {
	t.Run("passes a valid chart", func(t *testing.T) {
		c, err := LoadDir(copyChart(t, "nexus", nil))
		require.NoError(t, err)

		warnings, err := Lint(c, nil)

		require.NoError(t, err)
		assert.Empty(t, warnings)
	})

	t.Run("returns warnings without failing", func(t *testing.T) {
		// helm lint only warns about a resource name that kubernetes would reject.
		c, err := LoadDir(copyChart(t, "nexus", map[string]string{
			"templates/configmap.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: Invalid_Name\n",
		}))
		require.NoError(t, err)

		warnings, err := Lint(c, nil)

		require.NoError(t, err)
		require.NotEmpty(t, warnings)
		assert.Contains(t, warnings[0], "Invalid_Name")
	})

	t.Run("fails on errors including values.schema.json", func(t *testing.T) {
		c, err := LoadDir(copyChart(t, "nexus", map[string]string{
			"values.schema.json": `{"type": "object", "required": ["missingValue"]}`,
		}))
		require.NoError(t, err)

		_, err = Lint(c, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "missingValue")
	})

	t.Run("lints with the given values", func(t *testing.T) {
		c, err := LoadDir(copyChart(t, "nexus", map[string]string{
			"values.schema.json": `{"type": "object", "required": ["missingValue"]}`,
		}))
		require.NoError(t, err)

		_, err = Lint(c, map[string]any{"missingValue": "given"})

		require.NoError(t, err)
	})

	t.Run("lints the chart in memory with its changes", func(t *testing.T) {
		c, err := LoadDir(copyChart(t, "nexus", nil))
		require.NoError(t, err)
		findFile(c.Templates, "templates/statefulset.yaml").Data = []byte("replicas: {{ .Values.missing.replicas }}\n")

		_, err = Lint(c, nil)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing")
	})
}
