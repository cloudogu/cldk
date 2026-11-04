// Package build builds and pushes images with a Docker compatible build CLI, run as subprocess.
package build

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

// waitDelay is how long a builder may take to exit after it has been interrupted, before it is killed.
const waitDelay = 10 * time.Second

// BuildRequest describes an image to build. Paths are passed to the builder as given.
type BuildRequest struct {
	// Context is the build context.
	Context string
	// Dockerfile is the Dockerfile to build.
	Dockerfile string
	// Tag is the reference the built image is tagged with.
	Tag string
	// Target is the stage of a multi-stage build, optional.
	Target string
	// BuildArgs are the build arguments as "KEY=VALUE" or "KEY", which takes the value from the environment.
	BuildArgs []string
	// Platform is the platform to build for, e.g. "linux/arm64", optional.
	Platform string
}

// Builder runs a Docker compatible build CLI like docker or podman. The builder inherits the environment of cldk.
type Builder struct {
	// binary is the name or path of the build CLI.
	binary string
	// output receives stdout and stderr of the builder.
	output io.Writer
}

// New creates a builder that runs the given build CLI and writes its output to stderr.
func New(binary string) *Builder {
	return &Builder{binary: binary, output: os.Stderr}
}

// Check checks that the build CLI can be found.
func (b *Builder) Check(_ context.Context) error {
	if _, err := exec.LookPath(b.binary); err != nil {
		return fmt.Errorf("builder %q not found, install it or choose another one with --builder: %w", b.binary, err)
	}

	return nil
}

// Build builds the image of the request.
func (b *Builder) Build(ctx context.Context, req BuildRequest) error {
	args := []string{"build", "-f", req.Dockerfile, "-t", req.Tag}

	if req.Target != "" {
		args = append(args, "--target", req.Target)
	}

	for _, arg := range req.BuildArgs {
		args = append(args, "--build-arg", arg)
	}

	if req.Platform != "" {
		args = append(args, "--platform", req.Platform)
	}

	args = append(args, req.Context)

	return b.run(ctx, "build", args)
}

// Push pushes the image with the given reference.
func (b *Builder) Push(ctx context.Context, ref string) error {
	return b.run(ctx, "push", []string{"push", ref})
}

// run runs the builder with the arguments. On cancellation of the context, the builder is interrupted and killed if
// it does not exit in time.
func (b *Builder) run(ctx context.Context, action string, args []string) error {
	cmd := exec.CommandContext(ctx, b.binary, args...)
	cmd.Stdout = b.output
	cmd.Stderr = b.output
	cmd.Cancel = func() error {
		return cmd.Process.Signal(os.Interrupt)
	}
	cmd.WaitDelay = waitDelay

	// A canceled builder fails with the signal it got, so the cancellation is reported before the exit code.
	err := cmd.Run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("%s with %s canceled: %w", action, b.binary, ctxErr)
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return fmt.Errorf("%s failed with exit code %d of %s", action, exitErr.ExitCode(), b.binary)
	}

	if err != nil {
		return fmt.Errorf("failed to run %s: %w", b.binary, err)
	}

	return nil
}
