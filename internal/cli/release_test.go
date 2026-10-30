package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudogu/cldk/internal/release"
)

// newSuccessfulReleaser returns a releaser mock that releases every chart as nexus 3.96.4-3.
func newSuccessfulReleaser() *MockReleaser {
	return &MockReleaser{
		ReleaseFunc: func(_ context.Context, _ string, _ release.Target, _ release.Options) (release.Result, error) {
			return release.Result{
				ChartReference: "registry.cloudogu.com/testing/dogu/v3/charts/nexus:3.96.4-3",
				ChartDigest:    "sha256:1234",
			}, nil
		},
	}
}

// isolateCredentialEnv clears the environment variables for the target registry credentials.
func isolateCredentialEnv(t *testing.T) {
	t.Helper()

	t.Setenv("CLDK_USER", "")
	t.Setenv("CLDK_PASSWORD", "")
}

func TestReleaseCommand(t *testing.T) {
	t.Run("passes the chart and the target from the flags to the releaser", func(t *testing.T) {
		isolateCredentialEnv(t)
		releaser := newSuccessfulReleaser()

		_, _, err := runCldk(t, releaser, "release", "--reg", "staging-registry.cloudogu.com", "--namespace", "testing",
			"--user", "releaser", "--pass", "s3cret", "--plain-http", "--insecure", "nexus-3.96.4-3.tgz")

		require.NoError(t, err)
		require.Len(t, releaser.ReleaseCalls(), 1)
		assert.Equal(t, "nexus-3.96.4-3.tgz", releaser.ReleaseCalls()[0].ChartPath)
		assert.Equal(t, release.Target{
			Registry:              "staging-registry.cloudogu.com",
			Namespace:             "testing",
			User:                  "releaser",
			Password:              "s3cret",
			PlainHTTP:             true,
			InsecureSkipTLSVerify: true,
		}, releaser.ReleaseCalls()[0].Target)
	})

	t.Run("uses the defaults for omitted flags", func(t *testing.T) {
		isolateCredentialEnv(t)
		releaser := newSuccessfulReleaser()

		_, _, err := runCldk(t, releaser, "release", "--namespace", "testing", "nexus-3.96.4-3.tgz")

		require.NoError(t, err)
		require.Len(t, releaser.ReleaseCalls(), 1)
		assert.Equal(t, release.Target{Registry: "registry.cloudogu.com", Namespace: "testing"}, releaser.ReleaseCalls()[0].Target)
	})

	t.Run("passes the images to the releaser", func(t *testing.T) {
		isolateCredentialEnv(t)
		releaser := newSuccessfulReleaser()

		_, _, err := runCldk(t, releaser, "release", "--namespace", "testing",
			"-i", "ci.example.com/nexus:3.96.4-3", "--image", "ci.example.com/postgres:14.18", "nexus-3.96.4-3.tgz")

		require.NoError(t, err)
		require.Len(t, releaser.ReleaseCalls(), 1)
		assert.Equal(t, release.Options{Images: []string{"ci.example.com/nexus:3.96.4-3", "ci.example.com/postgres:14.18"}},
			releaser.ReleaseCalls()[0].Opts)
	})

	t.Run("prints each released image before the chart", func(t *testing.T) {
		isolateCredentialEnv(t)
		releaser := &MockReleaser{
			ReleaseFunc: func(_ context.Context, _ string, _ release.Target, _ release.Options) (release.Result, error) {
				return release.Result{
					Images: []release.ReleasedImage{
						{Source: "ci.example.com/nexus:3.96.4-3", Reference: "registry.cloudogu.com/testing/dogu/v3/images/nexus:3.96.4-3", Digest: "sha256:abcd"},
						{Source: "ci.example.com/postgres:14.18", Reference: "registry.cloudogu.com/testing/dogu/v3/images/postgres:14.18", Digest: "sha256:ef01"},
					},
					ChartReference: "registry.cloudogu.com/testing/dogu/v3/charts/nexus:3.96.4-3",
					ChartDigest:    "sha256:1234",
				}, nil
			},
		}

		stdout, _, err := runCldk(t, releaser, "release", "--namespace", "testing", "nexus-3.96.4-3.tgz")

		require.NoError(t, err)
		assert.Equal(t, "Released image ci.example.com/nexus:3.96.4-3 -> registry.cloudogu.com/testing/dogu/v3/images/nexus:3.96.4-3@sha256:abcd\n"+
			"Released image ci.example.com/postgres:14.18 -> registry.cloudogu.com/testing/dogu/v3/images/postgres:14.18@sha256:ef01\n"+
			"Released chart oci://registry.cloudogu.com/testing/dogu/v3/charts/nexus:3.96.4-3@sha256:1234\n", stdout)
	})

	t.Run("prints the released chart with its digest", func(t *testing.T) {
		isolateCredentialEnv(t)

		stdout, _, err := runCldk(t, newSuccessfulReleaser(), "release", "--namespace", "testing", "nexus-3.96.4-3.tgz")

		require.NoError(t, err)
		assert.Equal(t, "Released chart oci://registry.cloudogu.com/testing/dogu/v3/charts/nexus:3.96.4-3@sha256:1234\n", stdout)
	})

	t.Run("returns the error of the releaser", func(t *testing.T) {
		isolateCredentialEnv(t)
		releaser := &MockReleaser{
			ReleaseFunc: func(_ context.Context, _ string, _ release.Target, _ release.Options) (release.Result, error) {
				return release.Result{}, errors.New("registry unavailable")
			},
		}

		stdout, _, err := runCldk(t, releaser, "release", "--namespace", "testing", "nexus-3.96.4-3.tgz")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "registry unavailable")
		assert.Empty(t, stdout)
	})
}

func TestReleaseCommandValidation(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		errContains string
	}{
		{"missing chart argument", []string{"release", "--namespace", "testing"}, "accepts 1 arg"},
		{"more than one chart argument", []string{"release", "--namespace", "testing", "a.tgz", "b.tgz"}, "accepts 1 arg"},
		{"missing namespace", []string{"release", "nexus-3.96.4-3.tgz"}, `"namespace" not set`},
		{"empty namespace", []string{"release", "--namespace", "", "nexus-3.96.4-3.tgz"}, "--namespace must not be empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateCredentialEnv(t)
			releaser := newSuccessfulReleaser()

			_, _, err := runCldk(t, releaser, tt.args...)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContains)
			assert.Empty(t, releaser.ReleaseCalls())
		})
	}
}

func TestReleaseCommandCredentials(t *testing.T) {
	t.Run("uses CLDK_USER and CLDK_PASSWORD", func(t *testing.T) {
		t.Setenv("CLDK_USER", "releaser")
		t.Setenv("CLDK_PASSWORD", "s3cret")
		releaser := newSuccessfulReleaser()

		_, _, err := runCldk(t, releaser, "release", "--namespace", "testing", "nexus-3.96.4-3.tgz")

		require.NoError(t, err)
		require.Len(t, releaser.ReleaseCalls(), 1)
		assert.Equal(t, "releaser", releaser.ReleaseCalls()[0].Target.User)
		assert.Equal(t, "s3cret", releaser.ReleaseCalls()[0].Target.Password)
	})

	t.Run("flags take precedence over environment", func(t *testing.T) {
		t.Setenv("CLDK_USER", "someone")
		t.Setenv("CLDK_PASSWORD", "wrong")
		releaser := newSuccessfulReleaser()

		_, _, err := runCldk(t, releaser, "release", "--namespace", "testing", "--user", "releaser", "--pass", "s3cret", "nexus-3.96.4-3.tgz")

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
				isolateCredentialEnv(t)

				for key, value := range tt.env {
					t.Setenv(key, value)
				}

				releaser := newSuccessfulReleaser()
				args := append([]string{"release", "--namespace", "testing"}, tt.flags...)

				_, _, err := runCldk(t, releaser, append(args, "nexus-3.96.4-3.tgz")...)

				require.Error(t, err)
				assert.Contains(t, err.Error(), "user and password must be given together")
				assert.Empty(t, releaser.ReleaseCalls())
			})
		}
	})
}
