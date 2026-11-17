package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/joho/godotenv"
)

// dotEnvFile is the name of the file in the project directory whose variables are loaded into the environment.
const dotEnvFile = ".env"

// loadDotEnv reads <dir>/.env and sets each of its variables that is not set in the environment yet.
// A missing file is not an error. It returns the sorted keys that were set from the file.
func loadDotEnv(dir string) ([]string, error) {
	path := filepath.Join(dir, dotEnvFile)

	values, err := godotenv.Read(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}

	// Unlike godotenv.Load, the variables are set here to learn which keys came from the file.
	var keys []string

	for key, value := range values {
		if _, set := os.LookupEnv(key); set {
			continue
		}

		if err := os.Setenv(key, value); err != nil {
			return nil, fmt.Errorf("failed to set %s from %s: %w", key, path, err)
		}

		keys = append(keys, key)
	}

	slices.Sort(keys)

	return keys, nil
}
