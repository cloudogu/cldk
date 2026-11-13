package cldkfile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeProjectFile writes a cldk.yaml with the given content into a new temporary project directory and returns the
// directory.
func writeProjectFile(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cldk.yaml"), []byte(content), 0o600))

	return dir
}

func TestLoad(t *testing.T) {
	t.Run("applies the defaults to a minimal file", func(t *testing.T) {
		dir := writeProjectFile(t, "namespace: official\n")

		file, err := Load(dir)

		require.NoError(t, err)
		assert.Equal(t, ProjectFile{Namespace: "official", Chart: "k8s/helm"}, file)
	})

	t.Run("reads the complete schema and applies the defaults of the images", func(t *testing.T) {
		dir := writeProjectFile(t, `namespace: official
chart: helm/nexus
images:
  nexus:
    tag: 3.96.4-1
  sa-manager:
    context: sidecar
  postgres:
    context: db
    dockerfile: build/Dockerfile.postgres
    key: postgresql
    target: runtime
    buildArgs:
      FOO: bar
`)

		file, err := Load(dir)

		require.NoError(t, err)
		assert.Equal(t, ProjectFile{
			Namespace: "official",
			Chart:     "helm/nexus",
			Images: map[string]Image{
				"nexus":      {Context: ".", Dockerfile: "Dockerfile", Tag: "3.96.4-1"},
				"sa-manager": {Context: "sidecar", Dockerfile: filepath.Join("sidecar", "Dockerfile")},
				"postgres": {
					Context:    "db",
					Dockerfile: "build/Dockerfile.postgres",
					Key:        "postgresql",
					Target:     "runtime",
					BuildArgs:  map[string]string{"FOO": "bar"},
				},
			},
		}, file)
	})

	t.Run("names the expected path and a minimal example without a file", func(t *testing.T) {
		dir := t.TempDir()

		_, err := Load(dir)

		require.Error(t, err)
		assert.Contains(t, err.Error(), filepath.Join(dir, "cldk.yaml"))
		assert.Contains(t, err.Error(), "namespace: official")
	})

	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{name: "unknown field", content: "namespace: official\nchrat: k8s/helm\n", wantErr: "field chrat not found"},
		{name: "unknown field of an image", content: "namespace: official\nimages:\n  nexus:\n    dockerFile: x\n", wantErr: "field dockerFile not found"},
		{name: "missing namespace", content: "chart: k8s/helm\n", wantErr: "namespace is required"},
		{name: "image name with a slash", content: "namespace: official\nimages:\n  foo/nexus: {}\n", wantErr: `invalid image name "foo/nexus"`},
		{name: "image name with upper case", content: "namespace: official\nimages:\n  Nexus: {}\n", wantErr: `invalid image name "Nexus"`},
		{name: "a tag with a slash", content: "namespace: official\nimages:\n  nexus:\n    tag: 3.96/4\n", wantErr: `image nexus: invalid tag "3.96/4"`},
		{name: "a tag starting with a dot", content: "namespace: official\nimages:\n  nexus:\n    tag: .1\n", wantErr: `image nexus: invalid tag ".1"`},
		{name: "no YAML", content: "namespace: [official\n", wantErr: "failed to parse"},
	}

	for _, tt := range tests {
		t.Run("aborts on "+tt.name, func(t *testing.T) {
			dir := writeProjectFile(t, tt.content)

			_, err := Load(dir)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.Contains(t, err.Error(), filepath.Join(dir, "cldk.yaml"))
		})
	}
}
