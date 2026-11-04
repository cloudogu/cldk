package cli

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testEnvName  = "CLDK_TEST_NAME"
	testEnvForce = "CLDK_TEST_FORCE"
)

// newTestResolver returns a resolver for a command with the string flag --name and the boolean flag --force, parsed
// from args, and the buffer its debug log is written to.
func newTestResolver(t *testing.T, args ...string) (optionResolver, *bytes.Buffer) {
	t.Helper()

	unsetEnv(t, testEnvName, testEnvForce)

	cmd := &cobra.Command{}
	cmd.Flags().String("name", "", "")
	cmd.Flags().Bool("force", false, "")
	require.NoError(t, cmd.Flags().Parse(args))

	var log bytes.Buffer

	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	return optionResolver{cmd: cmd}, &log
}

func TestOptionResolverStr(t *testing.T) {
	t.Run("takes the flag over the environment", func(t *testing.T) {
		r, log := newTestResolver(t, "--name", "from-flag")
		t.Setenv(testEnvName, "from-env")

		assert.Equal(t, "from-flag", r.str("name", testEnvName, ""))
		assert.Contains(t, log.String(), `option=name value=from-flag source="flag --name"`)
	})

	t.Run("takes the environment without flag", func(t *testing.T) {
		r, log := newTestResolver(t)
		t.Setenv(testEnvName, "from-env")

		assert.Equal(t, "from-env", r.str("name", testEnvName, ""))
		assert.Contains(t, log.String(), `option=name value=from-env source="env CLDK_TEST_NAME"`)
	})

	t.Run("an empty environment variable counts as not set", func(t *testing.T) {
		r, log := newTestResolver(t)
		t.Setenv(testEnvName, "")

		assert.Empty(t, r.str("name", testEnvName, ""))
		assert.Contains(t, log.String(), `option=name value="" source=default`)
	})

	t.Run("names the source of the default", func(t *testing.T) {
		r, log := newTestResolver(t)

		assert.Empty(t, r.str("name", testEnvName, "name of the project"))
		assert.Contains(t, log.String(), `option=name value="" source="default: name of the project"`)
	})
}

func TestOptionResolverBoolean(t *testing.T) {
	t.Run("takes the flag over the environment", func(t *testing.T) {
		r, log := newTestResolver(t, "--force=false")
		t.Setenv(testEnvForce, "true")

		value, err := r.boolean("force", testEnvForce)

		require.NoError(t, err)
		assert.False(t, value)
		assert.Contains(t, log.String(), `option=force value=false source="flag --force"`)
	})

	t.Run("takes the environment without flag", func(t *testing.T) {
		r, log := newTestResolver(t)
		t.Setenv(testEnvForce, "1")

		value, err := r.boolean("force", testEnvForce)

		require.NoError(t, err)
		assert.True(t, value)
		assert.Contains(t, log.String(), `option=force value=true source="env CLDK_TEST_FORCE"`)
	})

	t.Run("an empty environment variable counts as not set", func(t *testing.T) {
		r, log := newTestResolver(t)
		t.Setenv(testEnvForce, "")

		value, err := r.boolean("force", testEnvForce)

		require.NoError(t, err)
		assert.False(t, value)
		assert.Contains(t, log.String(), `option=force value=false source=default`)
	})

	t.Run("names the variable of a value that is no boolean", func(t *testing.T) {
		r, _ := newTestResolver(t)
		t.Setenv(testEnvForce, "yes")

		_, err := r.boolean("force", testEnvForce)

		require.ErrorContains(t, err, `invalid value "yes" of CLDK_TEST_FORCE`)
	})
}
