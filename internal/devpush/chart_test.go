package devpush

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPush_chart(t *testing.T) {
	t.Run("pushes the chart under the dev version without changing the project directory", func(t *testing.T) {
		reg := newTestRegistry(t)
		dir := writeProject(t, "namespace: official\n", nil)
		before := snapshotDir(t, dir)

		result, err := newTestPusher().Push(context.Background(), Options{Dir: dir, Registry: reg, PlainHTTP: true})

		require.NoError(t, err)
		wantRef := reg + "/official/dogu/v3/charts/nexus:" + wantDevVersion
		assert.Equal(t, wantDevVersion, result.Version)
		assert.Equal(t, wantRef, result.ChartReference)
		assert.True(t, strings.HasPrefix(result.ChartDigest, "sha256:"), result.ChartDigest)

		pushed := pullChart(t, wantRef)
		original := loadTestChart(t)
		assert.Equal(t, wantDevVersion, pushed.Metadata.Version)
		assert.Equal(t, "3.96.4", pushed.Metadata.AppVersion, "appVersion should be kept")
		assert.Equal(t, original.Values, pushed.Values, "values should be unchanged")
		assert.Equal(t, fileContents(original.Templates), fileContents(pushed.Templates), "templates should be unchanged")
		assert.Equal(t, fileContents(original.Files), fileContents(pushed.Files), "files should be unchanged")

		assert.Equal(t, before, snapshotDir(t, dir), "the project directory should be unchanged")
	})

	for _, tt := range []abortCase{
		{name: "a missing chart", cldkYAML: "namespace: official\nchart: helm\n", wantErr: "failed to load chart"},
		{
			name:       "lint errors",
			cldkYAML:   "namespace: official\n",
			chartFiles: map[string]string{"values.schema.json": `{"type": "object", "required": ["missingValue"]}`},
			wantErr:    "missingValue",
		},
	} {
		t.Run("aborts without pushing on "+tt.name, func(t *testing.T) {
			assertAbortsWithoutPushing(t, tt)
		})
	}
}

func TestPush_version(t *testing.T) {
	t.Run("keeps a prerelease of the chart version", func(t *testing.T) {
		pusher := newMockedPusher()
		dir := writeProject(t, "namespace: official\n", map[string]string{
			"Chart.yaml": "apiVersion: v2\nname: nexus\nversion: 1.2.3-rc.1\n",
		})

		result, err := pusher.Push(context.Background(), Options{Dir: dir, Registry: "localhost:5001"})

		require.NoError(t, err)
		assert.Equal(t, "1.2.3-rc.1-dev.20261103143012", result.Version)
		assert.Len(t, pusher.chartPusher.PushChartCalls(), 1)
	})

	for _, tt := range []abortCase{
		{
			name:       "a chart version that is no strict semantic version",
			cldkYAML:   "namespace: official\n",
			chartFiles: map[string]string{"Chart.yaml": "apiVersion: v2\nname: nexus\nversion: 3.96\n"},
			wantErr:    `version "3.96" of chart nexus is no valid semantic version`,
		},
		{
			name:       "a chart version with build metadata",
			cldkYAML:   "namespace: official\n",
			chartFiles: map[string]string{"Chart.yaml": "apiVersion: v2\nname: nexus\nversion: 1.2.3+build.1\n"},
			wantErr:    `version "1.2.3+build.1" of chart nexus has build metadata`,
		},
	} {
		t.Run("aborts without pushing on "+tt.name, func(t *testing.T) {
			assertAbortsWithoutPushing(t, tt)
		})
	}
}
