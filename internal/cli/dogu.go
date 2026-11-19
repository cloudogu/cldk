package cli

import "github.com/spf13/cobra"

// newDoguCommand creates the dogu command, which groups the commands for dogus.
func newDoguCommand(releaser Releaser, devPusher DevPusher) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dogu",
		Short: "Release and develop dogu helm charts",
	}

	cmd.AddCommand(newReleaseCommand(releaser))
	cmd.AddCommand(newDevPushCommand(devPusher))

	return cmd
}
