package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/cloudogu/cldk/internal/release"
)

// Environment variables used as fallback for the target registry credentials.
const (
	envUser     = "CLDK_USER"
	envPassword = "CLDK_PASSWORD"
)

// Releaser publishes a release, see release.Releaser.
type Releaser interface {
	Release(ctx context.Context, chartPath string, target release.Target) (release.Result, error)
}

func newReleaseCommand(releaser Releaser) *cobra.Command {
	var target release.Target

	cmd := &cobra.Command{
		Use:   "release [flags] <chart.tgz>",
		Short: "Release a packed dogu v3 helm chart",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if target.Namespace == "" {
				return errors.New("--namespace must not be empty")
			}

			// Credentials are taken as a pair from one source, flags or environment, never mixed.
			if !cmd.Flags().Changed("user") && !cmd.Flags().Changed("pass") {
				target.User = os.Getenv(envUser)
				target.Password = os.Getenv(envPassword)
			}

			if (target.User == "") != (target.Password == "") {
				return fmt.Errorf("user and password must be given together (--user/--pass or %s/%s)", envUser, envPassword)
			}

			result, err := releaser.Release(cmd.Context(), args[0], target)
			if err != nil {
				return err
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Released chart oci://%s@%s\n", result.ChartReference, result.ChartDigest)

			return err
		},
	}

	cmd.Flags().StringVar(&target.Registry, "reg", "registry.cloudogu.com", "target registry")
	cmd.Flags().StringVar(&target.Namespace, "namespace", "", "target namespace in the registry (required)")
	_ = cmd.MarkFlagRequired("namespace")
	cmd.Flags().StringVar(&target.User, "user", "", "user for the target registry (env: "+envUser+"), default: stored helm/docker credentials")
	cmd.Flags().StringVar(&target.Password, "pass", "", "password for the target registry (env: "+envPassword+")")
	cmd.Flags().BoolVar(&target.PlainHTTP, "plain-http", false, "allow unencrypted HTTP connections to registries")
	cmd.Flags().BoolVar(&target.InsecureSkipTLSVerify, "insecure", false, "allow TLS without certificate verification, e.g. for self-signed certificates")

	return cmd
}
