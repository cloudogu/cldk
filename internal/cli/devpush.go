package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cloudogu/cldk/internal/chart"
	"github.com/cloudogu/cldk/internal/devpush"
	"github.com/cloudogu/cldk/internal/dogu"
	"github.com/cloudogu/cldk/internal/kube"
)

// Environment variables for the settings of dev-push.
const (
	envDevRegistry        = "CLDK_DEV_REG"
	envDevClusterRegistry = "CLDK_DEV_CLUSTER_REG"
	envDevNamespace       = "CLDK_DEV_NAMESPACE"
	envDevPlainHTTP       = "CLDK_DEV_PLAIN_HTTP"
	envDevInsecure        = "CLDK_DEV_INSECURE"
	envDevKubeContext     = "CLDK_DEV_KUBE_CONTEXT"
	envDevKubeNamespace   = "CLDK_DEV_KUBE_NAMESPACE"
	envDevBuilder         = "CLDK_DEV_BUILDER"
	envDevPlatform        = "CLDK_DEV_PLATFORM"
)

// defaultBuilder is the build CLI if none is given.
const defaultBuilder = "docker"

// Flags of dev-push.
const (
	flagRegistry        = "reg"
	flagClusterRegistry = "cluster-reg"
	flagNamespace       = "namespace"
	flagPlainHTTP       = "plain-http"
	flagInsecure        = "insecure"
	flagKubeContext     = "kube-context"
	flagKubeNamespace   = "kube-namespace"
	flagBuilder         = "builder"
	flagPlatform        = "platform"
	flagBuildArg        = "build-arg"
	flagReplaceImage    = "replace-image"
	flagSkipImage       = "skip-image"
	flagDryRun          = "dry-run"
)

// DevPusher pushes a dogu under development, see devpush.Pusher.
type DevPusher interface {
	Plan(ctx context.Context, opts devpush.Options) (devpush.Plan, error)
	Push(ctx context.Context, opts devpush.Options) (devpush.Result, error)
}

const devPushLong = `Push a dogu v3 under development to a dev registry and install it in a cluster.

dev-push reads cldk.yaml in the project directory and creates a new dev version <Chart.yaml version>-dev.<UTC timestamp>.
It builds each image of cldk.yaml with the build CLI (docker by default), tags it with the dev version and pushes it
to the dev registry. A copy of the chart gets the dev version and the references of the built images on the cluster
address of the dev registry; it is linted before the first build and pushed after the images. Finally, dev-push applies
the dogu with this version and the dev chart in the cluster of the current kubeconfig context. The project directory
is not changed.

Settings are taken from the flags, the CLDK_DEV_* environment variables or the .env of the project directory, in this
order. See the README for the dev mode of the dogu operator and for switching back to the regular dogu.`

const devPushExample = `  # .env in the dogu repository, e.g. for a k3d cluster with a local registry
  CLDK_DEV_REG=localhost:5001
  CLDK_DEV_CLUSTER_REG=k3d-registry-proxy.localhost:5000
  CLDK_DEV_PLAIN_HTTP=true

  # cldk.yaml in the dogu repository
  namespace: official
  images:
    nexus: {}

  # push the dogu in the current directory
  cldk dev-push

  # check the dev push and print the plan and the dogu resource without pushing anything
  cldk dev-push --dry-run

  # push the dogu in another directory to a personal namespace
  cldk --dir ../nexus dev-push --namespace personal

  # push the dogu to another cluster
  cldk dev-push --kube-context k3d-dev2 --kube-namespace ecosystem

  # build the images with podman for arm64 with a proxy from the environment
  cldk dev-push --builder podman --platform linux/arm64 --build-arg HTTP_PROXY

  # build nexus, take an existing sidecar image and leave the postgres image of the chart as it is
  cldk dev-push --replace-image sidecar=registry.example.com/foo/sidecar:1.0 --skip-image postgres`

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

			dryRun, err := cmd.Flags().GetBool(flagDryRun)
			if err != nil {
				return err
			}

			if dryRun {
				plan, err := pusher.Plan(cmd.Context(), options)
				if err != nil {
					return err
				}

				return printDevPushPlan(cmd.OutOrStdout(), plan)
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
	cmd.Flags().String(flagKubeContext, "", "kubeconfig context of the cluster, default the current context (env: "+envDevKubeContext+")")
	cmd.Flags().String(flagKubeNamespace, "", "Kubernetes namespace of the dogu, default the namespace of the context, otherwise default (env: "+envDevKubeNamespace+")")
	cmd.Flags().String(flagBuilder, "", "Docker compatible build CLI the images are built and pushed with, default "+defaultBuilder+" (env: "+envDevBuilder+")")
	cmd.Flags().String(flagPlatform, "", "platform the images are built for as os/arch, passed to the builder (env: "+envDevPlatform+")")
	cmd.Flags().StringArray(flagBuildArg, nil, "build argument KEY=VALUE for all images, overrides buildArgs of cldk.yaml; KEY alone takes the value from the environment; repeatable")
	cmd.Flags().StringArray(flagReplaceImage, nil, "image of cldk.yaml as <name>=<reference> that is not built, but written into the chart with this reference; repeatable")
	cmd.Flags().StringArray(flagSkipImage, nil, "image of cldk.yaml that is neither built nor written into the chart; repeatable")
	cmd.Flags().Bool(flagDryRun, false, "run all checks including the cluster connection and the lint, print the plan and the dogu resource, but build, push and apply nothing")

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

	buildArgs := r.stringArray(flagBuildArg)

	if err := validateBuildArgs(buildArgs); err != nil {
		return devpush.Options{}, err
	}

	replaceImages, skipImages, err := resolveImageSelection(r.stringArray(flagReplaceImage), r.stringArray(flagSkipImage))
	if err != nil {
		return devpush.Options{}, err
	}

	opts := devpush.Options{
		PlainHTTP: plainHTTP,
		Insecure:  insecure,
		Namespace: r.str(flagNamespace, envDevNamespace, "namespace of cldk.yaml"),
		Registry:  r.str(flagRegistry, envDevRegistry, ""),
		Kube: kube.Options{
			Context:   r.str(flagKubeContext, envDevKubeContext, "current context"),
			Namespace: r.str(flagKubeNamespace, envDevKubeNamespace, "namespace of the context"),
		},
		Builder:       r.str(flagBuilder, envDevBuilder, defaultBuilder),
		Platform:      r.str(flagPlatform, envDevPlatform, "platform of the builder"),
		BuildArgs:     buildArgs,
		ReplaceImages: replaceImages,
		SkipImages:    skipImages,
	}

	if opts.Builder == "" {
		opts.Builder = defaultBuilder
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

// resolveImageSelection parses the images to replace, given as <name>=<reference>, and checks that each image is
// given only once in both flags and has a valid reference with registry. Without images, both results are nil.
func resolveImageSelection(replacements, skips []string) (map[string]string, []string, error) {
	var replaceImages map[string]string

	for _, replacement := range replacements {
		imageName, ref, _ := strings.Cut(replacement, "=")

		if imageName == "" || ref == "" {
			return nil, nil, fmt.Errorf("invalid image replacement %q of --%s: must be given as <name>=<reference>, e.g. sidecar=registry.example.com/foo:1.0",
				replacement, flagReplaceImage)
		}

		if _, ok := replaceImages[imageName]; ok {
			return nil, nil, fmt.Errorf("image %q is given more than once in --%s", imageName, flagReplaceImage)
		}

		if err := chart.ValidateImageRef(ref); err != nil {
			return nil, nil, fmt.Errorf("invalid image replacement %q of --%s: %w", replacement, flagReplaceImage, err)
		}

		if replaceImages == nil {
			replaceImages = map[string]string{}
		}

		replaceImages[imageName] = ref
	}

	for i, imageName := range skips {
		if imageName == "" {
			return nil, nil, fmt.Errorf("invalid image name %q of --%s", imageName, flagSkipImage)
		}

		if slices.Contains(skips[:i], imageName) {
			return nil, nil, fmt.Errorf("image %q is given more than once in --%s", imageName, flagSkipImage)
		}

		if _, ok := replaceImages[imageName]; ok {
			return nil, nil, fmt.Errorf("image %q is given in both --%s and --%s", imageName, flagReplaceImage, flagSkipImage)
		}
	}

	return replaceImages, skips, nil
}

// validateBuildArgs checks that each build argument is given as KEY=VALUE or KEY, with a key without whitespace.
func validateBuildArgs(args []string) error {
	for _, arg := range args {
		key, _, _ := strings.Cut(arg, "=")

		if key == "" || strings.ContainsAny(key, " \t\n") {
			return fmt.Errorf("invalid build argument %q of --%s: must be given as KEY=VALUE or KEY, e.g. HTTP_PROXY=http://proxy:3128",
				arg, flagBuildArg)
		}
	}

	return nil
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

// printDevPushResult prints what has been pushed and applied and how to watch the dogu.
func printDevPushResult(out io.Writer, result devpush.Result) error {
	for _, img := range result.Images {
		if _, err := fmt.Fprintf(out, "Pushed image  %s\n", img.Reference); err != nil {
			return err
		}
	}

	for _, img := range result.ReplacedImages {
		if _, err := fmt.Fprintf(out, "Replaced image %s -> %s\n", img.Name, img.Reference); err != nil {
			return err
		}
	}

	for _, imageName := range result.SkippedImages {
		if _, err := fmt.Fprintf(out, "Skipped image %s\n", imageName); err != nil {
			return err
		}
	}

	cluster := result.Cluster
	_, err := fmt.Fprintf(out, "Pushed chart  oci://%s\nApplied dogu %s %s (context %s, namespace %s)\nWatch: kubectl --context %s -n %s get dogu %s -w\n",
		result.ChartReference,
		result.Dogu, result.Version, cluster.Context, cluster.Namespace,
		cluster.Context, cluster.Namespace, result.Dogu)

	return err
}

// printDevPushPlan prints what a dry run would build, push and apply, with the dogu resource as YAML.
func printDevPushPlan(out io.Writer, plan devpush.Plan) error {
	resource, err := dogu.YAML(plan.DevChart)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintln(out, "Dry run, nothing has been built, pushed or applied."); err != nil {
		return err
	}

	for _, img := range plan.Images {
		if _, err := fmt.Fprintf(out, "Would build image %s -> %s\n", img.Name, img.Reference); err != nil {
			return err
		}
	}

	for _, img := range plan.ReplacedImages {
		if _, err := fmt.Fprintf(out, "Would replace image %s -> %s\n", img.Name, img.Reference); err != nil {
			return err
		}
	}

	for _, imageName := range plan.SkippedImages {
		if _, err := fmt.Fprintf(out, "Would skip image %s\n", imageName); err != nil {
			return err
		}
	}

	_, err = fmt.Fprintf(out, "Would push chart oci://%s\nWould apply dogu %s %s (context %s, namespace %s):\n%s",
		plan.ChartReference, plan.DevChart.Name, plan.Version, plan.Cluster.Context, plan.Cluster.Namespace, resource)

	return err
}
