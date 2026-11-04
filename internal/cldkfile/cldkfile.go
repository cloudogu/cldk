// Package cldkfile reads the project file cldk.yaml, which describes how a dogu under development is built.
package cldkfile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"go.yaml.in/yaml/v3"

	"github.com/cloudogu/cldk/internal/oci"
)

const (
	// FileName is the name of the project file in the project directory.
	FileName = "cldk.yaml"
	// DefaultChart is the chart directory if the project file names none.
	DefaultChart = "k8s/helm"
	// defaultContext is the build context of an image if the project file names none.
	defaultContext = "."
	// defaultDockerfile is the name of the Dockerfile in the build context if the project file names none.
	defaultDockerfile = "Dockerfile"
)

// ProjectFile is the content of the project file. All paths are relative to the project directory.
type ProjectFile struct {
	// Namespace is the dogu namespace in the registry, e.g. "official".
	Namespace string `yaml:"namespace"`
	// Chart is the chart directory, by default DefaultChart.
	Chart string `yaml:"chart,omitempty"`
	// Images are the images to build, by image name. The image name is the last path segment of the image repository.
	Images map[string]Image `yaml:"images,omitempty"`
}

// Image describes how an image is built.
type Image struct {
	// Context is the build context, by default the project directory.
	Context string `yaml:"context,omitempty"`
	// Dockerfile is the Dockerfile, by default the Dockerfile in the build context.
	Dockerfile string `yaml:"dockerfile,omitempty"`
	// Key is the key of the image in values.images of the mirror patch template of the chart. Without one, the
	// image is assigned to the entry whose repository has the image name.
	Key string `yaml:"key,omitempty"`
	// Target is the stage of a multi-stage build, optional.
	Target string `yaml:"target,omitempty"`
	// BuildArgs are static build arguments.
	BuildArgs map[string]string `yaml:"buildArgs,omitempty"`
}

// Load reads the project file in the project directory, validates it and applies the defaults.
// Unknown fields are errors.
func Load(dir string) (ProjectFile, error) {
	path := filepath.Join(dir, FileName)

	content, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return ProjectFile{}, fmt.Errorf("project file %s not found, create it with at least the dogu namespace, e.g.:\n\nnamespace: official", path)
	}

	if err != nil {
		return ProjectFile{}, fmt.Errorf("failed to read project file %s: %w", path, err)
	}

	file, err := parse(content)
	if err != nil {
		return ProjectFile{}, fmt.Errorf("failed to parse project file %s: %w", path, err)
	}

	if err = file.validate(); err != nil {
		return ProjectFile{}, fmt.Errorf("invalid project file %s: %w", path, err)
	}

	file.applyDefaults()

	return file, nil
}

// parse parses the content. Unknown fields are errors, so that typos are not silently ignored.
func parse(content []byte) (ProjectFile, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)

	var file ProjectFile

	// An empty file has no document at all.
	if err := decoder.Decode(&file); err != nil && !errors.Is(err, io.EOF) {
		return ProjectFile{}, err
	}

	return file, nil
}

// validate checks the required fields and the image names.
func (f *ProjectFile) validate() error {
	if f.Namespace == "" {
		return errors.New("namespace is required")
	}

	for _, imageName := range slices.Sorted(maps.Keys(f.Images)) {
		if err := oci.ValidateImageName(imageName); err != nil {
			return err
		}
	}

	return nil
}

// applyDefaults sets the defaults of all fields that are not given.
func (f *ProjectFile) applyDefaults() {
	if f.Chart == "" {
		f.Chart = DefaultChart
	}

	for imageName, img := range f.Images {
		if img.Context == "" {
			img.Context = defaultContext
		}

		if img.Dockerfile == "" {
			img.Dockerfile = filepath.Join(img.Context, defaultDockerfile)
		}

		f.Images[imageName] = img
	}
}
