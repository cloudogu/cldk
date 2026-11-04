package devpush

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/chart/common"
	helmchart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	helmregistry "helm.sh/helm/v4/pkg/registry"

	"github.com/cloudogu/cldk/internal/oci"
)

// fixedNow is the time of every dev push in the tests, 2026-11-03 14:30:12 UTC given in another time zone.
var fixedNow = time.Date(2026, 11, 3, 15, 30, 12, 0, time.FixedZone("CET", 60*60))

const wantDevVersion = "3.96.4-3-dev.20261103143012"

// newTestPusher returns a pusher with the fixed clock that pushes to real registries.
func newTestPusher() *Pusher {
	p := NewPusher()
	p.now = func() time.Time { return fixedNow }

	return p
}

// newTestRegistry starts an in-memory OCI registry without TLS and returns its address host:port.
func newTestRegistry(t *testing.T) string {
	t.Helper()

	t.Setenv("DOCKER_CONFIG", t.TempDir())
	t.Setenv("HELM_CONFIG_HOME", t.TempDir())

	srv := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	t.Cleanup(srv.Close)

	return strings.TrimPrefix(srv.URL, "http://")
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

// pullChart pulls the chart at ref from a registry without TLS.
func pullChart(t *testing.T, ref string) *helmchart.Chart {
	t.Helper()

	client, err := helmregistry.NewClient(helmregistry.ClientOptPlainHTTP())
	require.NoError(t, err)

	pulled, err := client.Pull(ref)
	require.NoError(t, err)

	c, err := loader.LoadArchive(bytes.NewReader(pulled.Chart.Data))
	require.NoError(t, err)

	return c
}

// loadTestChart loads the unchanged test chart nexus.
func loadTestChart(t *testing.T) *helmchart.Chart {
	t.Helper()

	c, err := loader.LoadDir(filepath.Join("testdata", "charts", "nexus"))
	require.NoError(t, err)

	return c
}

// fileContents returns the content of the files by name.
func fileContents(files []*common.File) map[string]string {
	contents := make(map[string]string, len(files))

	for _, f := range files {
		contents[f.Name] = string(f.Data)
	}

	return contents
}

// newMockedPusher returns a pusher with the fixed clock and a mocked chart pusher, and the targets the chart pusher
// is created for.
func newMockedPusher() (*Pusher, *MockChartPusher, *[]oci.Target) {
	chartPusher := &MockChartPusher{
		PushChartFunc: func(_ context.Context, _ []byte, _ string) (string, error) {
			return "sha256:1234", nil
		},
	}

	var targets []oci.Target

	p := newTestPusher()
	p.newChartPusher = func(target oci.Target) (ChartPusher, error) {
		targets = append(targets, target)

		return chartPusher, nil
	}

	return p, chartPusher, &targets
}

// pushedRefs returns the references of all pushed charts.
func pushedRefs(chartPusher *MockChartPusher) []string {
	var refs []string

	for _, call := range chartPusher.PushChartCalls() {
		refs = append(refs, call.Ref)
	}

	return refs
}

func TestPush(t *testing.T) {
	t.Run("pushes the chart under the dev version without changing the project directory", func(t *testing.T) {
		reg := newTestRegistry(t)
		dir := writeProject(t, "namespace: official\n", nil)
		before := snapshotDir(t, dir)

		result, err := newTestPusher().Push(context.Background(), Options{Dir: dir, Registry: reg, PlainHTTP: true})

		require.NoError(t, err)
		wantRef := reg + "/official/dogu/v3/charts/nexus:" + wantDevVersion
		assert.Equal(t, wantDevVersion, result.Version)
		assert.Equal(t, wantRef, result.ChartReference)
		assert.True(t, strings.HasPrefix(result.ChartDigest, "sha256:"), result.ChartDigest)

		pushed := pullChart(t, wantRef)
		original := loadTestChart(t)
		assert.Equal(t, wantDevVersion, pushed.Metadata.Version)
		assert.Equal(t, "3.96.4", pushed.Metadata.AppVersion, "appVersion should be kept")
		assert.Equal(t, original.Values, pushed.Values, "values should be unchanged")
		assert.Equal(t, fileContents(original.Templates), fileContents(pushed.Templates), "templates should be unchanged")
		assert.Equal(t, fileContents(original.Files), fileContents(pushed.Files), "files should be unchanged")

		assert.Equal(t, before, snapshotDir(t, dir), "the project directory should be unchanged")
	})

	t.Run("pushes to the namespace of the options instead of cldk.yaml", func(t *testing.T) {
		pusher, chartPusher, _ := newMockedPusher()
		dir := writeProject(t, "namespace: official\n", nil)

		result, err := pusher.Push(context.Background(), Options{Dir: dir, Registry: "localhost:5001", Namespace: "personal"})

		require.NoError(t, err)
		assert.Equal(t, "localhost:5001/personal/dogu/v3/charts/nexus:"+wantDevVersion, result.ChartReference)
		assert.Equal(t, []string{result.ChartReference}, pushedRefs(chartPusher))
	})

	t.Run("pushes with plain HTTP and without certificate verification as given", func(t *testing.T) {
		pusher, _, targets := newMockedPusher()
		dir := writeProject(t, "namespace: official\n", nil)

		_, err := pusher.Push(context.Background(), Options{
			Dir: dir, Registry: "localhost:5001", ClusterRegistry: "k3d-registry.localhost:5000", PlainHTTP: true, Insecure: true,
		})

		require.NoError(t, err)
		assert.Equal(t, []oci.Target{{Registry: "localhost:5001", Namespace: "official", PlainHTTP: true, InsecureSkipTLSVerify: true}},
			*targets)
	})

	t.Run("keeps a prerelease of the chart version", func(t *testing.T) {
		pusher, chartPusher, _ := newMockedPusher()
		dir := writeProject(t, "namespace: official\n", map[string]string{
			"Chart.yaml": "apiVersion: v2\nname: nexus\nversion: 1.2.3-rc.1\n",
		})

		result, err := pusher.Push(context.Background(), Options{Dir: dir, Registry: "localhost:5001"})

		require.NoError(t, err)
		assert.Equal(t, "1.2.3-rc.1-dev.20261103143012", result.Version)
		assert.Len(t, chartPusher.PushChartCalls(), 1)
	})

	failures := []struct {
		name       string
		cldkYAML   string
		chartFiles map[string]string
		opts       Options
		wantErr    string
	}{
		{
			name:     "a missing project file",
			cldkYAML: "",
			wantErr:  "cldk.yaml not found",
		},
		{
			name:     "images in the project file",
			cldkYAML: "namespace: official\nimages:\n  nexus: {}\n",
			wantErr:  "images in cldk.yaml are not supported yet",
		},
		{
			name:     "a missing chart",
			cldkYAML: "namespace: official\nchart: helm\n",
			wantErr:  "failed to load chart",
		},
		{
			name:       "a chart version that is no strict semantic version",
			cldkYAML:   "namespace: official\n",
			chartFiles: map[string]string{"Chart.yaml": "apiVersion: v2\nname: nexus\nversion: 3.96\n"},
			wantErr:    `version "3.96" of chart nexus is no valid semantic version`,
		},
		{
			name:       "a chart version with build metadata",
			cldkYAML:   "namespace: official\n",
			chartFiles: map[string]string{"Chart.yaml": "apiVersion: v2\nname: nexus\nversion: 3.96.4-3+build.1\n"},
			wantErr:    `version "3.96.4-3+build.1" of chart nexus has build metadata`,
		},
		{
			name:       "lint errors",
			cldkYAML:   "namespace: official\n",
			chartFiles: map[string]string{"values.schema.json": `{"type": "object", "required": ["missingValue"]}`},
			wantErr:    "missingValue",
		},
	}

	for _, tt := range failures {
		t.Run("aborts without pushing on "+tt.name, func(t *testing.T) {
			pusher, chartPusher, _ := newMockedPusher()
			dir := writeProject(t, tt.cldkYAML, tt.chartFiles)

			if tt.cldkYAML == "" {
				require.NoError(t, os.Remove(filepath.Join(dir, "cldk.yaml")))
			}

			_, err := pusher.Push(context.Background(), Options{Dir: dir, Registry: "localhost:5001"})

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.Empty(t, chartPusher.PushChartCalls())
		})
	}

	t.Run("returns the error of the chart push", func(t *testing.T) {
		pusher := newTestPusher()
		pusher.newChartPusher = func(oci.Target) (ChartPusher, error) {
			return nil, errors.New("no registry client")
		}
		dir := writeProject(t, "namespace: official\n", nil)

		_, err := pusher.Push(context.Background(), Options{Dir: dir, Registry: "localhost:5001"})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no registry client")
	})
}
