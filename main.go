package main

import (
	"fmt"
	"os"

	"github.com/cloudogu/cldk/internal/cli"
)

// Version and CommitID are injected at build time via -ldflags (see build/make/build.mk).
var (
	Version  = "development"
	CommitID = "unknown"
)

func main() {
	cmd := cli.NewRootCommand(cli.BuildInfo{Version: Version, CommitID: CommitID})
	if err := cmd.Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
