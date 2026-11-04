package devpush

import (
	"bytes"
	"context"
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

	"github.com/cloudogu/cldk/internal/build"
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
	p.newBuilder = func(string) Builder {
		return newTestBuilder()
	}

	return p
}

// newTestBuilder returns a mocked builder that builds and pushes every image.
func newTestBuilder() *MockBuilder {
	return &MockBuilder{
		CheckFunc: func(context.Context) error {
			return nil
		},
		BuildFunc: func(context.Context, build.BuildRequest) error {
			return nil
		},
		PushFunc: func(context.Context, string) error {
			return nil
		},
	}
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
	builder     *MockBuilder
	// builderBinaries are the build CLIs the builder is created for.
	builderBinaries []string
	// targets are the targets the chart pusher is created for.
	targets []oci.Target
	// kubeOptions are the options the dogu client is created for.
	kubeOptions []kube.Options
	// steps are the pushes and applies in their order.
	steps []string
}

// newMockedPusher returns a pusher with the fixed clock, a mocked chart pusher and a mocked cluster without the dogu.
func newMockedPusher() *mockedPusher {
	m := &mockedPusher{Pusher: newTestPusher(), doguClient: newTestDoguClient(), builder: newTestBuilder()}

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
	m.builder.BuildFunc = func(_ context.Context, req build.BuildRequest) error {
		m.steps = append(m.steps, "build "+req.Tag)

		return nil
	}
	m.builder.PushFunc = func(_ context.Context, ref string) error {
		m.steps = append(m.steps, "push "+ref)

		return nil
	}

	m.newBuilder = func(binary string) Builder {
		m.builderBinaries = append(m.builderBinaries, binary)

		return m.builder
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

// abortCase is a project that a dev push must reject before building or pushing anything.
type abortCase struct {
	name string
	// cldkYAML is the project file; without one, the project has no cldk.yaml.
	cldkYAML string
	// chartFiles overwrite files of the test chart.
	chartFiles map[string]string
	wantErr    string
}

// assertAbortsWithoutPushing checks that a dev push of the project fails with the error and has built, pushed and
// applied nothing.
func assertAbortsWithoutPushing(t *testing.T, tt abortCase) {
	t.Helper()

	pusher := newMockedPusher()
	dir := writeProject(t, tt.cldkYAML, tt.chartFiles)

	if tt.cldkYAML == "" {
		require.NoError(t, os.Remove(filepath.Join(dir, "cldk.yaml")))
	}

	_, err := pusher.Push(context.Background(), Options{Dir: dir, Registry: "localhost:5001"})

	require.ErrorContains(t, err, tt.wantErr)
	assert.Empty(t, pusher.steps)
}
