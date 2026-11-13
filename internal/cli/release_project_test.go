package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudogu/cldk/internal/oci"
	"github.com/cloudogu/cldk/internal/project"
	"github.com/cloudogu/cldk/internal/release"
)

const (
	projectChartRef = "registry.cloudogu.com/official/dogu/v3/charts/nexus:1.2.3"
	projectNexusRef = "registry.cloudogu.com/official/dogu/v3/images/nexus:3.96.4-1"
)

// newProjectReleaser returns a releaser mock that plans and releases every project as nexus 1.2.3 with the built image
// nexus.
func newProjectReleaser() *MockReleaser {
	return &MockReleaser{
		PlanProjectFunc: func(_ context.Context, _ oci.Target, _ release.ProjectOptions) (release.Plan, error) {
			return release.Plan{
				BuiltImages:    []release.BuiltImage{{Name: "nexus", Reference: projectNexusRef}},
				ChartReference: projectChartRef,
			}, nil
		},
		ReleaseProjectFunc: func(_ context.Context, _ oci.Target, _ release.ProjectOptions) (release.Result, error) {
			return release.Result{
				BuiltImages:    []release.BuiltImage{{Name: "nexus", Reference: projectNexusRef, Digest: "sha256:abcd"}},
				ChartReference: projectChartRef,
				ChartDigest:    "sha256:1234",
			}, nil
		},
	}
}

func TestReleaseCommand_project(t *testing.T) {
	t.Run("releases the project directory without argument", func(t *testing.T) {
		isolateReleaseEnv(t)
		releaser := newProjectReleaser()
		dir := t.TempDir()

		stdout, _, err := runCldk(t, releaser, "--dir", dir, "dogu", "release")

		require.NoError(t, err)
		assert.Empty(t, releaser.ReleaseCalls())
		require.Len(t, releaser.ReleaseProjectCalls(), 1)
		assert.Equal(t, oci.Target{Registry: "registry.cloudogu.com"}, releaser.ReleaseProjectCalls()[0].Target,
			"without --namespace, the releaser takes the namespace of cldk.yaml")
		assert.Equal(t, release.ProjectOptions{Dir: dir, Build: project.BuildOptions{Builder: "docker"}}, releaser.ReleaseProjectCalls()[0].Opts)
		assert.Equal(t, "Released image nexus -> "+projectNexusRef+"@sha256:abcd\n"+
			"Released chart oci://"+projectChartRef+"@sha256:1234\n", stdout)
	})

	t.Run("passes the flags to the releaser", func(t *testing.T) {
		isolateReleaseEnv(t)
		releaser := newProjectReleaser()
		dir := t.TempDir()

		_, _, err := runCldk(t, releaser, "--dir", dir, "dogu", "release", "--reg", "localhost:5001", "--namespace", "testing",
			"--plain-http", "--tag", "nexus=3.96.4-2", "--tag", "sidecar=1.2.0", "-i", "ci.example.com/postgres:14.18",
			"--image-key", "postgres=postgresql", "--force", "--builder", "podman", "--platform", "linux/arm64", "--no-cache",
			"--build-arg", "HTTP_PROXY")

		require.NoError(t, err)
		require.Len(t, releaser.ReleaseProjectCalls(), 1)
		assert.Equal(t, oci.Target{Registry: "localhost:5001", Namespace: "testing", PlainHTTP: true}, releaser.ReleaseProjectCalls()[0].Target)
		assert.Equal(t, release.ProjectOptions{
			Images:    []string{"ci.example.com/postgres:14.18"},
			ImageKeys: []string{"postgres=postgresql"},
			Force:     true,
			Dir:       dir,
			Tags:      []string{"nexus=3.96.4-2", "sidecar=1.2.0"},
			Build:     project.BuildOptions{Builder: "podman", Platform: "linux/arm64", NoCache: true, BuildArgs: []string{"HTTP_PROXY"}},
		}, releaser.ReleaseProjectCalls()[0].Opts)
	})

	t.Run("takes the build settings from the .env of the project directory", func(t *testing.T) {
		isolateReleaseEnv(t)
		releaser := newProjectReleaser()
		dir := writeDotEnv(t, "CLDK_BUILDER=podman\nCLDK_PLATFORM=linux/arm64\nCLDK_NO_CACHE=true\nCLDK_NAMESPACE=testing\n")

		_, _, err := runCldk(t, releaser, "--dir", dir, "dogu", "release")

		require.NoError(t, err)
		require.Len(t, releaser.ReleaseProjectCalls(), 1)
		assert.Equal(t, "testing", releaser.ReleaseProjectCalls()[0].Target.Namespace)
		assert.Equal(t, project.BuildOptions{Builder: "podman", Platform: "linux/arm64", NoCache: true},
			releaser.ReleaseProjectCalls()[0].Opts.Build)
	})

	t.Run("ignores the build settings of the environment for a chart archive", func(t *testing.T) {
		isolateReleaseEnv(t)
		t.Setenv(envBuilder, "podman")
		t.Setenv(envNoCache, "not a boolean")
		releaser := newSuccessfulReleaser()

		_, _, err := runCldk(t, releaser, "dogu", "release", "--namespace", "testing", "nexus-1.2.3.tgz")

		require.NoError(t, err)
		assert.Len(t, releaser.ReleaseCalls(), 1)
	})

	t.Run("logs the namespace default of cldk.yaml on -v", func(t *testing.T) {
		isolateReleaseEnv(t)

		_, stderr, err := runCldk(t, newProjectReleaser(), "-v", "--dir", t.TempDir(), "dogu", "release")

		require.NoError(t, err)
		assert.Contains(t, stderr, `option=namespace value="" source="default: namespace of cldk.yaml"`)
	})

	t.Run("marks overwritten built images", func(t *testing.T) {
		isolateReleaseEnv(t)
		releaser := &MockReleaser{
			ReleaseProjectFunc: func(_ context.Context, _ oci.Target, _ release.ProjectOptions) (release.Result, error) {
				return release.Result{
					BuiltImages: []release.BuiltImage{
						{Name: "nexus", Reference: projectNexusRef, Digest: "sha256:abcd", Status: release.StatusOverwritten},
					},
					Images: []release.ReleasedImage{
						{Source: "ci.example.com/postgres:14.18", Reference: "registry.cloudogu.com/official/dogu/v3/images/postgres:14.18", Digest: "sha256:ef01"},
					},
					ChartReference: projectChartRef,
					ChartStatus:    release.StatusOverwritten,
					ChartDigest:    "sha256:1234",
				}, nil
			},
		}

		stdout, _, err := runCldk(t, releaser, "dogu", "release", "--force")

		require.NoError(t, err)
		assert.Equal(t, "Released image nexus -> "+projectNexusRef+"@sha256:abcd (overwritten)\n"+
			"Released image ci.example.com/postgres:14.18 -> registry.cloudogu.com/official/dogu/v3/images/postgres:14.18@sha256:ef01\n"+
			"Released chart oci://"+projectChartRef+"@sha256:1234 (overwritten)\n", stdout)
	})

	t.Run("prints the images released before a failed push", func(t *testing.T) {
		isolateReleaseEnv(t)
		releaser := &MockReleaser{
			ReleaseProjectFunc: func(_ context.Context, _ oci.Target, _ release.ProjectOptions) (release.Result, error) {
				return release.Result{
					BuiltImages:    []release.BuiltImage{{Name: "nexus", Reference: projectNexusRef, Digest: "sha256:abcd"}},
					ChartReference: projectChartRef,
				}, errors.New("failed to push image sidecar")
			},
		}

		stdout, _, err := runCldk(t, releaser, "dogu", "release")

		require.ErrorContains(t, err, "failed to push image sidecar")
		assert.Equal(t, "Released image nexus -> "+projectNexusRef+"@sha256:abcd\n", stdout)
	})

	t.Run("fails on an invalid build argument", func(t *testing.T) {
		isolateReleaseEnv(t)
		releaser := newProjectReleaser()

		_, _, err := runCldk(t, releaser, "dogu", "release", "--build-arg", "=bar")

		require.ErrorContains(t, err, `invalid build argument "=bar" of --build-arg`)
		assert.Empty(t, releaser.ReleaseProjectCalls())
	})

	t.Run("names the environment variables of the build flags in the help", func(t *testing.T) {
		stdout, _, err := runCldk(t, &MockReleaser{}, "dogu", "release", "--help")

		require.NoError(t, err)

		for _, env := range []string{envBuilder, envPlatform, envNoCache} {
			assert.Contains(t, stdout, "(env: "+env+")")
		}
	})
}

func TestReleaseCommand_projectDryRun(t *testing.T) {
	t.Run("plans the project release instead of releasing it", func(t *testing.T) {
		isolateReleaseEnv(t)
		releaser := newProjectReleaser()
		dir := t.TempDir()

		stdout, _, err := runCldk(t, releaser, "--dir", dir, "dogu", "release", "--dry-run", "--tag", "nexus=3.96.4-1")

		require.NoError(t, err)
		assert.Empty(t, releaser.ReleaseProjectCalls())
		require.Len(t, releaser.PlanProjectCalls(), 1)
		assert.Equal(t, []string{"nexus=3.96.4-1"}, releaser.PlanProjectCalls()[0].Opts.Tags)
		assert.Equal(t, "Dry run, nothing has been pushed.\n"+
			"Would build image nexus -> "+projectNexusRef+"\n"+
			"Would release chart oci://"+projectChartRef+"\n", stdout)
	})

	t.Run("prints the plan with all conflicts and fails", func(t *testing.T) {
		isolateReleaseEnv(t)
		plan := release.Plan{
			BuiltImages: []release.BuiltImage{
				{Name: "nexus", Reference: projectNexusRef, Status: release.StatusConflict},
				{Name: "sidecar", Reference: "registry.cloudogu.com/official/dogu/v3/images/sidecar:1.2.0", Status: release.StatusOverwritten},
			},
			Images: []release.PlannedImage{
				{Source: "ci.example.com/postgres:14.18", Reference: "registry.cloudogu.com/official/dogu/v3/images/postgres:14.18", Digest: "sha256:ef01"},
			},
			ChartReference: projectChartRef,
			ChartStatus:    release.StatusConflict,
		}
		releaser := &MockReleaser{
			PlanProjectFunc: func(_ context.Context, _ oci.Target, _ release.ProjectOptions) (release.Plan, error) {
				return plan, nil
			},
		}

		stdout, _, err := runCldk(t, releaser, "dogu", "release", "--dry-run")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "image "+projectNexusRef+" is already released, use --force to overwrite it")
		assert.Contains(t, err.Error(), "chart "+projectChartRef+" is already released")
		assert.Equal(t, "Dry run, nothing has been pushed.\n"+
			"Conflict for image nexus -> "+projectNexusRef+" (already released)\n"+
			"Would overwrite image sidecar -> registry.cloudogu.com/official/dogu/v3/images/sidecar:1.2.0\n"+
			"Would push image ci.example.com/postgres:14.18 -> registry.cloudogu.com/official/dogu/v3/images/postgres:14.18@sha256:ef01\n"+
			"Conflict for chart oci://"+projectChartRef+" (already released)\n", stdout)
	})

	t.Run("returns the error of the plan", func(t *testing.T) {
		isolateReleaseEnv(t)
		releaser := &MockReleaser{
			PlanProjectFunc: func(_ context.Context, _ oci.Target, _ release.ProjectOptions) (release.Plan, error) {
				return release.Plan{}, errors.New("no release tag for the images nexus")
			},
		}

		stdout, _, err := runCldk(t, releaser, "dogu", "release", "--dry-run")

		require.ErrorContains(t, err, "no release tag for the images nexus")
		assert.Empty(t, stdout)
	})
}
