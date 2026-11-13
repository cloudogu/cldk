package main

import (
	"runtime/debug"
	"strings"
)

const developmentVersion = "development"

// init falls back to the module version for binaries built without -ldflags, e.g. by go install.
func init() {
	info, ok := debug.ReadBuildInfo()
	Version = resolveVersion(Version, info, ok)
}

// resolveVersion keeps a version injected via -ldflags. Otherwise it returns the module version from the
// build info without its leading "v", so that both ways of building print the same version.
func resolveVersion(version string, info *debug.BuildInfo, ok bool) string {
	if version != developmentVersion || !ok || info == nil {
		return version
	}

	moduleVersion := info.Main.Version
	if moduleVersion == "" || moduleVersion == "(devel)" {
		return version
	}

	return strings.TrimPrefix(moduleVersion, "v")
}
