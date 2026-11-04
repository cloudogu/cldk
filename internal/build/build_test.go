package build

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeArgsPrefix starts the line in which the fake builder writes its arguments as JSON to stdout. The fake builder
// is a subprocess, so it passes its arguments back through the output the test captures anyway.
const fakeArgsPrefix = "fake builder args: "

// Environment variables that switch the test binary into a fake builder.
const (
	// envFakeBuilder makes the test binary a fake builder if set to "true".
	envFakeBuilder = "CLDK_TEST_FAKE_BUILDER"
	// envFakeExitCode is the exit code of the fake builder.
	envFakeExitCode = "CLDK_TEST_FAKE_EXIT_CODE"
	// envFakeSleep makes the fake builder sleep before it exits, so that it can be canceled.
	envFakeSleep = "CLDK_TEST_FAKE_SLEEP"
)

func TestMain(m *testing.M) {
	if os.Getenv(envFakeBuilder) == "true" {
		os.Exit(runFakeBuilder())
	}

	os.Exit(m.Run())
}

// runFakeBuilder writes its arguments and a line to stdout and stderr each and returns the exit code.
func runFakeBuilder() int {
	content, err := json.Marshal(os.Args[1:])
	if err != nil {
		panic(err)
	}

	_, _ = fmt.Fprintln(os.Stdout, fakeArgsPrefix+string(content))
	_, _ = fmt.Fprintln(os.Stdout, "fake builder stdout")
	_, _ = fmt.Fprintln(os.Stderr, "fake builder stderr")

	if sleep := os.Getenv(envFakeSleep); sleep != "" {
		d, err := time.ParseDuration(sleep)
		if err != nil {
			panic(err)
		}

		time.Sleep(d)
	}

	code, _ := strconv.Atoi(os.Getenv(envFakeExitCode))

	return code
}

// fakeBuilder is a builder that runs the test binary as fake builder.
type fakeBuilder struct {
	*Builder
	output *bytes.Buffer
}

// newFakeBuilder returns a builder that runs the test binary as fake builder with the given exit code.
func newFakeBuilder(t *testing.T, exitCode int) *fakeBuilder {
	t.Helper()

	t.Setenv(envFakeBuilder, "true")
	t.Setenv(envFakeExitCode, strconv.Itoa(exitCode))

	output := &bytes.Buffer{}
	b := New(os.Args[0])
	b.output = output

	return &fakeBuilder{Builder: b, output: output}
}

// args returns the arguments the fake builder has been called with, taken from its output.
func (f *fakeBuilder) args(t *testing.T) []string {
	t.Helper()

	for line := range strings.Lines(f.output.String()) {
		if content, ok := strings.CutPrefix(line, fakeArgsPrefix); ok {
			var args []string
			require.NoError(t, json.Unmarshal([]byte(content), &args))

			return args
		}
	}

	require.Fail(t, "the fake builder has not been called", f.output.String())

	return nil
}

func TestBuilder_Check(t *testing.T) {
	t.Run("finds a builder on the PATH", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "podman"), []byte("#!/bin/sh\n"), 0o700))
		t.Setenv("PATH", dir)

		assert.NoError(t, New("podman").Check(context.Background()))
	})

	t.Run("fails on a missing builder", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())

		err := New("docker").Check(context.Background())

		require.ErrorContains(t, err, `builder "docker" not found`)
		assert.ErrorContains(t, err, "--builder")
	})
}

func TestBuilder_Build(t *testing.T) {
	t.Run("passes all options to the builder", func(t *testing.T) {
		b := newFakeBuilder(t, 0)

		err := b.Build(context.Background(), BuildRequest{
			Context:    "/project/app",
			Dockerfile: "/project/app/Dockerfile.nexus",
			Tag:        "localhost:5001/official/dogu/v3/images/nexus:3.96.4-3-dev.20261103143012",
			Target:     "runtime",
			BuildArgs:  []string{"FOO=bar", "HTTP_PROXY"},
			Platform:   "linux/arm64",
		})

		require.NoError(t, err)
		assert.Equal(t, []string{
			"build",
			"-f", "/project/app/Dockerfile.nexus",
			"-t", "localhost:5001/official/dogu/v3/images/nexus:3.96.4-3-dev.20261103143012",
			"--target", "runtime",
			"--build-arg", "FOO=bar",
			"--build-arg", "HTTP_PROXY",
			"--platform", "linux/arm64",
			"/project/app",
		}, b.args(t))
	})

	t.Run("omits the options that are not given", func(t *testing.T) {
		b := newFakeBuilder(t, 0)

		err := b.Build(context.Background(), BuildRequest{Context: ".", Dockerfile: "Dockerfile", Tag: "localhost:5001/a:1"})

		require.NoError(t, err)
		assert.Equal(t, []string{"build", "-f", "Dockerfile", "-t", "localhost:5001/a:1", "."}, b.args(t))
	})

	t.Run("writes the stdout and stderr of the builder to the output", func(t *testing.T) {
		b := newFakeBuilder(t, 0)

		require.NoError(t, b.Build(context.Background(), BuildRequest{Context: ".", Dockerfile: "Dockerfile", Tag: "a:1"}))

		assert.Contains(t, b.output.String(), "fake builder stdout")
		assert.Contains(t, b.output.String(), "fake builder stderr")
	})

	t.Run("returns the exit code of a failed build", func(t *testing.T) {
		b := newFakeBuilder(t, 3)

		err := b.Build(context.Background(), BuildRequest{Context: ".", Dockerfile: "Dockerfile", Tag: "a:1"})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "build failed with exit code 3")
	})

	t.Run("stops the builder when the context is canceled", func(t *testing.T) {
		b := newFakeBuilder(t, 0)
		t.Setenv(envFakeSleep, "1m")
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()

		start := time.Now()
		err := b.Build(ctx, BuildRequest{Context: ".", Dockerfile: "Dockerfile", Tag: "a:1"})

		require.Error(t, err)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Less(t, time.Since(start), 30*time.Second)
	})
}

func TestBuilder_Push(t *testing.T) {
	t.Run("pushes the reference", func(t *testing.T) {
		b := newFakeBuilder(t, 0)

		require.NoError(t, b.Push(context.Background(), "localhost:5001/official/dogu/v3/images/nexus:1"))

		assert.Equal(t, []string{"push", "localhost:5001/official/dogu/v3/images/nexus:1"}, b.args(t))
	})

	t.Run("returns the exit code of a failed push", func(t *testing.T) {
		b := newFakeBuilder(t, 1)

		err := b.Push(context.Background(), "localhost:5001/a:1")

		require.ErrorContains(t, err, "push failed with exit code 1")
	})
}
