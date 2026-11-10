package kube

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testKubeconfig has the current context dev1 with the namespace ecosystem and the context dev2 without a namespace.
const testKubeconfig = `apiVersion: v1
kind: Config
current-context: dev1
clusters:
  - name: dev1
    cluster:
      server: https://dev1.example.com:6443
  - name: dev2
    cluster:
      server: https://dev2.example.com:6443
users:
  - name: developer
    user:
      token: secret
contexts:
  - name: dev1
    context:
      cluster: dev1
      user: developer
      namespace: ecosystem
  - name: dev2
    context:
      cluster: dev2
      user: developer
`

// useKubeconfig writes the kubeconfig to a temporary file and points KUBECONFIG to it.
func useKubeconfig(t *testing.T, content string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	t.Setenv("KUBECONFIG", path)
}

func TestConnect(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want Info
	}{
		{
			name: "uses the current context and its namespace",
			want: Info{Context: "dev1", Server: "https://dev1.example.com:6443", Namespace: "ecosystem"},
		},
		{
			name: "uses the given context and the namespace default if the context has none",
			opts: Options{Context: "dev2"},
			want: Info{Context: "dev2", Server: "https://dev2.example.com:6443", Namespace: "default"},
		},
		{
			name: "uses the given namespace instead of the one of the context",
			opts: Options{Namespace: "personal"},
			want: Info{Context: "dev1", Server: "https://dev1.example.com:6443", Namespace: "personal"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useKubeconfig(t, testKubeconfig)

			cluster, err := Connect(tt.opts)

			require.NoError(t, err)
			assert.Equal(t, tt.want, cluster.Info)
			assert.NotNil(t, cluster.Dynamic)
		})
	}

	t.Run("aborts on an unknown context", func(t *testing.T) {
		useKubeconfig(t, testKubeconfig)

		_, err := Connect(Options{Context: "dev3"})

		require.ErrorContains(t, err, `context "dev3"`)
	})

	t.Run("aborts without a context", func(t *testing.T) {
		useKubeconfig(t, "apiVersion: v1\nkind: Config\n")

		_, err := Connect(Options{})

		require.ErrorContains(t, err, "no current context")
	})
}
