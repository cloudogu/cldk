package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeDotEnv writes a .env with the given content into a new temporary project directory and returns the directory.
func writeDotEnv(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0o600))

	return dir
}

// unsetEnv removes the given environment variables for the test and restores them afterwards.
func unsetEnv(t *testing.T, keys ...string) {
	t.Helper()

	for _, key := range keys {
		t.Setenv(key, "")
		require.NoError(t, os.Unsetenv(key))
	}
}

func TestDotEnv(t *testing.T) {
	t.Run("passes the credentials from the .env to the releaser", func(t *testing.T) {
		unsetEnv(t, "CLDK_USER", "CLDK_PASSWORD")
		dir := writeDotEnv(t, "CLDK_USER=releaser\nCLDK_PASSWORD=s3cret\n")
		releaser := newSuccessfulReleaser()

		_, _, err := runCldk(t, releaser, "--dir", dir, "release", "--namespace", "testing", "nexus-3.96.4-3.tgz")

		require.NoError(t, err)
		require.Len(t, releaser.ReleaseCalls(), 1)
		assert.Equal(t, "releaser", releaser.ReleaseCalls()[0].Target.User)
		assert.Equal(t, "s3cret", releaser.ReleaseCalls()[0].Target.Password)
	})

	t.Run("process environment takes precedence over the .env", func(t *testing.T) {
		t.Setenv("CLDK_USER", "releaser")
		t.Setenv("CLDK_PASSWORD", "s3cret")
		dir := writeDotEnv(t, "CLDK_USER=someone\nCLDK_PASSWORD=wrong\n")
		releaser := newSuccessfulReleaser()

		_, _, err := runCldk(t, releaser, "-d", dir, "release", "--namespace", "testing", "nexus-3.96.4-3.tgz")

		require.NoError(t, err)
		require.Len(t, releaser.ReleaseCalls(), 1)
		assert.Equal(t, "releaser", releaser.ReleaseCalls()[0].Target.User)
		assert.Equal(t, "s3cret", releaser.ReleaseCalls()[0].Target.Password)
	})

	t.Run("an empty process variable takes precedence over the .env", func(t *testing.T) {
		isolateCredentialEnv(t)
		dir := writeDotEnv(t, "CLDK_USER=someone\nCLDK_PASSWORD=wrong\n")
		releaser := newSuccessfulReleaser()

		_, _, err := runCldk(t, releaser, "-d", dir, "release", "--namespace", "testing", "nexus-3.96.4-3.tgz")

		require.NoError(t, err)
		require.Len(t, releaser.ReleaseCalls(), 1)
		assert.Empty(t, releaser.ReleaseCalls()[0].Target.User)
		assert.Empty(t, releaser.ReleaseCalls()[0].Target.Password)
	})

	t.Run("loads the .env of the working directory by default", func(t *testing.T) {
		unsetEnv(t, "CLDK_USER", "CLDK_PASSWORD")
		t.Chdir(writeDotEnv(t, "CLDK_USER=releaser\nCLDK_PASSWORD=s3cret\n"))
		releaser := newSuccessfulReleaser()

		_, _, err := runCldk(t, releaser, "release", "--namespace", "testing", "nexus-3.96.4-3.tgz")

		require.NoError(t, err)
		require.Len(t, releaser.ReleaseCalls(), 1)
		assert.Equal(t, "releaser", releaser.ReleaseCalls()[0].Target.User)
	})

	t.Run("a missing .env is not an error", func(t *testing.T) {
		isolateCredentialEnv(t)
		releaser := newSuccessfulReleaser()

		_, _, err := runCldk(t, releaser, "--dir", t.TempDir(), "release", "--namespace", "testing", "nexus-3.96.4-3.tgz")

		require.NoError(t, err)
		require.Len(t, releaser.ReleaseCalls(), 1)
	})

	t.Run("a broken .env fails with its path", func(t *testing.T) {
		isolateCredentialEnv(t)
		dir := writeDotEnv(t, "CLDK_USER releaser\n")
		releaser := newSuccessfulReleaser()

		_, _, err := runCldk(t, releaser, "--dir", dir, "release", "--namespace", "testing", "nexus-3.96.4-3.tgz")

		require.Error(t, err)
		assert.Contains(t, err.Error(), filepath.Join(dir, ".env"))
		assert.Empty(t, releaser.ReleaseCalls())
	})

	t.Run("logs the keys from the .env without their values", func(t *testing.T) {
		unsetEnv(t, "CLDK_USER", "CLDK_PASSWORD")
		dir := writeDotEnv(t, "CLDK_USER=releaser\nCLDK_PASSWORD=s3cret\n")

		_, stderr, err := runCldk(t, &MockReleaser{}, "-v", "--dir", dir, "version")

		require.NoError(t, err)
		assert.Contains(t, stderr, "keys=\"[CLDK_PASSWORD CLDK_USER]\"")
		assert.NotContains(t, stderr, "s3cret")
	})
}
