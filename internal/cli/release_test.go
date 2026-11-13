package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudogu/cldk/internal/oci"

	"github.com/cloudogu/cldk/internal/release"
)

// newSuccessfulReleaser returns a releaser mock that plans and releases every chart as nexus 1.2.3.
func newSuccessfulReleaser() *MockReleaser {
	return &MockReleaser{
		PlanFunc: func(_ context.Context, _ string, _ oci.Target, _ release.ArchiveOptions) (release.Plan, error) {
			return release.Plan{ChartReference: "registry.cloudogu.com/testing/dogu/v3/charts/nexus:1.2.3"}, nil
		},
		ReleaseFunc: func(_ context.Context, _ string, _ oci.Target, _ release.ArchiveOptions) (release.Result, error) {
			return release.Result{
				ChartReference: "registry.cloudogu.com/testing/dogu/v3/charts/nexus:1.2.3",
				ChartDigest:    "sha256:1234",
			}, nil
		},
	}
}

// isolateReleaseEnv removes the environment variables of release for the test.
func isolateReleaseEnv(t *testing.T) {
	t.Helper()

	unsetEnv(t, envRegistry, envNamespace, envPlainHTTP, envInsecure, envUser, envPassword, envBuilder, envPlatform, envNoCache)
}

func TestReleaseCommand(t *testing.T) {
	t.Run("passes the chart and the target from the flags to the releaser", func(t *testing.T) {
		isolateReleaseEnv(t)
		releaser := newSuccessfulReleaser()

		_, _, err := runCldk(t, releaser, "dogu", "release", "--reg", "staging-registry.cloudogu.com", "--namespace", "testing",
			"--user", "releaser", "--pass", "s3cret", "--plain-http", "--insecure", "nexus-1.2.3.tgz")

		require.NoError(t, err)
		require.Len(t, releaser.ReleaseCalls(), 1)
		assert.Equal(t, "nexus-1.2.3.tgz", releaser.ReleaseCalls()[0].ChartPath)
		assert.Equal(t, oci.Target{
			Registry:              "staging-registry.cloudogu.com",
			Namespace:             "testing",
			User:                  "releaser",
			Password:              "s3cret",
			PlainHTTP:             true,
			InsecureSkipTLSVerify: true,
		}, releaser.ReleaseCalls()[0].Target)
	})

	t.Run("uses the defaults for omitted flags", func(t *testing.T) {
		isolateReleaseEnv(t)
		releaser := newSuccessfulReleaser()

		_, _, err := runCldk(t, releaser, "dogu", "release", "--namespace", "testing", "nexus-1.2.3.tgz")

		require.NoError(t, err)
		require.Len(t, releaser.ReleaseCalls(), 1)
		assert.Equal(t, oci.Target{Registry: "registry.cloudogu.com", Namespace: "testing"}, releaser.ReleaseCalls()[0].Target)
	})

	t.Run("passes the images to the releaser", func(t *testing.T) {
		isolateReleaseEnv(t)
		releaser := newSuccessfulReleaser()

		_, _, err := runCldk(t, releaser, "dogu", "release", "--namespace", "testing",
			"-i", "ci.example.com/nexus:3.96.4", "--image", "ci.example.com/postgres:14.18", "nexus-1.2.3.tgz")

		require.NoError(t, err)
		require.Len(t, releaser.ReleaseCalls(), 1)
		assert.Equal(t, release.ArchiveOptions{Images: []string{"ci.example.com/nexus:3.96.4", "ci.example.com/postgres:14.18"}},
			releaser.ReleaseCalls()[0].Opts)
	})

	t.Run("passes the images with explicit targets and the image keys to the releaser", func(t *testing.T) {
		isolateReleaseEnv(t)
		releaser := newSuccessfulReleaser()

		_, _, err := runCldk(t, releaser, "dogu", "release", "--namespace", "testing",
			"-i", "localhost:5001/local-dev/nexus/main:3.96.4=nexus:3.96.4", "-i", "ci.example.com/sa-producer:0.2.0",
			"--image-key", "nexus=nexus", "--image-key", "sa-producer=nexusSaManager", "nexus-1.2.3.tgz")

		require.NoError(t, err)
		require.Len(t, releaser.ReleaseCalls(), 1)
		assert.Equal(t, release.ArchiveOptions{
			Images:    []string{"localhost:5001/local-dev/nexus/main:3.96.4=nexus:3.96.4", "ci.example.com/sa-producer:0.2.0"},
			ImageKeys: []string{"nexus=nexus", "sa-producer=nexusSaManager"},
		}, releaser.ReleaseCalls()[0].Opts)
	})

	t.Run("passes force to the releaser", func(t *testing.T) {
		isolateReleaseEnv(t)
		releaser := newSuccessfulReleaser()

		_, _, err := runCldk(t, releaser, "dogu", "release", "--namespace", "testing", "--force", "nexus-1.2.3.tgz")

		require.NoError(t, err)
		require.Len(t, releaser.ReleaseCalls(), 1)
		assert.Equal(t, release.ArchiveOptions{Force: true}, releaser.ReleaseCalls()[0].Opts)
	})

	t.Run("only plans the release on a dry run", func(t *testing.T) {
		isolateReleaseEnv(t)
		releaser := newSuccessfulReleaser()

		_, _, err := runCldk(t, releaser, "dogu", "release", "--namespace", "testing", "--dry-run", "--force", "nexus-1.2.3.tgz")

		require.NoError(t, err)
		assert.Empty(t, releaser.ReleaseCalls())
		require.Len(t, releaser.PlanCalls(), 1)
		assert.Equal(t, "nexus-1.2.3.tgz", releaser.PlanCalls()[0].ChartPath)
		assert.Equal(t, oci.Target{Registry: "registry.cloudogu.com", Namespace: "testing"}, releaser.PlanCalls()[0].Target)
		assert.Equal(t, release.ArchiveOptions{Force: true}, releaser.PlanCalls()[0].Opts)
	})

	t.Run("marks overwritten images and charts", func(t *testing.T) {
		isolateReleaseEnv(t)
		releaser := &MockReleaser{
			ReleaseFunc: func(_ context.Context, _ string, _ oci.Target, _ release.ArchiveOptions) (release.Result, error) {
				return release.Result{
					Images: []release.ReleasedImage{
						{Source: "ci.example.com/nexus:3.96.4", Reference: "registry.cloudogu.com/testing/dogu/v3/images/nexus:3.96.4", Digest: "sha256:abcd", Status: release.StatusOverwritten},
					},
					ChartReference: "registry.cloudogu.com/testing/dogu/v3/charts/nexus:1.2.3",
					ChartStatus:    release.StatusOverwritten,
					ChartDigest:    "sha256:1234",
				}, nil
			},
		}

		stdout, _, err := runCldk(t, releaser, "dogu", "release", "--namespace", "testing", "--force", "nexus-1.2.3.tgz")

		require.NoError(t, err)
		assert.Equal(t, "Released image ci.example.com/nexus:3.96.4 -> registry.cloudogu.com/testing/dogu/v3/images/nexus:3.96.4@sha256:abcd (overwritten)\n"+
			"Released chart oci://registry.cloudogu.com/testing/dogu/v3/charts/nexus:1.2.3@sha256:1234 (overwritten)\n", stdout)
	})

	t.Run("marks skipped images", func(t *testing.T) {
		isolateReleaseEnv(t)
		releaser := &MockReleaser{
			ReleaseFunc: func(_ context.Context, _ string, _ oci.Target, _ release.ArchiveOptions) (release.Result, error) {
				return release.Result{
					Images: []release.ReleasedImage{
						{Source: "ci.example.com/nexus:3.96.4", Reference: "registry.cloudogu.com/testing/dogu/v3/images/nexus:3.96.4", Digest: "sha256:abcd", Status: release.StatusSkipped},
						{Source: "ci.example.com/postgres:14.18", Reference: "registry.cloudogu.com/testing/dogu/v3/images/postgres:14.18", Digest: "sha256:ef01"},
					},
					ChartReference: "registry.cloudogu.com/testing/dogu/v3/charts/nexus:1.2.3",
					ChartDigest:    "sha256:1234",
				}, nil
			},
		}

		stdout, _, err := runCldk(t, releaser, "dogu", "release", "--namespace", "testing", "nexus-1.2.3.tgz")

		require.NoError(t, err)
		assert.Equal(t, "Skipped image ci.example.com/nexus:3.96.4 -> registry.cloudogu.com/testing/dogu/v3/images/nexus:3.96.4@sha256:abcd (already released)\n"+
			"Released image ci.example.com/postgres:14.18 -> registry.cloudogu.com/testing/dogu/v3/images/postgres:14.18@sha256:ef01\n"+
			"Released chart oci://registry.cloudogu.com/testing/dogu/v3/charts/nexus:1.2.3@sha256:1234\n", stdout)
	})

	t.Run("prints each released image before the chart", func(t *testing.T) {
		isolateReleaseEnv(t)
		releaser := &MockReleaser{
			ReleaseFunc: func(_ context.Context, _ string, _ oci.Target, _ release.ArchiveOptions) (release.Result, error) {
				return release.Result{
					Images: []release.ReleasedImage{
						{Source: "ci.example.com/nexus:3.96.4", Reference: "registry.cloudogu.com/testing/dogu/v3/images/nexus:3.96.4", Digest: "sha256:abcd"},
						{Source: "ci.example.com/postgres:14.18", Reference: "registry.cloudogu.com/testing/dogu/v3/images/postgres:14.18", Digest: "sha256:ef01"},
					},
					ChartReference: "registry.cloudogu.com/testing/dogu/v3/charts/nexus:1.2.3",
					ChartDigest:    "sha256:1234",
				}, nil
			},
		}

		stdout, _, err := runCldk(t, releaser, "dogu", "release", "--namespace", "testing", "nexus-1.2.3.tgz")

		require.NoError(t, err)
		assert.Equal(t, "Released image ci.example.com/nexus:3.96.4 -> registry.cloudogu.com/testing/dogu/v3/images/nexus:3.96.4@sha256:abcd\n"+
			"Released image ci.example.com/postgres:14.18 -> registry.cloudogu.com/testing/dogu/v3/images/postgres:14.18@sha256:ef01\n"+
			"Released chart oci://registry.cloudogu.com/testing/dogu/v3/charts/nexus:1.2.3@sha256:1234\n", stdout)
	})

	t.Run("prints the released chart with its digest", func(t *testing.T) {
		isolateReleaseEnv(t)

		stdout, _, err := runCldk(t, newSuccessfulReleaser(), "dogu", "release", "--namespace", "testing", "nexus-1.2.3.tgz")

		require.NoError(t, err)
		assert.Equal(t, "Released chart oci://registry.cloudogu.com/testing/dogu/v3/charts/nexus:1.2.3@sha256:1234\n", stdout)
	})

	t.Run("returns the error of the releaser", func(t *testing.T) {
		isolateReleaseEnv(t)
		releaser := &MockReleaser{
			ReleaseFunc: func(_ context.Context, _ string, _ oci.Target, _ release.ArchiveOptions) (release.Result, error) {
				return release.Result{}, errors.New("registry unavailable")
			},
		}

		stdout, _, err := runCldk(t, releaser, "dogu", "release", "--namespace", "testing", "nexus-1.2.3.tgz")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "registry unavailable")
		assert.Empty(t, stdout)
	})

	t.Run("prints the images released before a failed push", func(t *testing.T) {
		isolateReleaseEnv(t)
		releaser := &MockReleaser{
			ReleaseFunc: func(_ context.Context, _ string, _ oci.Target, _ release.ArchiveOptions) (release.Result, error) {
				return release.Result{
					Images: []release.ReleasedImage{
						{Source: "ci.example.com/nexus:3.96.4", Reference: "registry.cloudogu.com/testing/dogu/v3/images/nexus:3.96.4", Digest: "sha256:abcd"},
					},
					ChartReference: "registry.cloudogu.com/testing/dogu/v3/charts/nexus:1.2.3",
				}, errors.New("failed to push chart")
			},
		}

		stdout, _, err := runCldk(t, releaser, "dogu", "release", "--namespace", "testing", "nexus-1.2.3.tgz")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to push chart")
		assert.Equal(t, "Released image ci.example.com/nexus:3.96.4 -> registry.cloudogu.com/testing/dogu/v3/images/nexus:3.96.4@sha256:abcd\n", stdout)
	})
}

func TestReleaseCommandValidation(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		errContains string
	}{
		{"more than one chart argument", []string{"dogu", "release", "--namespace", "testing", "a.tgz", "b.tgz"}, "accepts at most 1 arg"},
		{"--tag with a chart archive", []string{"dogu", "release", "--namespace", "testing", "--tag", "nexus=1.0", "nexus-1.2.3.tgz"}, "--tag only applies to a release from the project directory, not to the chart archive nexus-1.2.3.tgz"},
		{"--builder with a chart archive", []string{"dogu", "release", "--namespace", "testing", "--builder", "podman", "nexus-1.2.3.tgz"}, "--builder only applies to a release from the project directory"},
		{"--platform with a chart archive", []string{"dogu", "release", "--namespace", "testing", "--platform", "linux/arm64", "nexus-1.2.3.tgz"}, "--platform only applies"},
		{"--no-cache with a chart archive", []string{"dogu", "release", "--namespace", "testing", "--no-cache", "nexus-1.2.3.tgz"}, "--no-cache only applies"},
		{"--build-arg with a chart archive", []string{"dogu", "release", "--namespace", "testing", "--build-arg", "FOO=bar", "nexus-1.2.3.tgz"}, "--build-arg only applies"},
		{"missing namespace", []string{"dogu", "release", "nexus-1.2.3.tgz"}, "a chart archive needs the namespace, give it with --namespace or CLDK_NAMESPACE"},
		{"empty namespace", []string{"dogu", "release", "--namespace", "", "nexus-1.2.3.tgz"}, "a chart archive needs the namespace"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateReleaseEnv(t)
			releaser := newSuccessfulReleaser()

			_, _, err := runCldk(t, releaser, tt.args...)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContains)
			assert.Empty(t, releaser.ReleaseCalls())
			assert.Empty(t, releaser.ReleaseProjectCalls())
		})
	}
}

func TestReleaseCommandOptions(t *testing.T) {
	t.Run("takes the target from the environment", func(t *testing.T) {
		isolateReleaseEnv(t)
		t.Setenv(envRegistry, "localhost:5001")
		t.Setenv(envNamespace, "testing")
		t.Setenv(envPlainHTTP, "true")
		t.Setenv(envInsecure, "1")
		releaser := newSuccessfulReleaser()

		_, _, err := runCldk(t, releaser, "dogu", "release", "nexus-1.2.3.tgz")

		require.NoError(t, err)
		require.Len(t, releaser.ReleaseCalls(), 1)
		assert.Equal(t, oci.Target{Registry: "localhost:5001", Namespace: "testing", PlainHTTP: true, InsecureSkipTLSVerify: true},
			releaser.ReleaseCalls()[0].Target)
	})

	t.Run("takes the target from the .env of the project directory", func(t *testing.T) {
		isolateReleaseEnv(t)
		dir := writeDotEnv(t, "CLDK_REG=localhost:5001\nCLDK_NAMESPACE=testing\nCLDK_PLAIN_HTTP=true\n")
		releaser := newSuccessfulReleaser()

		_, _, err := runCldk(t, releaser, "--dir", dir, "dogu", "release", "nexus-1.2.3.tgz")

		require.NoError(t, err)
		require.Len(t, releaser.ReleaseCalls(), 1)
		assert.Equal(t, oci.Target{Registry: "localhost:5001", Namespace: "testing", PlainHTTP: true}, releaser.ReleaseCalls()[0].Target)
	})

	t.Run("flags take precedence over the environment", func(t *testing.T) {
		isolateReleaseEnv(t)
		t.Setenv(envRegistry, "localhost:5001")
		t.Setenv(envNamespace, "testing")
		t.Setenv(envPlainHTTP, "true")
		releaser := newSuccessfulReleaser()

		_, _, err := runCldk(t, releaser, "dogu", "release", "--reg", "registry.example.com", "--namespace", "official",
			"--plain-http=false", "nexus-1.2.3.tgz")

		require.NoError(t, err)
		require.Len(t, releaser.ReleaseCalls(), 1)
		assert.Equal(t, oci.Target{Registry: "registry.example.com", Namespace: "official"}, releaser.ReleaseCalls()[0].Target)
	})

	t.Run("logs the resolved options with their source on -v, without the password", func(t *testing.T) {
		isolateReleaseEnv(t)
		t.Setenv(envNamespace, "testing")
		t.Setenv(envUser, "releaser")
		t.Setenv(envPassword, "s3cret")

		_, stderr, err := runCldk(t, newSuccessfulReleaser(), "-v", "dogu", "release", "--plain-http", "nexus-1.2.3.tgz")

		require.NoError(t, err)
		assert.Contains(t, stderr, `option=reg value="" source="default: registry.cloudogu.com"`)
		assert.Contains(t, stderr, `option=namespace value=testing source="env CLDK_NAMESPACE"`)
		assert.Contains(t, stderr, `option=plain-http value=true source="flag --plain-http"`)
		assert.Contains(t, stderr, `option=user value=releaser source="env CLDK_USER"`)
		assert.Contains(t, stderr, `option=pass value=*** source="env CLDK_PASSWORD"`)
		assert.NotContains(t, stderr, "s3cret")
	})

	t.Run("fails on an environment variable that is no boolean", func(t *testing.T) {
		isolateReleaseEnv(t)
		t.Setenv(envPlainHTTP, "yes please")
		releaser := newSuccessfulReleaser()

		_, _, err := runCldk(t, releaser, "dogu", "release", "--namespace", "testing", "nexus-1.2.3.tgz")

		require.ErrorContains(t, err, `invalid value "yes please" of CLDK_PLAIN_HTTP`)
		assert.Empty(t, releaser.ReleaseCalls())
	})

	t.Run("names the environment variable of each flag in the help", func(t *testing.T) {
		stdout, _, err := runCldk(t, &MockReleaser{}, "dogu", "release", "--help")

		require.NoError(t, err)

		for _, env := range []string{envRegistry, envNamespace, envPlainHTTP, envInsecure, envUser, envPassword} {
			assert.Contains(t, stdout, "(env: "+env+")")
		}
	})
}

func TestReleaseCommandCredentials(t *testing.T) {
	t.Run("uses CLDK_USER and CLDK_PASSWORD", func(t *testing.T) {
		isolateReleaseEnv(t)
		t.Setenv("CLDK_USER", "releaser")
		t.Setenv("CLDK_PASSWORD", "s3cret")
		releaser := newSuccessfulReleaser()

		_, _, err := runCldk(t, releaser, "dogu", "release", "--namespace", "testing", "nexus-1.2.3.tgz")

		require.NoError(t, err)
		require.Len(t, releaser.ReleaseCalls(), 1)
		assert.Equal(t, "releaser", releaser.ReleaseCalls()[0].Target.User)
		assert.Equal(t, "s3cret", releaser.ReleaseCalls()[0].Target.Password)
	})

	t.Run("flags take precedence over environment", func(t *testing.T) {
		isolateReleaseEnv(t)
		t.Setenv("CLDK_USER", "someone")
		t.Setenv("CLDK_PASSWORD", "wrong")
		releaser := newSuccessfulReleaser()

		_, _, err := runCldk(t, releaser, "dogu", "release", "--namespace", "testing", "--user", "releaser", "--pass", "s3cret", "nexus-1.2.3.tgz")

		require.NoError(t, err)
		require.Len(t, releaser.ReleaseCalls(), 1)
		assert.Equal(t, "releaser", releaser.ReleaseCalls()[0].Target.User)
		assert.Equal(t, "s3cret", releaser.ReleaseCalls()[0].Target.Password)
	})

	t.Run("fails if user and password are not given together", func(t *testing.T) {
		tests := []struct {
			name  string
			flags []string
			env   map[string]string
		}{
			{"only --user", []string{"--user", "releaser"}, nil},
			{"only --pass", []string{"--pass", "s3cret"}, nil},
			{"only CLDK_USER", nil, map[string]string{"CLDK_USER": "releaser"}},
			{"only CLDK_PASSWORD", nil, map[string]string{"CLDK_PASSWORD": "s3cret"}},
			{"--user with CLDK_PASSWORD", []string{"--user", "releaser"}, map[string]string{"CLDK_PASSWORD": "s3cret"}},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				isolateReleaseEnv(t)

				for key, value := range tt.env {
					t.Setenv(key, value)
				}

				releaser := newSuccessfulReleaser()
				args := append([]string{"dogu", "release", "--namespace", "testing"}, tt.flags...)

				_, _, err := runCldk(t, releaser, append(args, "nexus-1.2.3.tgz")...)

				require.Error(t, err)
				assert.Contains(t, err.Error(), "user and password must be given together")
				assert.Empty(t, releaser.ReleaseCalls())
			})
		}
	})
}

// dryRunPlan is the plan of a dry run with an image in each status.
var dryRunPlan = release.Plan{
	Images: []release.PlannedImage{
		{Source: "ci.example.com/nexus:3.96.4", Reference: "registry.cloudogu.com/testing/dogu/v3/images/nexus:3.96.4", Digest: "sha256:abcd"},
		{Source: "ci.example.com/postgres:14.18", Reference: "registry.cloudogu.com/testing/dogu/v3/images/postgres:14.18", Digest: "sha256:ef01", ExistingDigest: "sha256:ef01", Status: release.StatusSkipped},
		{Source: "ci.example.com/sa-producer:0.2.0", Reference: "registry.cloudogu.com/testing/dogu/v3/images/sa-producer:0.2.0", Digest: "sha256:2345", ExistingDigest: "sha256:aaaa", Status: release.StatusOverwritten},
		{Source: "ci.example.com/usermgt:1.20.0-1", Reference: "registry.cloudogu.com/testing/dogu/v3/images/usermgt:1.20.0-1", Digest: "sha256:6789", ExistingDigest: "sha256:bbbb", Status: release.StatusConflict},
	},
	ChartReference: "registry.cloudogu.com/testing/dogu/v3/charts/nexus:1.2.3",
}

// dryRunOutput is the output of dryRunPlan without the chart.
const dryRunOutput = "Dry run, nothing has been pushed.\n" +
	"Would push image ci.example.com/nexus:3.96.4 -> registry.cloudogu.com/testing/dogu/v3/images/nexus:3.96.4@sha256:abcd\n" +
	"Would skip image ci.example.com/postgres:14.18 -> registry.cloudogu.com/testing/dogu/v3/images/postgres:14.18@sha256:ef01 (already released)\n" +
	"Would overwrite image ci.example.com/sa-producer:0.2.0 -> registry.cloudogu.com/testing/dogu/v3/images/sa-producer:0.2.0@sha256:2345\n" +
	"Conflict for image ci.example.com/usermgt:1.20.0-1 -> registry.cloudogu.com/testing/dogu/v3/images/usermgt:1.20.0-1@sha256:6789 (exists with another digest)\n"

// newPlanningReleaser returns a releaser mock that plans the given plan.
func newPlanningReleaser(plan release.Plan, err error) *MockReleaser {
	return &MockReleaser{
		PlanFunc: func(_ context.Context, _ string, _ oci.Target, _ release.ArchiveOptions) (release.Plan, error) {
			return plan, err
		},
	}
}

func TestReleaseCommandDryRun(t *testing.T) {
	tests := []struct {
		name        string
		chartStatus release.Status
		chartOutput string
	}{
		{"new chart", release.StatusPushed, "Would release chart oci://registry.cloudogu.com/testing/dogu/v3/charts/nexus:1.2.3\n"},
		{"overwritten chart", release.StatusOverwritten, "Would overwrite chart oci://registry.cloudogu.com/testing/dogu/v3/charts/nexus:1.2.3\n"},
		{"conflicting chart", release.StatusConflict, "Conflict for chart oci://registry.cloudogu.com/testing/dogu/v3/charts/nexus:1.2.3 (already released)\n"},
	}

	for _, tt := range tests {
		t.Run("prints the plan with a "+tt.name, func(t *testing.T) {
			isolateReleaseEnv(t)
			plan := dryRunPlan
			plan.ChartStatus = tt.chartStatus

			stdout, _, _ := runCldk(t, newPlanningReleaser(plan, nil), "dogu", "release", "--namespace", "testing", "--dry-run", "nexus-1.2.3.tgz")

			assert.Equal(t, dryRunOutput+tt.chartOutput, stdout)
		})
	}

	t.Run("prints the plan and fails with all conflicts", func(t *testing.T) {
		isolateReleaseEnv(t)
		plan := dryRunPlan
		plan.ChartStatus = release.StatusConflict

		stdout, _, err := runCldk(t, newPlanningReleaser(plan, nil), "dogu", "release", "--namespace", "testing", "--dry-run", "nexus-1.2.3.tgz")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "image registry.cloudogu.com/testing/dogu/v3/images/usermgt:1.20.0-1 already exists with digest sha256:bbbb")
		assert.Contains(t, err.Error(), "chart registry.cloudogu.com/testing/dogu/v3/charts/nexus:1.2.3 is already released")
		assert.Equal(t, dryRunOutput+"Conflict for chart oci://registry.cloudogu.com/testing/dogu/v3/charts/nexus:1.2.3 (already released)\n", stdout)
	})

	t.Run("succeeds without conflicts", func(t *testing.T) {
		isolateReleaseEnv(t)
		plan := release.Plan{
			Images:         dryRunPlan.Images[:3],
			ChartReference: dryRunPlan.ChartReference,
		}

		_, _, err := runCldk(t, newPlanningReleaser(plan, nil), "dogu", "release", "--namespace", "testing", "--dry-run", "nexus-1.2.3.tgz")

		require.NoError(t, err)
	})

	t.Run("returns the error of the releaser without a plan", func(t *testing.T) {
		isolateReleaseEnv(t)

		stdout, _, err := runCldk(t, newPlanningReleaser(release.Plan{}, errors.New("registry unavailable")),
			"dogu", "release", "--namespace", "testing", "--dry-run", "nexus-1.2.3.tgz")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "registry unavailable")
		assert.Empty(t, stdout)
	})
}
