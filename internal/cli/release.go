package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/cloudogu/cldk/internal/release"
)

// Environment variables used as fallback for the target registry credentials.
const (
	envUser     = "CLDK_USER"
	envPassword = "CLDK_PASSWORD"
)

// Releaser plans and publishes a release, see release.Releaser.
type Releaser interface {
	Plan(ctx context.Context, chartPath string, target release.Target, opts release.Options) (release.Plan, error)
	Release(ctx context.Context, chartPath string, target release.Target, opts release.Options) (release.Result, error)
}

func newReleaseCommand(releaser Releaser) *cobra.Command {
	var (
		target release.Target
		opts   release.Options
		dryRun bool
	)

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

			if dryRun {
				plan, err := releaser.Plan(cmd.Context(), args[0], target, opts)
				if err != nil {
					return err
				}

				// The plan is printed even with conflicts, so that all of them show up at once.
				return errors.Join(printPlan(cmd.OutOrStdout(), plan), plan.Conflicts())
			}

			result, err := releaser.Release(cmd.Context(), args[0], target, opts)
			if err != nil {
				return err
			}

			return printResult(cmd.OutOrStdout(), result)
		},
	}

	cmd.Flags().StringArrayVarP(&opts.Images, "image", "i", nil, "image to release with the chart as <ref>[=<name>:<tag>] (repeatable), e.g. ci-registry.example.com/nexus:3.96.4-3 or localhost:5001/local-dev/nexus/main:3.96.4-3=nexus:3.96.4-3")
	cmd.Flags().StringArrayVar(&opts.ImageKeys, "image-key", nil, "assign an image to values.images.<key> in mirror-patch-tpl.yaml as <name>=<key> (repeatable), e.g. nexus=nexus")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "overwrite image tags with another digest and an already released chart version in the target registry")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "check the release and print the plan without pushing anything")
	cmd.Flags().StringVar(&target.Registry, "reg", "registry.cloudogu.com", "target registry")
	cmd.Flags().StringVar(&target.Namespace, "namespace", "", "target namespace in the registry (required)")
	_ = cmd.MarkFlagRequired("namespace")
	cmd.Flags().StringVar(&target.User, "user", "", "user for the target registry (env: "+envUser+"), default: stored helm/docker credentials")
	cmd.Flags().StringVar(&target.Password, "pass", "", "password for the target registry (env: "+envPassword+")")
	cmd.Flags().BoolVar(&target.PlainHTTP, "plain-http", false, "allow unencrypted HTTP connections to the target registry")
	cmd.Flags().BoolVar(&target.InsecureSkipTLSVerify, "insecure", false, "allow TLS without certificate verification for the target registry, e.g. for self-signed certificates")

	return cmd
}

// printResult prints the released images and the released chart.
func printResult(out io.Writer, result release.Result) error {
	for _, img := range result.Images {
		format := "Released image %s -> %s@%s\n"

		switch img.Status {
		case release.StatusSkipped:
			format = "Skipped image %s -> %s@%s (already released)\n"
		case release.StatusOverwritten:
			format = "Released image %s -> %s@%s (overwritten)\n"
		}

		if _, err := fmt.Fprintf(out, format, img.Source, img.Reference, img.Digest); err != nil {
			return err
		}
	}

	format := "Released chart oci://%s@%s\n"

	if result.ChartStatus == release.StatusOverwritten {
		format = "Released chart oci://%s@%s (overwritten)\n"
	}

	_, err := fmt.Fprintf(out, format, result.ChartReference, result.ChartDigest)

	return err
}

// printPlan prints what a dry run would release: the images and the chart.
func printPlan(out io.Writer, plan release.Plan) error {
	if _, err := fmt.Fprintln(out, "Dry run, nothing has been pushed."); err != nil {
		return err
	}

	for _, img := range plan.Images {
		format := "Would push image %s -> %s@%s\n"

		switch img.Status {
		case release.StatusSkipped:
			format = "Would skip image %s -> %s@%s (already released)\n"
		case release.StatusOverwritten:
			format = "Would overwrite image %s -> %s@%s\n"
		case release.StatusConflict:
			format = "Conflict for image %s -> %s@%s (exists with another digest)\n"
		}

		if _, err := fmt.Fprintf(out, format, img.Source, img.Reference, img.Digest); err != nil {
			return err
		}
	}

	format := "Would release chart oci://%s\n"

	switch plan.ChartStatus {
	case release.StatusOverwritten:
		format = "Would overwrite chart oci://%s\n"
	case release.StatusConflict:
		format = "Conflict for chart oci://%s (already released)\n"
	}

	_, err := fmt.Fprintf(out, format, plan.ChartReference)

	return err
}
