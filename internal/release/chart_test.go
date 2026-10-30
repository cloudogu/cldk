package release

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// authRecordingRegistry starts a server without TLS that asks for basic auth and records the Authorization headers
// it receives. It returns the host of the server and a function that returns the recorded headers.
func authRecordingRegistry(t *testing.T) (host string, recorded func() []string) {
	t.Helper()

	var mu sync.Mutex

	var headers []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		if auth := r.Header.Get("Authorization"); auth != "" {
			headers = append(headers, auth)
			w.WriteHeader(http.StatusNotFound)

			return
		}

		w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	return strings.TrimPrefix(srv.URL, "http://"), func() []string {
		mu.Lock()
		defer mu.Unlock()

		return append([]string(nil), headers...)
	}
}

func basicAuth(user, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
}

func TestNewChartClient(t *testing.T) {
	tests := []struct {
		name             string
		target           Target
		dockerCredential string
		wantAuth         []string
		wantErr          string
	}{
		{
			name:     "sends the credentials of the target",
			target:   Target{User: "releaser", Password: "s3cret", PlainHTTP: true},
			wantAuth: []string{basicAuth("releaser", "s3cret")},
		},
		{
			name:             "credentials of the target take precedence over stored credentials",
			target:           Target{User: "releaser", Password: "s3cret", PlainHTTP: true},
			dockerCredential: "someone:wrong",
			wantAuth:         []string{basicAuth("releaser", "s3cret")},
		},
		{
			name:             "sends the stored docker credentials without credentials of the target",
			target:           Target{PlainHTTP: true},
			dockerCredential: "docker-user:docker-secret",
			wantAuth:         []string{basicAuth("docker-user", "docker-secret")},
		},
		{
			name:    "fails without any credentials",
			target:  Target{PlainHTTP: true},
			wantErr: "credential not found",
		},
		{
			name:    "requires TLS without PlainHTTP",
			target:  Target{User: "releaser", Password: "s3cret"},
			wantErr: "server gave HTTP response to HTTPS client",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dockerConfig, _ := isolateCredentialConfig(t)
			host, recorded := authRecordingRegistry(t)

			if tt.dockerCredential != "" {
				auth := base64.StdEncoding.EncodeToString([]byte(tt.dockerCredential))
				config := `{"auths":{"` + host + `":{"auth":"` + auth + `"}}}`
				require.NoError(t, os.WriteFile(filepath.Join(dockerConfig, "config.json"), []byte(config), 0o600))
			}

			client, err := newChartClient(tt.target)
			require.NoError(t, err)

			_, err = client.Resolve(host + "/testing/dogu/v3/charts/nexus:3.96.4-3")

			require.Error(t, err) // the registry is empty, so the chart is never found
			if tt.wantErr != "" {
				assert.Contains(t, err.Error(), tt.wantErr)
			}

			assert.Equal(t, tt.wantAuth, recorded())
		})
	}
}
