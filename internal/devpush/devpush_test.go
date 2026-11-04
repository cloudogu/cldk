package devpush

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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

	"github.com/cloudogu/cldk/internal/dogu"
	"github.com/cloudogu/cldk/internal/kube"
	"github.com/cloudogu/cldk/internal/oci"
)

// fixedNow is the time of every dev push in the tests, 2026-11-03 14:30:12 UTC given in another time zone.
var fixedNow = time.Date(2026, 11, 3, 15, 30, 12, 0, time.FixedZone("CET", 60*60))

const wantDevVersion = "3.96.4-3-dev.20261103143012"

// testCluster is the cluster of the mocked dogu client.
var testCluster = kube.Info{Context: "k3d-dev1", Server: "https://0.0.0.0:6443", Namespace: "ecosystem"}

// newTestPusher returns a pusher with the fixed clock that pushes to real registries and applies to a mocked cluster
// without the dogu.
func newTestPusher() *Pusher {
	p := NewPusher()
	p.now = func() time.Time { return fixedNow }
	p.newDoguClient = func(kube.Options) (DoguClient, kube.Info, error) {
		return newTestDoguClient(), testCluster, nil
	}

	return p
}

// newTestDoguClient returns a mocked dogu client that applies every dogu.
func newTestDoguClient() *MockDoguClient {
	return &MockDoguClient{
		ApplyFunc: func(context.Context, dogu.DevChart) error {
			return nil
		},
	}
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

// mockedPusher is a pusher with the fixed clock, a mocked chart pusher and a mocked cluster without the dogu.
type mockedPusher struct {
	*Pusher
	chartPusher *MockChartPusher
	doguClient  *MockDoguClient
	// targets are the targets the chart pusher is created for.
	targets []oci.Target
	// kubeOptions are the options the dogu client is created for.
	kubeOptions []kube.Options
	// steps are the pushes and applies in their order.
	steps []string
}

// newMockedPusher returns a pusher with the fixed clock, a mocked chart pusher and a mocked cluster without the dogu.
func newMockedPusher() *mockedPusher {
	m := &mockedPusher{Pusher: newTestPusher(), doguClient: newTestDoguClient()}

	m.chartPusher = &MockChartPusher{
		PushChartFunc: func(_ context.Context, _ []byte, ref string) (string, error) {
			m.steps = append(m.steps, "push "+ref)

			return "sha256:1234", nil
		},
	}
	m.doguClient.ApplyFunc = func(_ context.Context, chart dogu.DevChart) error {
		m.steps = append(m.steps, "apply "+chart.Name)

		return nil
	}

	m.newChartPusher = func(target oci.Target) (ChartPusher, error) {
		m.targets = append(m.targets, target)

		return m.chartPusher, nil
	}
	m.newDoguClient = func(opts kube.Options) (DoguClient, kube.Info, error) {
		m.kubeOptions = append(m.kubeOptions, opts)

		return m.doguClient, testCluster, nil
	}

	return m
}

// appliedDevCharts returns the dev charts the dogu has been applied with.
func (m *mockedPusher) appliedDevCharts() []dogu.DevChart {
	var charts []dogu.DevChart

	for _, call := range m.doguClient.ApplyCalls() {
		charts = append(charts, call.Chart)
	}

	return charts
}

// pushedRefs returns the references of all pushed charts.
func (m *mockedPusher) pushedRefs() []string {
	var refs []string

	for _, call := range m.chartPusher.PushChartCalls() {
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
		pusher := newMockedPusher()
		dir := writeProject(t, "namespace: official\n", nil)

		result, err := pusher.Push(context.Background(), Options{Dir: dir, Registry: "localhost:5001", Namespace: "personal"})

		require.NoError(t, err)
		assert.Equal(t, "localhost:5001/personal/dogu/v3/charts/nexus:"+wantDevVersion, result.ChartReference)
		assert.Equal(t, []string{result.ChartReference}, pusher.pushedRefs())
	})

	t.Run("pushes with plain HTTP and without certificate verification as given", func(t *testing.T) {
		pusher := newMockedPusher()
		dir := writeProject(t, "namespace: official\n", nil)

		_, err := pusher.Push(context.Background(), Options{
			Dir: dir, Registry: "localhost:5001", ClusterRegistry: "k3d-registry.localhost:5000", PlainHTTP: true, Insecure: true,
		})

		require.NoError(t, err)
		assert.Equal(t, []oci.Target{{Registry: "localhost:5001", Namespace: "official", PlainHTTP: true, InsecureSkipTLSVerify: true}},
			pusher.targets)
	})

	t.Run("keeps a prerelease of the chart version", func(t *testing.T) {
		pusher := newMockedPusher()
		dir := writeProject(t, "namespace: official\n", map[string]string{
			"Chart.yaml": "apiVersion: v2\nname: nexus\nversion: 1.2.3-rc.1\n",
		})

		result, err := pusher.Push(context.Background(), Options{Dir: dir, Registry: "localhost:5001"})

		require.NoError(t, err)
		assert.Equal(t, "1.2.3-rc.1-dev.20261103143012", result.Version)
		assert.Len(t, pusher.chartPusher.PushChartCalls(), 1)
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
			pusher := newMockedPusher()
			dir := writeProject(t, tt.cldkYAML, tt.chartFiles)

			if tt.cldkYAML == "" {
				require.NoError(t, os.Remove(filepath.Join(dir, "cldk.yaml")))
			}

			_, err := pusher.Push(context.Background(), Options{Dir: dir, Registry: "localhost:5001"})

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.Empty(t, pusher.chartPusher.PushChartCalls())
			assert.Empty(t, pusher.appliedDevCharts())
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

	t.Run("applies the dogu with the dev chart after pushing the chart", func(t *testing.T) {
		pusher := newMockedPusher()
		dir := writeProject(t, "namespace: official\n", nil)
		kubeOpts := kube.Options{Context: "k3d-dev1", Namespace: "ecosystem"}

		result, err := pusher.Push(context.Background(), Options{
			Dir: dir, Registry: "localhost:5001", ClusterRegistry: "k3d-registry-proxy.localhost:5000", Kube: kubeOpts,
		})

		require.NoError(t, err)
		assert.Equal(t, []kube.Options{kubeOpts}, pusher.kubeOptions)
		assert.Equal(t, []string{"push " + result.ChartReference, "apply nexus"}, pusher.steps)
		assert.Equal(t, []dogu.DevChart{{
			Name:      "nexus",
			Namespace: "official",
			Version:   wantDevVersion,
			URL:       "oci://k3d-registry-proxy.localhost:5000/official/dogu/v3/charts/nexus",
		}}, pusher.appliedDevCharts())
		assert.Equal(t, "nexus", result.Dogu)
		assert.Equal(t, testCluster, result.Cluster)
	})

	t.Run("applies the dev chart in the namespace of the options", func(t *testing.T) {
		pusher := newMockedPusher()
		dir := writeProject(t, "namespace: official\n", nil)

		_, err := pusher.Push(context.Background(), Options{
			Dir: dir, Registry: "localhost:5001", ClusterRegistry: "localhost:5001", Namespace: "personal",
		})

		require.NoError(t, err)
		require.Len(t, pusher.appliedDevCharts(), 1)
		assert.Equal(t, "personal", pusher.appliedDevCharts()[0].Namespace)
		assert.Equal(t, "oci://localhost:5001/personal/dogu/v3/charts/nexus", pusher.appliedDevCharts()[0].URL)
	})

	for _, opts := range []Options{{PlainHTTP: true}, {Insecure: true}} {
		t.Run(fmt.Sprintf("marks the dev chart as insecure with %+v", opts), func(t *testing.T) {
			pusher := newMockedPusher()
			opts.Dir = writeProject(t, "namespace: official\n", nil)
			opts.Registry = "localhost:5001"

			_, err := pusher.Push(context.Background(), opts)

			require.NoError(t, err)
			require.Len(t, pusher.appliedDevCharts(), 1)
			assert.True(t, pusher.appliedDevCharts()[0].Insecure)
		})
	}

	t.Run("aborts before linting and pushing on an invalid kubeconfig", func(t *testing.T) {
		pusher := newMockedPusher()
		pusher.newDoguClient = func(kube.Options) (DoguClient, kube.Info, error) {
			return nil, kube.Info{}, errors.New(`context "dev3" not found`)
		}
		// The chart has lint errors, connecting to the cluster must abort first.
		dir := writeProject(t, "namespace: official\n", map[string]string{
			"values.schema.json": `{"type": "object", "required": ["missingValue"]}`,
		})

		_, err := pusher.Push(context.Background(), Options{Dir: dir, Registry: "localhost:5001"})

		require.ErrorContains(t, err, `failed to connect to the cluster: context "dev3" not found`)
		assert.Empty(t, pusher.chartPusher.PushChartCalls())
		assert.Empty(t, pusher.appliedDevCharts())
	})

	t.Run("returns the error of the apply after pushing the chart", func(t *testing.T) {
		pusher := newMockedPusher()
		pusher.doguClient.ApplyFunc = func(context.Context, dogu.DevChart) error {
			return errors.New("admission webhook denied")
		}
		dir := writeProject(t, "namespace: official\n", nil)

		_, err := pusher.Push(context.Background(), Options{Dir: dir, Registry: "localhost:5001"})

		require.ErrorContains(t, err, "pushed chart localhost:5001/official/dogu/v3/charts/nexus:"+wantDevVersion+", but admission webhook denied")
		assert.Len(t, pusher.chartPusher.PushChartCalls(), 1)
	})
}
