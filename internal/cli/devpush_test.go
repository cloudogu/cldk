package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudogu/cldk/internal/devpush"
	"github.com/cloudogu/cldk/internal/dogu"
	"github.com/cloudogu/cldk/internal/kube"
	"github.com/cloudogu/cldk/internal/project"
)

const devPushChartRef = "localhost:5001/official/dogu/v3/charts/nexus:1.2.3-dev.20261103143012"

// newSuccessfulDevPusher returns a dev pusher mock that pushes every chart as nexus 1.2.3-dev.20261103143012.
func newSuccessfulDevPusher() *MockDevPusher {
	return &MockDevPusher{
		PushFunc: func(_ context.Context, _ devpush.Options) (devpush.Result, error) {
			return devpush.Result{
				Version:        "1.2.3-dev.20261103143012",
				ChartReference: devPushChartRef,
				ChartDigest:    "sha256:1234",
				Dogu:           "nexus",
				Cluster:        kube.Info{Context: "k3d-dev1", Server: "https://0.0.0.0:6443", Namespace: "ecosystem"},
			}, nil
		},
		PlanFunc: func(_ context.Context, _ devpush.Options) (devpush.Plan, error) {
			return devpush.Plan{
				Version: "1.2.3-dev.20261103143012",
				Images: []devpush.PushedImage{
					{Name: "nexus", Reference: "localhost:5001/official/dogu/v3/images/nexus:1.2.3-dev.20261103143012"},
				},
				ReplacedImages: []devpush.ReplacedImage{{Name: "sidecar", Reference: "registry.example.com/foo:1.0"}},
				SkippedImages:  []string{"postgres"},
				ChartReference: devPushChartRef,
				DevChart: dogu.DevChart{
					Name:      "nexus",
					Namespace: "official",
					Version:   "1.2.3-dev.20261103143012",
					URL:       "oci://k3d-registry-proxy.localhost:5000/official/dogu/v3/charts/nexus",
				},
				Cluster: kube.Info{Context: "k3d-dev1", Server: "https://0.0.0.0:6443", Namespace: "ecosystem"},
			}, nil
		},
	}
}

// isolateDevPushEnv removes the environment variables of dev-push for the test.
func isolateDevPushEnv(t *testing.T) {
	t.Helper()

	unsetEnv(t, envDevRegistry, envDevClusterRegistry, envDevNamespace, envDevPlainHTTP, envDevInsecure, envDevKubeContext,
		envDevKubeNamespace, envDevValues, envBuilder, envPlatform, envNoCache)
}

// runDevPush runs dev-push with the given arguments in the project directory dir and returns the options passed to
// the dev pusher, stdout and stderr.
func runDevPush(t *testing.T, dir string, args ...string) (opts devpush.Options, stdout, stderr string, err error) {
	t.Helper()

	pusher := newSuccessfulDevPusher()

	stdout, stderr, err = runCldkWith(t, &MockReleaser{}, pusher, append([]string{"--dir", dir, "dogu", "dev-push"}, args...)...)
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
			"--namespace", "personal", "--plain-http", "--insecure", "--kube-context", "k3d-dev2", "--kube-namespace", "dogus",
			"--builder", "podman", "--platform", "linux/arm64", "--no-cache", "--values", "dev-values.yaml", "--build-arg", "FOO=bar", "--build-arg", "HTTP_PROXY")

		require.NoError(t, err)
		assert.Equal(t, devpush.Options{
			Dir:             dir,
			Registry:        "localhost:5001",
			ClusterRegistry: "k3d-registry-proxy.localhost:5000",
			Namespace:       "personal",
			PlainHTTP:       true,
			Insecure:        true,
			Kube:            kube.Options{Context: "k3d-dev2", Namespace: "dogus"},
			Build: project.BuildOptions{
				Builder: "podman", Platform: "linux/arm64", NoCache: true, BuildArgs: []string{"FOO=bar", "HTTP_PROXY"},
			},
			ValuesFile: "dev-values.yaml",
		}, opts)
	})

	t.Run("uses the defaults for omitted flags", func(t *testing.T) {
		isolateDevPushEnv(t)
		dir := t.TempDir()

		opts, _, _, err := runDevPush(t, dir, "--reg", "registry.example.com")

		require.NoError(t, err)
		assert.Equal(t, devpush.Options{
			Dir: dir, Registry: "registry.example.com", ClusterRegistry: "registry.example.com",
			Build: project.BuildOptions{Builder: "docker"},
		}, opts)
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
		t.Setenv(envBuilder, "nerdctl")
		t.Setenv(envPlatform, "linux/amd64")
		t.Setenv(envNoCache, "true")
		t.Setenv(envDevValues, "local/values.yaml")
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
			Build:           project.BuildOptions{Builder: "nerdctl", Platform: "linux/amd64", NoCache: true},
			ValuesFile:      "local/values.yaml",
		}, opts)
	})

	t.Run("ignores the former build variables CLDK_DEV_BUILDER, CLDK_DEV_PLATFORM and CLDK_DEV_NO_CACHE", func(t *testing.T) {
		isolateDevPushEnv(t)
		t.Setenv("CLDK_DEV_BUILDER", "nerdctl")
		t.Setenv("CLDK_DEV_PLATFORM", "linux/amd64")
		t.Setenv("CLDK_DEV_NO_CACHE", "true")

		opts, _, _, err := runDevPush(t, t.TempDir(), "--reg", "localhost:5001")

		require.NoError(t, err)
		assert.Equal(t, project.BuildOptions{Builder: "docker"}, opts.Build)
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
			Build:           project.BuildOptions{Builder: "docker"},
		}, opts)
	})

	t.Run("takes the kube context from the .env of the project directory", func(t *testing.T) {
		isolateDevPushEnv(t)
		dir := writeDotEnv(t, "CLDK_DEV_REG=localhost:5001\nCLDK_DEV_KUBE_CONTEXT=k3d-dev2\nCLDK_DEV_KUBE_NAMESPACE=dogus\n")

		opts, _, _, err := runDevPush(t, dir, "--kube-namespace", "ecosystem")

		require.NoError(t, err)
		assert.Equal(t, kube.Options{Context: "k3d-dev2", Namespace: "ecosystem"}, opts.Kube)
	})

	t.Run("passes the replaced and skipped images to the dev pusher", func(t *testing.T) {
		isolateDevPushEnv(t)

		opts, _, _, err := runDevPush(t, t.TempDir(), "--reg", "localhost:5001",
			"--replace-image", "sidecar=registry.example.com/foo/sidecar:1.0",
			"--replace-image", "exporter=localhost:5001/official/exporter@sha256:"+strings.Repeat("a", 64),
			"--skip-image", "postgres", "--skip-image", "redis")

		require.NoError(t, err)
		assert.Equal(t, map[string]string{
			"sidecar":  "registry.example.com/foo/sidecar:1.0",
			"exporter": "localhost:5001/official/exporter@sha256:" + strings.Repeat("a", 64),
		}, opts.ReplaceImages)
		assert.Equal(t, []string{"postgres", "redis"}, opts.SkipImages)
	})

	t.Run("passes neither replaced nor skipped images without the flags", func(t *testing.T) {
		isolateDevPushEnv(t)

		opts, _, _, err := runDevPush(t, t.TempDir(), "--reg", "localhost:5001")

		require.NoError(t, err)
		assert.Nil(t, opts.ReplaceImages)
		assert.Nil(t, opts.SkipImages)
	})

	t.Run("takes no build arguments from the environment", func(t *testing.T) {
		isolateDevPushEnv(t)
		t.Setenv("CLDK_DEV_BUILD_ARG", "FOO=bar")

		opts, _, _, err := runDevPush(t, t.TempDir(), "--reg", "localhost:5001")

		require.NoError(t, err)
		assert.Empty(t, opts.Build.BuildArgs)
	})

	t.Run("prints the pushed images before the pushed chart", func(t *testing.T) {
		isolateDevPushEnv(t)
		pusher := newSuccessfulDevPusher()
		successful := pusher.PushFunc
		pusher.PushFunc = func(ctx context.Context, opts devpush.Options) (devpush.Result, error) {
			result, err := successful(ctx, opts)
			result.Images = []devpush.PushedImage{
				{Name: "nexus", Reference: "localhost:5001/official/dogu/v3/images/nexus:1.2.3-dev.20261103143012"},
				{Name: "sidecar", Reference: "localhost:5001/official/dogu/v3/images/sidecar:1.2.3-dev.20261103143012"},
			}

			return result, err
		}

		stdout, _, err := runCldkWith(t, &MockReleaser{}, pusher, "dogu", "dev-push", "--reg", "localhost:5001")

		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(stdout,
			"Pushed image  localhost:5001/official/dogu/v3/images/nexus:1.2.3-dev.20261103143012\n"+
				"Pushed image  localhost:5001/official/dogu/v3/images/sidecar:1.2.3-dev.20261103143012\n"+
				"Pushed chart  oci://"+devPushChartRef+"\n"), stdout)
	})

	t.Run("prints the replaced and skipped images after the pushed images", func(t *testing.T) {
		isolateDevPushEnv(t)
		pusher := newSuccessfulDevPusher()
		successful := pusher.PushFunc
		pusher.PushFunc = func(ctx context.Context, opts devpush.Options) (devpush.Result, error) {
			result, err := successful(ctx, opts)
			result.Images = []devpush.PushedImage{
				{Name: "nexus", Reference: "localhost:5001/official/dogu/v3/images/nexus:1.2.3-dev.20261103143012"},
			}
			result.ReplacedImages = []devpush.ReplacedImage{{Name: "sidecar", Reference: "registry.example.com/foo:1.0"}}
			result.SkippedImages = []string{"postgres"}

			return result, err
		}

		stdout, _, err := runCldkWith(t, &MockReleaser{}, pusher, "dogu", "dev-push", "--reg", "localhost:5001")

		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(stdout,
			"Pushed image  localhost:5001/official/dogu/v3/images/nexus:1.2.3-dev.20261103143012\n"+
				"Replaced image sidecar -> registry.example.com/foo:1.0\n"+
				"Skipped image postgres\n"+
				"Pushed chart  oci://"+devPushChartRef+"\n"), stdout)
	})

	t.Run("prints the pushed chart, the applied dogu and how to watch it", func(t *testing.T) {
		isolateDevPushEnv(t)

		_, stdout, _, err := runDevPush(t, t.TempDir(), "--reg", "localhost:5001")

		require.NoError(t, err)
		assert.Equal(t, "Pushed chart  oci://"+devPushChartRef+"\n"+
			"Applied dogu nexus 1.2.3-dev.20261103143012 (context k3d-dev1, namespace ecosystem)\n"+
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

		stdout, _, err := runCldkWith(t, &MockReleaser{}, pusher, "dogu", "dev-push", "--reg", "localhost:5001")

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
		{name: "a build argument without key", args: []string{"--reg", "localhost:5001", "--build-arg", "=bar"}, wantErr: `invalid build argument "=bar" of --build-arg: must be given as KEY=VALUE or KEY`},
		{name: "an empty build argument", args: []string{"--reg", "localhost:5001", "--build-arg", ""}, wantErr: `invalid build argument "" of --build-arg`},
		{name: "a build argument key with a space", args: []string{"--reg", "localhost:5001", "--build-arg", "FOO BAR=1"}, wantErr: `invalid build argument "FOO BAR=1"`},
		{name: "a replaced image without reference", args: []string{"--reg", "localhost:5001", "--replace-image", "sidecar"}, wantErr: `invalid image replacement "sidecar" of --replace-image: must be given as <name>=<reference>`},
		{name: "a replaced image with an empty reference", args: []string{"--reg", "localhost:5001", "--replace-image", "sidecar="}, wantErr: `invalid image replacement "sidecar=" of --replace-image`},
		{name: "a replaced image without name", args: []string{"--reg", "localhost:5001", "--replace-image", "=registry.example.com/foo:1.0"}, wantErr: `invalid image replacement "=registry.example.com/foo:1.0" of --replace-image`},
		{name: "a replaced image without registry", args: []string{"--reg", "localhost:5001", "--replace-image", "sidecar=foo/sidecar:1.0"}, wantErr: `image "foo/sidecar:1.0" must include a registry hostname, e.g. docker.io/foo/sidecar:1.0`},
		{name: "an invalid replaced reference", args: []string{"--reg", "localhost:5001", "--replace-image", "sidecar=registry.example.com/Foo:1.0"}, wantErr: "invalid image reference registry.example.com/Foo:1.0"},
		{name: "an image replaced twice", args: []string{"--reg", "localhost:5001", "--replace-image", "sidecar=registry.example.com/a:1", "--replace-image", "sidecar=registry.example.com/b:1"}, wantErr: `image "sidecar" is given more than once in --replace-image`},
		{name: "an image skipped twice", args: []string{"--reg", "localhost:5001", "--skip-image", "postgres", "--skip-image", "postgres"}, wantErr: `image "postgres" is given more than once in --skip-image`},
		{name: "an image both replaced and skipped", args: []string{"--reg", "localhost:5001", "--replace-image", "sidecar=registry.example.com/a:1", "--skip-image", "sidecar"}, wantErr: `image "sidecar" is given in both --replace-image and --skip-image`},
		{name: "an empty skipped image", args: []string{"--reg", "localhost:5001", "--skip-image", ""}, wantErr: `invalid image name "" of --skip-image`},
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
		stdout, _, err := runCldk(t, &MockReleaser{}, "dogu", "dev-push", "--help")

		require.NoError(t, err)

		for _, env := range []string{
			envDevRegistry, envDevClusterRegistry, envDevNamespace, envDevPlainHTTP, envDevInsecure, envDevKubeContext,
			envDevKubeNamespace, envDevValues, envBuilder, envPlatform, envNoCache,
		} {
			assert.Contains(t, stdout, "(env: "+env+")")
		}
	})
}

func TestDevPushCommand_dryRun(t *testing.T) {
	t.Run("plans with the options instead of pushing", func(t *testing.T) {
		isolateDevPushEnv(t)
		pusher := newSuccessfulDevPusher()
		dir := t.TempDir()

		_, _, err := runCldkWith(t, &MockReleaser{}, pusher, "--dir", dir, "dogu", "dev-push", "--dry-run", "--reg", "localhost:5001",
			"--skip-image", "postgres")

		require.NoError(t, err)
		assert.Empty(t, pusher.PushCalls())
		require.Len(t, pusher.PlanCalls(), 1)
		assert.Equal(t, devpush.Options{
			Dir:             dir,
			Registry:        "localhost:5001",
			ClusterRegistry: "localhost:5001",
			Build:           project.BuildOptions{Builder: "docker"},
			SkipImages:      []string{"postgres"},
		}, pusher.PlanCalls()[0].Opts)
	})

	t.Run("prints the plan with the dogu resource as YAML", func(t *testing.T) {
		isolateDevPushEnv(t)

		stdout, _, err := runCldkWith(t, &MockReleaser{}, newSuccessfulDevPusher(), "dogu", "dev-push", "--dry-run", "--reg", "localhost:5001")

		require.NoError(t, err)
		assert.Equal(t, `Dry run, nothing has been built, pushed or applied.
Would build image nexus -> localhost:5001/official/dogu/v3/images/nexus:1.2.3-dev.20261103143012
Would replace image sidecar -> registry.example.com/foo:1.0
Would skip image postgres
Would push chart oci://`+devPushChartRef+`
Would apply dogu nexus 1.2.3-dev.20261103143012 (context k3d-dev1, namespace ecosystem):
apiVersion: k8s.cloudogu.com/v3beta1
kind: Dogu
metadata:
  annotations:
    k8s.cloudogu.com/dev-chart-url: oci://k3d-registry-proxy.localhost:5000/official/dogu/v3/charts/nexus
  name: nexus
spec:
  doguApiVersion: v3
  doguNamespace: official
  name: nexus
  version: 1.2.3-dev.20261103143012
`, stdout)
	})

	t.Run("returns the error of the plan", func(t *testing.T) {
		isolateDevPushEnv(t)
		pusher := &MockDevPusher{
			PlanFunc: func(_ context.Context, _ devpush.Options) (devpush.Plan, error) {
				return devpush.Plan{}, errors.New("chart has lint errors")
			},
		}

		stdout, _, err := runCldkWith(t, &MockReleaser{}, pusher, "dogu", "dev-push", "--dry-run", "--reg", "localhost:5001")

		require.ErrorContains(t, err, "chart has lint errors")
		assert.Empty(t, stdout)
	})

	t.Run("validates the flags before planning", func(t *testing.T) {
		isolateDevPushEnv(t)
		pusher := newSuccessfulDevPusher()

		_, _, err := runCldkWith(t, &MockReleaser{}, pusher, "dogu", "dev-push", "--dry-run")

		require.ErrorContains(t, err, "the dev registry is required")
		assert.Empty(t, pusher.PlanCalls())
	})

	t.Run("takes no dry run from the environment", func(t *testing.T) {
		isolateDevPushEnv(t)
		t.Setenv("CLDK_DEV_DRY_RUN", "true")
		pusher := newSuccessfulDevPusher()

		_, _, err := runCldkWith(t, &MockReleaser{}, pusher, "dogu", "dev-push", "--reg", "localhost:5001")

		require.NoError(t, err)
		assert.Empty(t, pusher.PlanCalls())
		assert.Len(t, pusher.PushCalls(), 1)
	})
}
