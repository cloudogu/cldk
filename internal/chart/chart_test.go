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
		{name: "release version", archive: packed("nexus"), wantName: "nexus", wantVersion: "3.96.4-3"},
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

	SetVersion(c, "3.96.4-3-dev.20261104120000")

	packed, err := Pack(c)
	require.NoError(t, err)

	reloaded, err := LoadArchive(packed)
	require.NoError(t, err)
	assert.Equal(t, "3.96.4-3-dev.20261104120000", reloaded.Metadata.Version)
	assert.Equal(t, c.Metadata.AppVersion, reloaded.Metadata.AppVersion, "appVersion should be kept")
}

func TestValidateImageRef(t *testing.T) {
	tests := []struct {
		name    string
		ref     string
		wantErr string
	}{
		{name: "registry with a dot", ref: "registry.cloudogu.com/official/nexus:3.96.4-3"},
		{name: "registry with a port", ref: "localhost:5001/nexus:3.96.4-3"},
		{name: "localhost", ref: "localhost/nexus:3.96.4-3"},
		{name: "digest", ref: "registry.cloudogu.com/official/nexus@sha256:" + strings.Repeat("a", 64)},
		{
			name:    "without registry",
			ref:     "usermgt:3.4.5",
			wantErr: "image usermgt:3.4.5 has no registry, give it explicitly, e.g. docker.io/library/usermgt:3.4.5",
		},
		{
			name:    "with a path but without registry",
			ref:     "cloudogu/usermgt:3.4.5",
			wantErr: "image cloudogu/usermgt:3.4.5 has no registry, give it explicitly, e.g. docker.io/cloudogu/usermgt:3.4.5",
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
