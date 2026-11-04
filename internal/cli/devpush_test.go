package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudogu/cldk/internal/devpush"
	"github.com/cloudogu/cldk/internal/kube"
)

const devPushChartRef = "localhost:5001/official/dogu/v3/charts/nexus:3.96.4-3-dev.20261103143012"

// newSuccessfulDevPusher returns a dev pusher mock that pushes every chart as nexus 3.96.4-3-dev.20261103143012.
func newSuccessfulDevPusher() *MockDevPusher {
	return &MockDevPusher{
		PushFunc: func(_ context.Context, _ devpush.Options) (devpush.Result, error) {
			return devpush.Result{
				Version:        "3.96.4-3-dev.20261103143012",
				ChartReference: devPushChartRef,
				ChartDigest:    "sha256:1234",
				Dogu:           "nexus",
				Cluster:        kube.Info{Context: "k3d-dev1", Server: "https://0.0.0.0:6443", Namespace: "ecosystem"},
			}, nil
		},
	}
}

// isolateDevPushEnv removes the environment variables of dev-push for the test.
func isolateDevPushEnv(t *testing.T) {
	t.Helper()

	unsetEnv(t, envDevRegistry, envDevClusterRegistry, envDevNamespace, envDevPlainHTTP, envDevInsecure, envDevKubeContext,
		envDevKubeNamespace)
}

// runDevPush runs dev-push with the given arguments in the project directory dir and returns the options passed to
// the dev pusher, stdout and stderr.
func runDevPush(t *testing.T, dir string, args ...string) (opts devpush.Options, stdout, stderr string, err error) {
	t.Helper()

	pusher := newSuccessfulDevPusher()

	stdout, stderr, err = runCldkWith(t, &MockReleaser{}, pusher, append([]string{"--dir", dir, "dev-push"}, args...)...)
	if err == nil {
		require.Len(t, pusher.PushCalls(), 1)
		opts = pusher.PushCalls()[0].Opts
	} else {
		assert.Empty(t, pusher.PushCalls(), "the dev pusher should not be called on an invalid invocation")
	}

	return opts, stdout, stderr, err
}

func TestDevPushCommand(t *testing.T) {
	t.Run("passes the flags to the dev pusher", func(t *testing.T) {
		isolateDevPushEnv(t)
		dir := t.TempDir()

		opts, _, _, err := runDevPush(t, dir, "--reg", "localhost:5001", "--cluster-reg", "k3d-registry-proxy.localhost:5000",
			"--namespace", "personal", "--plain-http", "--insecure", "--kube-context", "k3d-dev2", "--kube-namespace", "dogus")

		require.NoError(t, err)
		assert.Equal(t, devpush.Options{
			Dir:             dir,
			Registry:        "localhost:5001",
			ClusterRegistry: "k3d-registry-proxy.localhost:5000",
			Namespace:       "personal",
			PlainHTTP:       true,
			Insecure:        true,
			Kube:            kube.Options{Context: "k3d-dev2", Namespace: "dogus"},
		}, opts)
	})

	t.Run("uses the defaults for omitted flags", func(t *testing.T) {
		isolateDevPushEnv(t)
		dir := t.TempDir()

		opts, _, _, err := runDevPush(t, dir, "--reg", "registry.example.com")

		require.NoError(t, err)
		assert.Equal(t, devpush.Options{Dir: dir, Registry: "registry.example.com", ClusterRegistry: "registry.example.com"}, opts)
	})

	t.Run("takes the options from the environment", func(t *testing.T) {
		isolateDevPushEnv(t)
		t.Setenv(envDevRegistry, "localhost:5001")
		t.Setenv(envDevClusterRegistry, "k3d-registry-proxy.localhost:5000")
		t.Setenv(envDevNamespace, "personal")
		t.Setenv(envDevPlainHTTP, "true")
		t.Setenv(envDevInsecure, "1")
		t.Setenv(envDevKubeContext, "k3d-dev2")
		t.Setenv(envDevKubeNamespace, "dogus")
		dir := t.TempDir()

		opts, _, _, err := runDevPush(t, dir)

		require.NoError(t, err)
		assert.Equal(t, devpush.Options{
			Dir:             dir,
			Registry:        "localhost:5001",
			ClusterRegistry: "k3d-registry-proxy.localhost:5000",
			Namespace:       "personal",
			PlainHTTP:       true,
			Insecure:        true,
			Kube:            kube.Options{Context: "k3d-dev2", Namespace: "dogus"},
		}, opts)
	})

	t.Run("takes the options from the .env of the project directory", func(t *testing.T) {
		isolateDevPushEnv(t)
		dir := writeDotEnv(t, "CLDK_DEV_REG=localhost:5001\nCLDK_DEV_CLUSTER_REG=k3d-registry-proxy.localhost:5000\nCLDK_DEV_PLAIN_HTTP=true\n")

		opts, _, _, err := runDevPush(t, dir)

		require.NoError(t, err)
		assert.Equal(t, devpush.Options{
			Dir:             dir,
			Registry:        "localhost:5001",
			ClusterRegistry: "k3d-registry-proxy.localhost:5000",
			PlainHTTP:       true,
		}, opts)
	})

	t.Run("takes the kube context from the .env of the project directory", func(t *testing.T) {
		isolateDevPushEnv(t)
		dir := writeDotEnv(t, "CLDK_DEV_REG=localhost:5001\nCLDK_DEV_KUBE_CONTEXT=k3d-dev2\nCLDK_DEV_KUBE_NAMESPACE=dogus\n")

		opts, _, _, err := runDevPush(t, dir, "--kube-namespace", "ecosystem")

		require.NoError(t, err)
		assert.Equal(t, kube.Options{Context: "k3d-dev2", Namespace: "ecosystem"}, opts.Kube)
	})

	t.Run("prints the pushed chart, the applied dogu and how to watch it", func(t *testing.T) {
		isolateDevPushEnv(t)

		_, stdout, _, err := runDevPush(t, t.TempDir(), "--reg", "localhost:5001")

		require.NoError(t, err)
		assert.Equal(t, "Pushed chart  oci://"+devPushChartRef+"\n"+
			"Applied dogu nexus 3.96.4-3-dev.20261103143012 (context k3d-dev1, namespace ecosystem)\n"+
			"Watch: kubectl --context k3d-dev1 -n ecosystem get dogu nexus -w\n", stdout)
	})

	t.Run("logs the resolved options and the cluster address on -v", func(t *testing.T) {
		isolateDevPushEnv(t)

		_, _, stderr, err := runDevPush(t, t.TempDir(), "-v", "--reg", "localhost:5001")

		require.NoError(t, err)
		assert.Contains(t, stderr, `option=reg value=localhost:5001 source="flag --reg"`)
		assert.Contains(t, stderr, `option=cluster-reg value="" source="default: --reg"`)
		assert.Contains(t, stderr, `option=namespace value="" source="default: namespace of cldk.yaml"`)
		assert.Contains(t, stderr, `option=kube-context value="" source="default: current context"`)
		assert.Contains(t, stderr, `option=kube-namespace value="" source="default: namespace of the context"`)
		assert.Contains(t, stderr, `cluster address of the dev registry" address=localhost:5001`)
	})

	t.Run("returns the error of the dev pusher", func(t *testing.T) {
		isolateDevPushEnv(t)
		pusher := &MockDevPusher{
			PushFunc: func(_ context.Context, _ devpush.Options) (devpush.Result, error) {
				return devpush.Result{}, errors.New("chart has lint errors")
			},
		}

		stdout, _, err := runCldkWith(t, &MockReleaser{}, pusher, "dev-push", "--reg", "localhost:5001")

		require.ErrorContains(t, err, "chart has lint errors")
		assert.Empty(t, stdout)
	})

	invalid := []struct {
		name    string
		env     map[string]string
		args    []string
		wantErr string
	}{
		{name: "a positional argument", args: []string{"--reg", "localhost:5001", "../nexus"}, wantErr: "dev-push takes no arguments, give the project directory with --dir"},
		{name: "a missing registry", wantErr: "the dev registry is required, give it with --reg or CLDK_DEV_REG"},
		{name: "a registry with scheme", args: []string{"--reg", "http://localhost:5001"}, wantErr: `invalid registry address "http://localhost:5001" of --reg/CLDK_DEV_REG`},
		{name: "a registry with path", args: []string{"--reg", "localhost:5001/official"}, wantErr: `invalid registry address "localhost:5001/official"`},
		{
			name:    "a cluster registry with scheme",
			args:    []string{"--reg", "localhost:5001"},
			env:     map[string]string{envDevClusterRegistry: "oci://k3d-registry-proxy.localhost:5000"},
			wantErr: "of --cluster-reg/CLDK_DEV_CLUSTER_REG",
		},
		{name: "a short flag for the namespace", args: []string{"--reg", "localhost:5001", "-n", "official"}, wantErr: "unknown shorthand flag: 'n'"},
	}

	for _, tt := range invalid {
		t.Run("aborts without pushing on "+tt.name, func(t *testing.T) {
			isolateDevPushEnv(t)

			for key, value := range tt.env {
				t.Setenv(key, value)
			}

			_, _, _, err := runDevPush(t, t.TempDir(), tt.args...)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}

	t.Run("names the environment variable of each flag in the help", func(t *testing.T) {
		stdout, _, err := runCldk(t, &MockReleaser{}, "dev-push", "--help")

		require.NoError(t, err)

		for _, env := range []string{
			envDevRegistry, envDevClusterRegistry, envDevNamespace, envDevPlainHTTP, envDevInsecure, envDevKubeContext,
			envDevKubeNamespace,
		} {
			assert.Contains(t, stdout, "(env: "+env+")")
		}
	})
}
