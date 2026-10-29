package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newVersionCommand(info BuildInfo) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version of cldk",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "cldk %s (commit %s)\n", info.Version, info.CommitID)
			return err
		},
	}
}
