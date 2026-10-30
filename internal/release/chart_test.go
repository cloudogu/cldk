package release

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadChartMetadata(t *testing.T) {
	packed := func(chart string) func(t *testing.T) []byte {
		return func(t *testing.T) []byte {
			_, content := packChart(t, chart)

			return content
		}
	}

	static := func(file string) func(t *testing.T) []byte {
		return func(t *testing.T) []byte {
			content, err := os.ReadFile(filepath.Join("testdata", file))
			require.NoError(t, err)

			return content
		}
	}

	tests := []struct {
		name        string
		archive     func(t *testing.T) []byte
		wantName    string
		wantVersion string
		wantErr     string
	}{
		{name: "release version", archive: packed("nexus"), wantName: "nexus", wantVersion: "3.96.4-3"},
		{name: "prerelease version", archive: packed("redmine-prerelease"), wantName: "redmine", wantVersion: "6.1.2-dev.1"},
		{name: "archive without Chart.yaml", archive: static("without-chart-yaml.tgz"), wantErr: "Chart.yaml file is missing"},
		{name: "no gzipped tar archive", archive: static("not-a-chart.tgz"), wantErr: "failed to load chart archive"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta, err := loadChartMetadata(tt.archive(t))

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantName, meta.Name)
			assert.Equal(t, tt.wantVersion, meta.Version)
		})
	}
}
