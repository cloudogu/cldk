package release

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/cloudogu/cldk/internal/chart"
	"github.com/stretchr/testify/require"
	helmchart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"sigs.k8s.io/yaml"
)

// Files of a chart that a release rewrites.
const (
	mirrorPatchTemplateFile = "mirror-patch-tpl.yaml"
	valuesFile              = "values.yaml"
)

// chartFile returns the content of the file with the given name inside a packed chart archive.
func chartFile(t *testing.T, archive []byte, name string) []byte {
	t.Helper()

	c, err := loader.LoadArchive(bytes.NewReader(archive))
	require.NoError(t, err)

	for _, f := range c.Raw {
		if f.Name == name {
			return f.Data
		}
	}

	require.Failf(t, "file not found in chart", "%s", name)

	return nil
}

// chartYAML returns the parsed content of the YAML file with the given name inside a packed chart archive.
func chartYAML(t *testing.T, archive []byte, name string) map[string]any {
	t.Helper()

	var content map[string]any
	require.NoError(t, yaml.Unmarshal(chartFile(t, archive, name), &content))

	return content
}

// packChartWith packs the test chart testdata/charts/<name> after modifying it and returns the archive.
func packChartWith(t *testing.T, name string, modify func(c *helmchart.Chart)) []byte {
	t.Helper()

	c, err := loader.LoadDir(filepath.Join("testdata", "charts", name))
	require.NoError(t, err)

	modify(c)

	archive, err := chart.Pack(c)
	require.NoError(t, err)

	return archive
}

// withMirrorPatchTemplate replaces the mirror patch template of a chart.
func withMirrorPatchTemplate(content string) func(c *helmchart.Chart) {
	return func(c *helmchart.Chart) {
		for _, f := range c.Files {
			if f.Name == mirrorPatchTemplateFile {
				f.Data = []byte(content)
			}
		}
	}
}
