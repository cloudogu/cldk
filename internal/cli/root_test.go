package cli

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runCldk executes the root command in-process with the given releaser and arguments and returns stdout and stderr.
func runCldk(t *testing.T, releaser Releaser, args ...string) (stdout string, stderr string, err error) {
	t.Helper()

	return runCldkWith(t, releaser, &MockDevPusher{}, args...)
}

// runCldkWith executes the root command in-process with the given releaser, dev pusher and arguments and returns
// stdout and stderr.
func runCldkWith(t *testing.T, releaser Releaser, devPusher DevPusher, args ...string) (stdout string, stderr string, err error) {
	t.Helper()

	var out, errOut bytes.Buffer
	cmd := NewRootCommand(BuildInfo{Version: "1.2.3", CommitID: "abc123"}, releaser, devPusher)
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)

	err = cmd.Execute()

	return out.String(), errOut.String(), err
}

func TestVersion(t *testing.T) {
	stdout, _, err := runCldk(t, &MockReleaser{}, "version")

	require.NoError(t, err)
	assert.Equal(t, "cldk 1.2.3 (commit abc123)\n", stdout)
}

func TestVerboseFlag(t *testing.T) {
	t.Run("without flag no debug output is logged", func(t *testing.T) {
		_, stderr, err := runCldk(t, &MockReleaser{}, "version")

		require.NoError(t, err)
		assert.NotContains(t, stderr, "level=DEBUG")
	})

	for _, flag := range []string{"-v", "--verbose"} {
		t.Run(flag+" enables debug output", func(t *testing.T) {
			_, stderr, err := runCldk(t, &MockReleaser{}, flag, "version")

			require.NoError(t, err)
			assert.Contains(t, stderr, "level=DEBUG")
			assert.Contains(t, stderr, "version=1.2.3")
		})
	}
}

func TestDoguCommands(t *testing.T) {
	for _, command := range []string{"release", "dev-push"} {
		t.Run(command+" is a subcommand of dogu only", func(t *testing.T) {
			_, _, err := runCldk(t, &MockReleaser{}, command, "--help")

			require.Error(t, err)
			assert.Contains(t, err.Error(), `unknown command "`+command+`"`)

			stdout, _, err := runCldk(t, &MockReleaser{}, "dogu", command, "--help")

			require.NoError(t, err)
			assert.Contains(t, stdout, "cldk dogu "+command)
		})
	}
}
