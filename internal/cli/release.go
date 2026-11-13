package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/cloudogu/cldk/internal/oci"
	"github.com/cloudogu/cldk/internal/release"
)

// Environment variables for the target of release.
const (
	envRegistry  = "CLDK_REG"
	envNamespace = "CLDK_NAMESPACE"
	envPlainHTTP = "CLDK_PLAIN_HTTP"
	envInsecure  = "CLDK_INSECURE"
	envUser      = "CLDK_USER"
	envPassword  = "CLDK_PASSWORD"
)

// defaultRegistry is the target registry of release if none is given.
const defaultRegistry = "registry.cloudogu.com"

// Flags of release besides those shared with dev-push.
const (
	flagUser     = "user"
	flagPassword = "pass"
	flagImage    = "image"
	flagImageKey = "image-key"
	flagForce    = "force"
)

// flagTag is the flag for the release tags of the built images.
const flagTag = "tag"

// projectFlags are the flags that only apply to a release from the project directory.
var projectFlags = append([]string{flagTag}, buildFlags...)

// Releaser plans and publishes a release, see release.Releaser.
type Releaser interface {
	Plan(ctx context.Context, chartPath string, target oci.Target, opts release.ArchiveOptions) (release.Plan, error)
	Release(ctx context.Context, chartPath string, target oci.Target, opts release.ArchiveOptions) (release.Result, error)
	PlanProject(ctx context.Context, target oci.Target, opts release.ProjectOptions) (release.Plan, error)
	ReleaseProject(ctx context.Context, target oci.Target, opts release.ProjectOptions) (release.Result, error)
}

const releaseLong = `Release a dogu v3 to a namespace of the target registry.

Without argument, release builds the images of cldk.yaml in the project directory (--dir), pushes them with their
release tag (tag: in cldk.yaml or --tag) and pushes the chart rewritten to them. The build CLI pushes with its own
login, e.g. from "docker login". An image given with -i is copied instead of built.

With a chart archive (.tgz) as argument, release copies the images given with -i and pushes the chart rewritten to
them. The namespace is required then.

All targets are checked before anything is built or pushed. Existing tags and chart versions abort the release unless
--force is given.`

const releaseExample = `  # cldk.yaml in the dogu repository
  namespace: official
  images:
    nexus:
      tag: 3.96.4-1

  # check the release of the dogu in the current directory and print the plan without building or pushing anything
  cldk dogu release --dry-run

  # release the dogu in the current directory to registry.cloudogu.com
  cldk dogu release

  # release a testing version to another namespace with another release tag
  cldk dogu release --namespace testing --tag nexus=3.96.4-1-rc.1

  # release to a local registry and overwrite an existing release
  cldk dogu release --reg localhost:5001 --plain-http --force

  # take the sidecar image built in CI instead of building it, build the other images of cldk.yaml
  cldk dogu release -i ci-registry.example.com/nexus/sidecar:1.2.0

  # release a packed chart with an image from CI
  cldk dogu release --namespace official -i ci-registry.example.com/nexus:3.96.4-1 nexus-1.2.3.tgz`

func newReleaseCommand(releaser Releaser) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "release [flags] [<chart.tgz>]",
		Short:   "Release a dogu v3 from its project directory or a packed helm chart",
		Long:    releaseLong,
		Example: releaseExample,
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r := optionResolver{cmd: cmd}

			if len(args) == 1 {
				return runArchiveRelease(cmd, r, releaser, args[0])
			}

			return runProjectRelease(cmd, r, releaser)
		},
	}

	// The values of the target flags are resolved together with the environment, see resolveTarget.
	cmd.Flags().StringArrayP(flagImage, "i", nil, "image to release with the chart as <ref>[=<name>:<tag>] (repeatable), e.g. ci-registry.example.com/nexus:3.96.4 or localhost:5001/local-dev/nexus/main:3.96.4=nexus:3.96.4; replaces the build of the image of cldk.yaml with the same name")
	cmd.Flags().StringArray(flagImageKey, nil, "assign an image of -i to values.images.<key> in mirror-patch-tpl.yaml as <name>=<key> (repeatable), e.g. nexus=nexus")
	cmd.Flags().StringArray(flagTag, nil, "release tag of an image of cldk.yaml as <name>=<tag> (repeatable), overrides tag: of cldk.yaml, e.g. nexus=3.96.4-1; not with <chart.tgz>")
	cmd.Flags().Bool(flagForce, false, "overwrite existing release tags, image tags with another digest and an already released chart version in the target registry")
	cmd.Flags().Bool(flagDryRun, false, "check the release and print the plan without building or pushing anything")
	cmd.Flags().String(flagRegistry, "", "target registry, default "+defaultRegistry+" (env: "+envRegistry+")")
	cmd.Flags().String(flagNamespace, "", "target namespace in the registry, default the namespace of cldk.yaml, required with <chart.tgz> (env: "+envNamespace+")")
	cmd.Flags().String(flagUser, "", "user for the target registry, default: stored helm/docker credentials; not used for pushing built images (env: "+envUser+")")
	cmd.Flags().String(flagPassword, "", "password for the target registry (env: "+envPassword+")")
	cmd.Flags().Bool(flagPlainHTTP, false, "allow unencrypted HTTP connections to the target registry (env: "+envPlainHTTP+")")
	cmd.Flags().Bool(flagInsecure, false, "allow TLS without certificate verification for the target registry, e.g. for self-signed certificates (env: "+envInsecure+")")
	addBuildFlags(cmd)

	return cmd
}

// runArchiveRelease releases the packed chart archive at chartPath.
func runArchiveRelease(cmd *cobra.Command, r optionResolver, releaser Releaser, chartPath string) error {
	// check if any project-flags were set
	for _, flag := range projectFlags {
		if cmd.Flags().Changed(flag) {
			return fmt.Errorf("--%s only applies to a release from the project directory, not to the chart archive %s", flag, chartPath)
		}
	}

	target, err := resolveTarget(r, "")
	if err != nil {
		return err
	}

	if target.Namespace == "" {
		return fmt.Errorf("a chart archive needs the namespace, give it with --%s or %s", flagNamespace, envNamespace)
	}

	opts, dryRun, err := resolveReleaseOptions(r)
	if err != nil {
		return err
	}

	if dryRun {
		plan, err := releaser.Plan(cmd.Context(), chartPath, target, opts)

		return printPlanAndConflicts(cmd.OutOrStdout(), plan, err)
	}

	// After a failed push, the result shows what already has been released.
	result, err := releaser.Release(cmd.Context(), chartPath, target, opts)

	return errors.Join(printResult(cmd.OutOrStdout(), result), err)
}

// runProjectRelease releases the project directory.
func runProjectRelease(cmd *cobra.Command, r optionResolver, releaser Releaser) error {
	target, err := resolveTarget(r, "namespace of cldk.yaml")
	if err != nil {
		return err
	}

	opts, dryRun, err := resolveReleaseOptions(r)
	if err != nil {
		return err
	}

	tags, tagsErr := r.stringArray(flagTag)
	buildOpts, buildErr := resolveBuildOptions(r)

	if err := errors.Join(tagsErr, buildErr); err != nil {
		return err
	}

	// --dir is a persistent flag of the root command.
	dir, err := cmd.Flags().GetString(flagDir)
	if err != nil {
		return err
	}

	projectOpts := release.ProjectOptions{
		Images: opts.Images, ImageKeys: opts.ImageKeys, Force: opts.Force, Dir: dir, Tags: tags, Build: buildOpts,
	}

	if dryRun {
		plan, err := releaser.PlanProject(cmd.Context(), target, projectOpts)

		return printPlanAndConflicts(cmd.OutOrStdout(), plan, err)
	}

	// After a failed push, the result shows what already has been released.
	result, err := releaser.ReleaseProject(cmd.Context(), target, projectOpts)

	return errors.Join(printResult(cmd.OutOrStdout(), result), err)
}

// printPlanAndConflicts prints the plan of a dry run and returns its conflicts, or returns err without a plan.
func printPlanAndConflicts(out io.Writer, plan release.Plan, err error) error {
	if err != nil {
		return err
	}

	return errors.Join(printPlan(out, plan), plan.Conflicts())
}

// resolveTarget resolves the target of release from the flags and the environment, flags first.
func resolveTarget(r optionResolver, namespaceDefault string) (oci.Target, error) {
	plainHTTP, plainHTTPErr := r.boolean(flagPlainHTTP, envPlainHTTP)
	insecure, insecureErr := r.boolean(flagInsecure, envInsecure)
	user, password, credentialsErr := resolveCredentials(r)

	if err := errors.Join(plainHTTPErr, insecureErr, credentialsErr); err != nil {
		return oci.Target{}, err
	}

	target := oci.Target{
		Registry:              r.str(flagRegistry, envRegistry, defaultRegistry),
		Namespace:             r.str(flagNamespace, envNamespace, namespaceDefault),
		User:                  user,
		Password:              password,
		PlainHTTP:             plainHTTP,
		InsecureSkipTLSVerify: insecure,
	}

	if target.Registry == "" {
		target.Registry = defaultRegistry
	}

	return target, nil
}

// resolveCredentials resolves user and password for the target registry.
func resolveCredentials(r optionResolver) (user, password string, err error) {
	flags := r.cmd.Flags()
	userSource, passwordSource := "flag --"+flagUser, "flag --"+flagPassword

	if flags.Changed(flagUser) || flags.Changed(flagPassword) {
		user = flags.Lookup(flagUser).Value.String()
		password = flags.Lookup(flagPassword).Value.String()
	} else {
		user, password = os.Getenv(envUser), os.Getenv(envPassword)
		userSource, passwordSource = "env "+envUser, "env "+envPassword
	}

	if user == "" && password == "" {
		userSource, passwordSource = "default: stored helm/docker credentials", "default: stored helm/docker credentials"
	}

	maskedPassword := ""

	if password != "" {
		maskedPassword = "***"
	}

	slog.Debug("resolved option", "option", flagUser, "value", user, "source", userSource)
	slog.Debug("resolved option", "option", flagPassword, "value", maskedPassword, "source", passwordSource)

	if (user == "") != (password == "") {
		return "", "", fmt.Errorf("user and password must be given together (--%s/--%s or %s/%s)", flagUser, flagPassword, envUser, envPassword)
	}

	return user, password, nil
}

// resolveReleaseOptions resolves the images, image keys, force and dry run of release from the flags.
func resolveReleaseOptions(r optionResolver) (opts release.ArchiveOptions, dryRun bool, err error) {
	flags := r.cmd.Flags()

	images, imagesErr := r.stringArray(flagImage)
	imageKeys, imageKeysErr := r.stringArray(flagImageKey)
	force, forceErr := flags.GetBool(flagForce)
	dryRun, dryRunErr := flags.GetBool(flagDryRun)

	if err := errors.Join(imagesErr, imageKeysErr, forceErr, dryRunErr); err != nil {
		return release.ArchiveOptions{}, false, err
	}

	return release.ArchiveOptions{Images: images, ImageKeys: imageKeys, Force: force}, dryRun, nil
}

// printResult prints the built and the copied images and the released chart, if it has been released.
func printResult(out io.Writer, result release.Result) error {
	for _, img := range result.BuiltImages {
		format := "Released image %s -> %s@%s\n"

		if img.Status == release.StatusOverwritten {
			format = "Released image %s -> %s@%s (overwritten)\n"
		}

		if _, err := fmt.Fprintf(out, format, img.Name, img.Reference, img.Digest); err != nil {
			return err
		}
	}

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

	if result.ChartDigest == "" {
		return nil
	}

	format := "Released chart oci://%s@%s\n"

	if result.ChartStatus == release.StatusOverwritten {
		format = "Released chart oci://%s@%s (overwritten)\n"
	}

	_, err := fmt.Fprintf(out, format, result.ChartReference, result.ChartDigest)

	return err
}

// printPlan prints what a dry run would release: the built and the copied images and the chart.
func printPlan(out io.Writer, plan release.Plan) error {
	if _, err := fmt.Fprintln(out, "Dry run, nothing has been pushed."); err != nil {
		return err
	}

	for _, img := range plan.BuiltImages {
		format := "Would build image %s -> %s\n"

		switch img.Status {
		case release.StatusOverwritten:
			format = "Would overwrite image %s -> %s\n"
		case release.StatusConflict:
			format = "Conflict for image %s -> %s (already released)\n"
		}

		if _, err := fmt.Fprintf(out, format, img.Name, img.Reference); err != nil {
			return err
		}
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
