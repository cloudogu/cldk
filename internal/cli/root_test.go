package cli

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runCldk executes the root command in-process with the given arguments and returns stdout and stderr.
func runCldk(t *testing.T, args ...string) (stdout string, stderr string, err error) {
	t.Helper()

	var out, errOut bytes.Buffer
	cmd := NewRootCommand(BuildInfo{Version: "1.2.3", CommitID: "abc123"})
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)

	err = cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestVersion(t *testing.T) {
	stdout, _, err := runCldk(t, "version")

	require.NoError(t, err)
	assert.Equal(t, "cldk 1.2.3 (commit abc123)\n", stdout)
}

func TestVerboseFlag(t *testing.T) {
	t.Run("without flag no debug output is logged", func(t *testing.T) {
		_, stderr, err := runCldk(t, "version")

		require.NoError(t, err)
		assert.NotContains(t, stderr, "level=DEBUG")
	})

	for _, flag := range []string{"-v", "--verbose"} {
		t.Run(flag+" enables debug output", func(t *testing.T) {
			_, stderr, err := runCldk(t, flag, "version")

			require.NoError(t, err)
			assert.Contains(t, stderr, "level=DEBUG")
			assert.Contains(t, stderr, "version=1.2.3")
		})
	}
}
