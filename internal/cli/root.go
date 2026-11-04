// Package cli contains the cobra commands of cldk.
package cli

import (
	"log/slog"

	"github.com/spf13/cobra"
)

// BuildInfo holds the values injected into the binary at build time.
type BuildInfo struct {
	Version  string
	CommitID string
}

// flagDir is the persistent flag of the root command for the project directory.
const flagDir = "dir"

// NewRootCommand creates the cldk root command with all subcommands.
func NewRootCommand(info BuildInfo, releaser Releaser, devPusher DevPusher) *cobra.Command {
	var (
		verbose bool
		dir     string
	)

	root := &cobra.Command{
		Use:           "cldk",
		Short:         "Cloudogu Development Kit for releasing and developing Dogu v3 Helm charts",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			level := slog.LevelInfo

			if verbose {
				level = slog.LevelDebug
			}

			slog.SetDefault(slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{Level: level})))
			slog.Debug("starting cldk", "version", info.Version, "commit", info.CommitID)

			keys, err := loadDotEnv(dir)
			if err != nil {
				return err
			}

			if len(keys) > 0 {
				slog.Debug("loaded variables from .env", "dir", dir, "keys", keys)
			}

			return nil
		},
	}

	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable debug logging")
	root.PersistentFlags().StringVarP(&dir, flagDir, "d", ".", "project directory, its .env is loaded into the environment")

	root.AddCommand(newVersionCommand(info))
	root.AddCommand(newReleaseCommand(releaser))
	root.AddCommand(newDevPushCommand(devPusher))

	return root
}
