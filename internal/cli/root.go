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

// NewRootCommand creates the cldk root command with all subcommands.
func NewRootCommand(info BuildInfo, releaser Releaser) *cobra.Command {
	var verbose bool

	root := &cobra.Command{
		Use:           "cldk",
		Short:         "Cloudogu Development Kit for releasing and developing Dogu v3 Helm charts",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			level := slog.LevelInfo

			if verbose {
				level = slog.LevelDebug
			}

			slog.SetDefault(slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{Level: level})))
			slog.Debug("starting cldk", "version", info.Version, "commit", info.CommitID)
		},
	}

	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable debug logging")

	root.AddCommand(newVersionCommand(info))
	root.AddCommand(newReleaseCommand(releaser))

	return root
}
