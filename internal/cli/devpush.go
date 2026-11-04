package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cloudogu/cldk/internal/devpush"
)

// Environment variables for the settings of dev-push.
const (
	envDevRegistry        = "CLDK_DEV_REG"
	envDevClusterRegistry = "CLDK_DEV_CLUSTER_REG"
	envDevNamespace       = "CLDK_DEV_NAMESPACE"
	envDevPlainHTTP       = "CLDK_DEV_PLAIN_HTTP"
	envDevInsecure        = "CLDK_DEV_INSECURE"
)

// Flags of dev-push.
const (
	flagRegistry        = "reg"
	flagClusterRegistry = "cluster-reg"
	flagNamespace       = "namespace"
	flagPlainHTTP       = "plain-http"
	flagInsecure        = "insecure"
)

// DevPusher pushes a dogu under development, see devpush.Pusher.
type DevPusher interface {
	Push(ctx context.Context, opts devpush.Options) (devpush.Result, error)
}

const devPushLong = `Push a dogu v3 under development from its project directory to a dev registry.

dev-push reads cldk.yaml in the project directory (--dir, default the current directory), loads the chart directory
named there (default k8s/helm), sets a new dev version <Chart.yaml version>-dev.<UTC timestamp> in a copy of the
chart, lints it and pushes it to <reg>/<namespace>/dogu/v3/charts/<name>. Nothing in the project directory is changed.

cldk.yaml is required and names at least the dogu namespace. The environment (registry addresses, plain HTTP,
insecure TLS) is taken from the flags, the CLDK_DEV_* environment variables or the .env of the project directory,
in this order. Credentials for the dev registry are taken from the docker configuration, e.g. "docker login".`

const devPushExample = `  # .env in the dogu repository, e.g. for a k3d cluster with a local registry
  CLDK_DEV_REG=localhost:5001
  CLDK_DEV_CLUSTER_REG=k3d-registry-proxy.localhost:5000
  CLDK_DEV_PLAIN_HTTP=true

  # cldk.yaml in the dogu repository
  namespace: official

  # push the dogu in the current directory
  cldk dev-push

  # push the dogu in another directory to a personal namespace
  cldk --dir ../nexus dev-push --namespace personal`

// newDevPushCommand creates the dev-push command.
func newDevPushCommand(pusher DevPusher) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "dev-push [flags]",
		Short:   "Build, push and install a dogu v3 under development in a cluster",
		Long:    devPushLong,
		Example: devPushExample,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("dev-push takes no arguments, give the project directory with --dir, e.g. cldk --dir %s dev-push", args[0])
			}

			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			options, err := resolveDevPushOptions(optionResolver{cmd: cmd})
			if err != nil {
				return err
			}

			// --dir is a persistent flag of the root command.
			if options.Dir, err = cmd.Flags().GetString(flagDir); err != nil {
				return err
			}

			result, err := pusher.Push(cmd.Context(), options)
			if err != nil {
				return err
			}

			return printDevPushResult(cmd.OutOrStdout(), result)
		},
	}

	// The values of the flags are resolved together with the environment, see resolveDevPushOptions.
	cmd.Flags().String(flagRegistry, "", "address of the dev registry to push to as host[:port], required (env: "+envDevRegistry+")")
	cmd.Flags().String(flagClusterRegistry, "", "address of the dev registry as seen from the cluster as host[:port], default --reg (env: "+envDevClusterRegistry+")")
	cmd.Flags().String(flagNamespace, "", "dogu namespace in the dev registry, default the namespace of cldk.yaml (env: "+envDevNamespace+")")
	cmd.Flags().Bool(flagPlainHTTP, false, "allow unencrypted HTTP connections to the dev registry (env: "+envDevPlainHTTP+")")
	cmd.Flags().Bool(flagInsecure, false, "allow TLS without certificate verification for the dev registry (env: "+envDevInsecure+")")

	return cmd
}

// resolveDevPushOptions resolves the options of dev-push from the flags and the environment, flags first.
// Every resolved value is logged with its source.
func resolveDevPushOptions(r optionResolver) (devpush.Options, error) {
	plainHTTP, plainHTTPErr := r.boolean(flagPlainHTTP, envDevPlainHTTP)
	insecure, insecureErr := r.boolean(flagInsecure, envDevInsecure)

	if err := errors.Join(plainHTTPErr, insecureErr); err != nil {
		return devpush.Options{}, err
	}

	opts := devpush.Options{
		PlainHTTP: plainHTTP,
		Insecure:  insecure,
		Namespace: r.str(flagNamespace, envDevNamespace, "namespace of cldk.yaml"),
		Registry:  r.str(flagRegistry, envDevRegistry, ""),
	}

	if opts.Registry == "" {
		return devpush.Options{}, fmt.Errorf("the dev registry is required, give it with --%s or %s", flagRegistry, envDevRegistry)
	}

	opts.ClusterRegistry = r.str(flagClusterRegistry, envDevClusterRegistry, "--"+flagRegistry)

	if opts.ClusterRegistry == "" {
		opts.ClusterRegistry = opts.Registry
	}

	if err := validateRegistryAddress(flagRegistry, envDevRegistry, opts.Registry); err != nil {
		return devpush.Options{}, err
	}

	if err := validateRegistryAddress(flagClusterRegistry, envDevClusterRegistry, opts.ClusterRegistry); err != nil {
		return devpush.Options{}, err
	}

	slog.Debug("cluster address of the dev registry", "address", opts.ClusterRegistry)

	return opts, nil
}

// validateRegistryAddress checks that the address of a registry is given as host[:port], without scheme or path.
func validateRegistryAddress(flag, env, address string) error {
	parsed, err := url.Parse("//" + address)
	if err != nil || parsed.Host != address || parsed.User != nil || strings.ContainsAny(address, " \t") {
		return fmt.Errorf("invalid registry address %q of --%s/%s: must be given as host[:port] without scheme or path, e.g. localhost:5001",
			address, flag, env)
	}

	return nil
}

// printDevPushResult prints what has been pushed.
func printDevPushResult(out io.Writer, result devpush.Result) error {
	_, err := fmt.Fprintf(out, "Pushed chart  oci://%s\n", result.ChartReference)

	return err
}
