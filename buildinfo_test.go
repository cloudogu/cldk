package main

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveVersion(t *testing.T) {
	buildInfo := func(moduleVersion string) *debug.BuildInfo {
		return &debug.BuildInfo{Main: debug.Module{Path: "github.com/cloudogu/cldk", Version: moduleVersion}}
	}

	tests := []struct {
		name    string
		version string
		info    *debug.BuildInfo
		ok      bool
		want    string
	}{
		{name: "keeps version injected via ldflags", version: "0.1.0", info: buildInfo("v0.2.0"), ok: true, want: "0.1.0"},
		{name: "uses module version of go install", version: developmentVersion, info: buildInfo("v0.1.0"), ok: true, want: "0.1.0"},
		{name: "uses pseudo version", version: developmentVersion, info: buildInfo("v0.0.0-20261111120000-abcdef123456"), ok: true, want: "0.0.0-20261111120000-abcdef123456"},
		{name: "keeps development for devel build", version: developmentVersion, info: buildInfo("(devel)"), ok: true, want: developmentVersion},
		{name: "keeps development for empty module version", version: developmentVersion, info: buildInfo(""), ok: true, want: developmentVersion},
		{name: "keeps development without build info", version: developmentVersion, info: nil, ok: false, want: developmentVersion},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, resolveVersion(tt.version, tt.info, tt.ok))
		})
	}
}
