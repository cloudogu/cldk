package cli

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"github.com/spf13/cobra"
)

// optionResolver resolves an option from its flag and environment variable and logs its value and source.
// Variables from the .env of the project directory are part of the environment, see loadDotEnv.
type optionResolver struct {
	// cmd is the command whose flags are resolved.
	cmd *cobra.Command
}

// lookup returns the value of the flag if it is given, otherwise the value of the environment variable, and their
// source. An empty environment variable counts as not set. Without either, the source is empty.
func (r optionResolver) lookup(flag, env string) (value, source string) {
	if r.cmd.Flags().Changed(flag) {
		return r.cmd.Flags().Lookup(flag).Value.String(), "flag --" + flag
	}

	if value = os.Getenv(env); value != "" {
		return value, "env " + env
	}

	return "", ""
}

// str resolves a string option. defaultSource describes where the value comes from if neither flag nor environment
// variable is given; empty if the option has no default.
func (r optionResolver) str(flag, env, defaultSource string) string {
	value, source := r.lookup(flag, env)

	if source == "" {
		source = "default"

		if defaultSource != "" {
			source = "default: " + defaultSource
		}
	}

	slog.Debug("resolved option", "option", flag, "value", value, "source", source)

	return value
}

// boolean resolves a boolean option. A value of the environment variable that is no boolean is an error.
func (r optionResolver) boolean(flag, env string) (bool, error) {
	value, source := r.lookup(flag, env)

	if source == "" {
		slog.Debug("resolved option", "option", flag, "value", false, "source", "default")

		return false, nil
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("invalid value %q of %s: must be a boolean like true or false", value, env)
	}

	slog.Debug("resolved option", "option", flag, "value", parsed, "source", source)

	return parsed, nil
}
