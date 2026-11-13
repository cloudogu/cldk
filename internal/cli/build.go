package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cloudogu/cldk/internal/project"
)

// Environment variables for the build settings, shared by all commands that build images.
const (
	envBuilder  = "CLDK_BUILDER"
	envPlatform = "CLDK_PLATFORM"
	envNoCache  = "CLDK_NO_CACHE"
)

// defaultBuilder is the build CLI if none is given.
const defaultBuilder = "docker"

// Flags of the build settings.
const (
	flagBuilder  = "builder"
	flagPlatform = "platform"
	flagNoCache  = "no-cache"
	flagBuildArg = "build-arg"
)

// buildFlags are the flags of the build settings.
var buildFlags = []string{flagBuilder, flagPlatform, flagNoCache, flagBuildArg}

// addBuildFlags adds the flags of the build settings to the command.
// Their values are resolved together with the environment, see resolveBuildOptions.
func addBuildFlags(cmd *cobra.Command) {
	cmd.Flags().String(flagBuilder, "", "Docker compatible build CLI the images are built and pushed with, default "+defaultBuilder+" (env: "+envBuilder+")")
	cmd.Flags().String(flagPlatform, "", "platform the images are built for as os/arch, passed to the builder (env: "+envPlatform+")")
	cmd.Flags().Bool(flagNoCache, false, "build the images without the cache of the builder (env: "+envNoCache+")")
	cmd.Flags().StringArray(flagBuildArg, nil, "build argument KEY=VALUE for all images, overrides buildArgs of cldk.yaml; KEY alone takes the value from the environment; repeatable")
}

// resolveBuildOptions resolves the build settings from the flags and the environment, flags first.
// The build arguments are taken from the flag only.
func resolveBuildOptions(r optionResolver) (project.BuildOptions, error) {
	noCache, noCacheErr := r.boolean(flagNoCache, envNoCache)
	buildArgs, buildArgsErr := r.stringArray(flagBuildArg)

	if err := errors.Join(noCacheErr, buildArgsErr); err != nil {
		return project.BuildOptions{}, err
	}

	if err := validateBuildArgs(buildArgs); err != nil {
		return project.BuildOptions{}, err
	}

	opts := project.BuildOptions{
		Builder:   r.str(flagBuilder, envBuilder, defaultBuilder),
		Platform:  r.str(flagPlatform, envPlatform, "platform of the builder"),
		NoCache:   noCache,
		BuildArgs: buildArgs,
	}

	if opts.Builder == "" {
		opts.Builder = defaultBuilder
	}

	return opts, nil
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
