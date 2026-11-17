package devpush

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"sigs.k8s.io/yaml"
)

// loadValues reads the values file of the options, relative to the project directory.
// Without a values file, or with an empty one, it returns nil.
func loadValues(opts Options) (map[string]any, error) {
	if opts.ValuesFile == "" {
		return nil, nil
	}

	path := opts.ValuesFile
	if !filepath.IsAbs(path) {
		path = filepath.Join(opts.Dir, path)
	}

	slog.Info("loading values", "file", path)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read values file: %w", err)
	}

	var parsed any
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("values file %s is no valid YAML: %w", path, err)
	}

	if parsed == nil {
		return nil, nil
	}

	values, ok := parsed.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("values file %s must contain a map of Helm values", path)
	}

	if len(values) == 0 {
		return nil, nil
	}

	return values, nil
}
