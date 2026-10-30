package release

import (
	"bytes"
	"fmt"

	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
)

// loadChartMetadata reads the Chart.yaml of a packed chart archive.
func loadChartMetadata(archive []byte) (*chart.Metadata, error) {
	c, err := loader.LoadArchive(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("failed to load chart archive: %w", err)
	}

	return c.Metadata, nil
}
