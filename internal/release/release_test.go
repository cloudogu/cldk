package release

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	chartutil "helm.sh/helm/v4/pkg/chart/v2/util"
	helmregistry "helm.sh/helm/v4/pkg/registry"
)

const chartReference = "/testing/dogu/v3/charts/nexus:3.96.4-3"

// newRegistryHandler returns an in-memory OCI registry that does not log.
func newRegistryHandler() http.Handler {
	return registry.New(registry.Logger(log.New(io.Discard, "", 0)))
}

// newTargetRegistry starts an in-memory OCI registry without TLS and returns its host (e.g. "127.0.0.1:12345").
func newTargetRegistry(t *testing.T) string {
	t.Helper()

	srv := httptest.NewServer(newRegistryHandler())
	t.Cleanup(srv.Close)

	return strings.TrimPrefix(srv.URL, "http://")
}

// newTargetRegistryWithBasicAuth starts an in-memory OCI registry without TLS that requires the given credentials.
func newTargetRegistryWithBasicAuth(t *testing.T, user, password string) string {
	t.Helper()

	handler := newRegistryHandler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != user || p != password {
			w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	return strings.TrimPrefix(srv.URL, "http://")
}

// testTarget returns a target for the namespace "testing" of a test registry without TLS.
func testTarget(reg string) Target {
	return Target{Registry: reg, Namespace: "testing", PlainHTTP: true}
}

// packChart packs the test chart testdata/charts/<name> like "helm package" does and returns the path and content of
// the archive.
func packChart(t *testing.T, name string) (path string, content []byte) {
	t.Helper()

	c, err := loader.LoadDir(filepath.Join("testdata", "charts", name))
	require.NoError(t, err)

	path, err = chartutil.Save(c, t.TempDir())
	require.NoError(t, err)

	content, err = os.ReadFile(path)
	require.NoError(t, err)

	return path, content
}

func sha256Digest(data []byte) string {
	sum := sha256.Sum256(data)

	return "sha256:" + hex.EncodeToString(sum[:])
}

// isolateCredentialConfig points the docker and helm credential configuration to empty directories and returns them.
func isolateCredentialConfig(t *testing.T) (dockerConfig, helmConfig string) {
	t.Helper()

	dockerConfig = t.TempDir()
	helmConfig = t.TempDir()
	t.Setenv("DOCKER_CONFIG", dockerConfig)
	t.Setenv("HELM_CONFIG_HOME", helmConfig)

	return dockerConfig, helmConfig
}

func TestReleaseChart(t *testing.T) {
	t.Run("pushes the unchanged chart to the chart repository of the target", func(t *testing.T) {
		reg := newTargetRegistry(t)
		chartPath, chartContent := packChart(t, "nexus")

		_, err := NewReleaser().Release(context.Background(), chartPath, testTarget(reg), Options{})
		require.NoError(t, err)

		ref, err := name.ParseReference(reg + chartReference)
		require.NoError(t, err)

		img, err := remote.Image(ref)
		require.NoError(t, err)

		manifest, err := img.Manifest()
		require.NoError(t, err)
		assert.Equal(t, "application/vnd.cncf.helm.config.v1+json", string(manifest.Config.MediaType))
		require.Len(t, manifest.Layers, 1)
		assert.Equal(t, "application/vnd.cncf.helm.chart.content.v1.tar+gzip", string(manifest.Layers[0].MediaType))
		assert.Equal(t, sha256Digest(chartContent), manifest.Layers[0].Digest.String())
	})

	t.Run("pushed chart can be pulled and loaded with the helm registry client", func(t *testing.T) {
		reg := newTargetRegistry(t)
		chartPath, chartContent := packChart(t, "nexus")

		_, err := NewReleaser().Release(context.Background(), chartPath, testTarget(reg), Options{})
		require.NoError(t, err)

		client, err := helmregistry.NewClient(helmregistry.ClientOptPlainHTTP())
		require.NoError(t, err)

		pulled, err := client.Pull(reg + chartReference)
		require.NoError(t, err)
		assert.Equal(t, chartContent, pulled.Chart.Data)
		assert.Equal(t, "nexus", pulled.Chart.Meta.Name)
		assert.Equal(t, "3.96.4-3", pulled.Chart.Meta.Version)

		loaded, err := loader.LoadArchive(bytes.NewReader(pulled.Chart.Data))
		require.NoError(t, err)
		assert.Equal(t, "nexus", loaded.Metadata.Name)
		assert.Equal(t, "3.96.4-3", loaded.Metadata.Version)
	})

	t.Run("returns the reference and digest of the released chart", func(t *testing.T) {
		reg := newTargetRegistry(t)
		chartPath, _ := packChart(t, "nexus")

		result, err := NewReleaser().Release(context.Background(), chartPath, testTarget(reg), Options{})
		require.NoError(t, err)

		ref, err := name.ParseReference(reg + chartReference)
		require.NoError(t, err)

		desc, err := remote.Head(ref)
		require.NoError(t, err)
		assert.Equal(t, reg+chartReference, result.ChartReference)
		assert.Equal(t, desc.Digest.String(), result.ChartDigest)
	})

	t.Run("fails if the chart file does not exist", func(t *testing.T) {
		reg := newTargetRegistry(t)

		_, err := NewReleaser().Release(context.Background(), "/does/not/exist.tgz", testTarget(reg), Options{})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "/does/not/exist.tgz")
	})

	t.Run("fails if the chart file is no chart archive", func(t *testing.T) {
		reg := newTargetRegistry(t)

		_, err := NewReleaser().Release(context.Background(), filepath.Join("testdata", "not-a-chart.tgz"), testTarget(reg), Options{})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a valid chart archive")
	})
}

func TestReleaseAuthentication(t *testing.T) {
	assertChartReleased := func(t *testing.T, reg string) {
		t.Helper()

		ref, err := name.ParseReference(reg + chartReference)
		require.NoError(t, err)

		_, err = remote.Head(ref, remote.WithAuth(&authn.Basic{Username: "releaser", Password: "s3cret"}))
		assert.NoError(t, err)
	}

	t.Run("fails without credentials", func(t *testing.T) {
		isolateCredentialConfig(t)
		reg := newTargetRegistryWithBasicAuth(t, "releaser", "s3cret")
		chartPath, _ := packChart(t, "nexus")

		_, err := NewReleaser().Release(context.Background(), chartPath, testTarget(reg), Options{})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "credential not found")
	})

	t.Run("uses user and password of the target without persisting them", func(t *testing.T) {
		dockerConfig, helmConfig := isolateCredentialConfig(t)
		reg := newTargetRegistryWithBasicAuth(t, "releaser", "s3cret")
		chartPath, _ := packChart(t, "nexus")
		target := testTarget(reg)
		target.User = "releaser"
		target.Password = "s3cret"

		_, err := NewReleaser().Release(context.Background(), chartPath, target, Options{})

		require.NoError(t, err)
		assertChartReleased(t, reg)

		for _, dir := range []string{dockerConfig, helmConfig} {
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			assert.Empty(t, entries)
		}
	})

	t.Run("falls back to the stored docker credentials", func(t *testing.T) {
		dockerConfig, _ := isolateCredentialConfig(t)
		reg := newTargetRegistryWithBasicAuth(t, "releaser", "s3cret")
		auth := base64.StdEncoding.EncodeToString([]byte("releaser:s3cret"))
		config := `{"auths":{"` + reg + `":{"auth":"` + auth + `"}}}`
		require.NoError(t, os.WriteFile(filepath.Join(dockerConfig, "config.json"), []byte(config), 0o600))
		chartPath, _ := packChart(t, "nexus")

		_, err := NewReleaser().Release(context.Background(), chartPath, testTarget(reg), Options{})

		require.NoError(t, err)
		assertChartReleased(t, reg)
	})
}

func TestReleasePlainHTTP(t *testing.T) {
	reg := newTargetRegistry(t)
	chartPath, _ := packChart(t, "nexus")

	t.Run("fails on a registry without TLS", func(t *testing.T) {
		_, err := NewReleaser().Release(context.Background(), chartPath, Target{Registry: reg, Namespace: "testing"}, Options{})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "server gave HTTP response to HTTPS client")
	})

	t.Run("PlainHTTP allows a registry without TLS", func(t *testing.T) {
		_, err := NewReleaser().Release(context.Background(), chartPath, Target{Registry: reg, Namespace: "testing", PlainHTTP: true}, Options{})

		require.NoError(t, err)

		ref, err := name.ParseReference(reg + chartReference)
		require.NoError(t, err)

		_, err = remote.Head(ref)
		assert.NoError(t, err)
	})
}

func TestReleaseInsecureSkipTLSVerify(t *testing.T) {
	srv := httptest.NewUnstartedServer(newRegistryHandler())
	srv.Config.ErrorLog = log.New(io.Discard, "", 0) // the failing TLS handshakes are expected
	srv.StartTLS()
	t.Cleanup(srv.Close)

	reg := strings.TrimPrefix(srv.URL, "https://")
	chartPath, _ := packChart(t, "nexus")

	t.Run("fails on a registry with an untrusted certificate", func(t *testing.T) {
		_, err := NewReleaser().Release(context.Background(), chartPath, Target{Registry: reg, Namespace: "testing"}, Options{})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "certificate")
	})

	t.Run("InsecureSkipTLSVerify allows an untrusted certificate", func(t *testing.T) {
		target := Target{Registry: reg, Namespace: "testing", InsecureSkipTLSVerify: true}

		_, err := NewReleaser().Release(context.Background(), chartPath, target, Options{})

		require.NoError(t, err)

		ref, err := name.ParseReference(reg + chartReference)
		require.NoError(t, err)

		_, err = remote.Head(ref, remote.WithTransport(srv.Client().Transport))
		assert.NoError(t, err)
	})
}

// pullReleasedChart pulls the released nexus chart from the registry with the helm registry client and returns the
// archive.
func pullReleasedChart(t *testing.T, reg string) []byte {
	t.Helper()

	client, err := helmregistry.NewClient(helmregistry.ClientOptPlainHTTP())
	require.NoError(t, err)

	pulled, err := client.Pull(reg + chartReference)
	require.NoError(t, err)

	return pulled.Chart.Data
}

// newSourceImage pushes a random image with the stored docker credentials to the registry under the given repository
// and tag and returns its reference and digest.
func newSourceImage(t *testing.T, reg, repositoryAndTag string) (ref string, digest string) {
	t.Helper()

	img, err := random.Image(256, 1)
	require.NoError(t, err)

	ref = reg + "/" + repositoryAndTag
	parsed, err := name.ParseReference(ref)
	require.NoError(t, err)
	require.NoError(t, remote.Write(parsed, img, remote.WithAuthFromKeychain(authn.DefaultKeychain)))

	d, err := img.Digest()
	require.NoError(t, err)

	return ref, d.String()
}

// imageDigest returns the digest of the image or index with the given reference, or fails if it does not exist.
func imageDigest(t *testing.T, ref string, opts ...remote.Option) string {
	t.Helper()

	parsed, err := name.ParseReference(ref)
	require.NoError(t, err)

	desc, err := remote.Head(parsed, opts...)
	require.NoError(t, err)

	return desc.Digest.String()
}

func TestReleaseImages(t *testing.T) {
	t.Run("copies the image unchanged to the image repository of the target", func(t *testing.T) {
		src := newTargetRegistry(t)
		reg := newTargetRegistry(t)
		chartPath, _ := packChart(t, "nexus")
		source, digest := newSourceImage(t, src, "ci/nexus:3.96.4-3")

		_, err := NewReleaser().Release(context.Background(), chartPath, testTarget(reg), Options{Images: []string{source}})

		require.NoError(t, err)
		assert.Equal(t, digest, imageDigest(t, reg+"/testing/dogu/v3/images/nexus:3.96.4-3"))
	})

	t.Run("copies an index unchanged", func(t *testing.T) {
		src := newTargetRegistry(t)
		reg := newTargetRegistry(t)
		chartPath, _ := packChart(t, "nexus")
		idx, err := random.Index(256, 1, 2)
		require.NoError(t, err)
		source := src + "/ci/nexus:3.96.4-3"
		sourceRef, err := name.ParseReference(source)
		require.NoError(t, err)
		require.NoError(t, remote.WriteIndex(sourceRef, idx))
		digest, err := idx.Digest()
		require.NoError(t, err)

		_, err = NewReleaser().Release(context.Background(), chartPath, testTarget(reg), Options{Images: []string{source}})

		require.NoError(t, err)
		assert.Equal(t, digest.String(), imageDigest(t, reg+"/testing/dogu/v3/images/nexus:3.96.4-3"))
	})

	t.Run("names the target after the last path segment of the source and keeps the tag", func(t *testing.T) {
		src := newTargetRegistry(t)
		reg := newTargetRegistry(t)
		chartPath, _ := packChart(t, "nexus")
		source, digest := newSourceImage(t, src, "ci/staging/nexus:3.96.4-3-rc1")

		_, err := NewReleaser().Release(context.Background(), chartPath, testTarget(reg), Options{Images: []string{source}})

		require.NoError(t, err)
		assert.Equal(t, digest, imageDigest(t, reg+"/testing/dogu/v3/images/nexus:3.96.4-3-rc1"))
	})

	t.Run("releases the image with an explicit target name and tag", func(t *testing.T) {
		src := newTargetRegistry(t)
		reg := newTargetRegistry(t)
		chartPath, _ := packChart(t, "nexus")
		source, digest := newSourceImage(t, src, "local-dev/nexus/main:3.96.4-3-build.7")

		result, err := NewReleaser().Release(context.Background(), chartPath, testTarget(reg),
			Options{Images: []string{source + "=nexus:3.96.4-3"}})

		require.NoError(t, err)
		assert.Equal(t, digest, imageDigest(t, reg+"/testing/dogu/v3/images/nexus:3.96.4-3"))
		assert.Equal(t, []ReleasedImage{
			{Source: source, Reference: reg + "/testing/dogu/v3/images/nexus:3.96.4-3", Digest: digest},
		}, result.Images)

		pulled := pullReleasedChart(t, reg)
		images := chartYAML(t, pulled, mirrorPatchTemplateFile)["values"].(map[string]any)["images"]
		assert.Equal(t, reg+"/testing/dogu/v3/images/nexus:3.96.4-3", images.(map[string]any)["nexus"])
	})

	t.Run("assigns an image to the entry of values.images with the explicit key", func(t *testing.T) {
		src := newTargetRegistry(t)
		reg := newTargetRegistry(t)
		chartPath, _ := packChart(t, "nexus")
		source, _ := newSourceImage(t, src, "ci/sa-producer:0.2.0")

		_, err := NewReleaser().Release(context.Background(), chartPath, testTarget(reg), Options{
			Images:    []string{source},
			ImageKeys: []string{"sa-producer=nexusSaManager"},
		})

		require.NoError(t, err)

		pulled := pullReleasedChart(t, reg)
		images := chartYAML(t, pulled, mirrorPatchTemplateFile)["values"].(map[string]any)["images"]
		assert.Equal(t, map[string]any{
			"nexus":          "registry.cloudogu.com/official/nexus:3.96.4-3",
			"nexusSaManager": reg + "/testing/dogu/v3/images/sa-producer:0.2.0",
			"postgresql":     "docker.io/library/postgres:14.18",
		}, images)
	})

	t.Run("releases the chart rewritten to the released images", func(t *testing.T) {
		src := newTargetRegistry(t)
		reg := newTargetRegistry(t)
		chartPath, _ := packChart(t, "nexus")
		nexus, _ := newSourceImage(t, src, "ci/nexus:3.96.4-3")
		postgres, _ := newSourceImage(t, src, "ci/postgres:14.18")

		_, err := NewReleaser().Release(context.Background(), chartPath, testTarget(reg), Options{Images: []string{nexus, postgres}})

		require.NoError(t, err)

		pulled := pullReleasedChart(t, reg)
		images := chartYAML(t, pulled, mirrorPatchTemplateFile)["values"].(map[string]any)["images"]
		assert.Equal(t, map[string]any{
			"nexus":          reg + "/testing/dogu/v3/images/nexus:3.96.4-3",
			"nexusSaManager": "registry.cloudogu.com/k8s/service-account-producer-sidecar:0.1.3",
			"postgresql":     reg + "/testing/dogu/v3/images/postgres:14.18",
		}, images)

		values := chartYAML(t, pulled, valuesFile)
		assert.Equal(t, map[string]any{
			"registry":   reg,
			"repository": "testing/dogu/v3/images/nexus",
			"tag":        "3.96.4-3",
		}, values["nexus"].(map[string]any)["image"])
		assert.Equal(t, map[string]any{
			"registry":   "registry.cloudogu.com",
			"repository": "k8s/service-account-producer-sidecar",
			"tag":        "0.1.3",
		}, values["nexus"].(map[string]any)["saManager"].(map[string]any)["image"])
	})

	t.Run("returns source, reference and digest of each released image", func(t *testing.T) {
		src := newTargetRegistry(t)
		reg := newTargetRegistry(t)
		chartPath, _ := packChart(t, "nexus")
		nexus, nexusDigest := newSourceImage(t, src, "ci/nexus:3.96.4-3")
		postgres, postgresDigest := newSourceImage(t, src, "ci/postgres:14.18")

		result, err := NewReleaser().Release(context.Background(), chartPath, testTarget(reg), Options{Images: []string{nexus, postgres}})

		require.NoError(t, err)
		assert.Equal(t, []ReleasedImage{
			{Source: nexus, Reference: reg + "/testing/dogu/v3/images/nexus:3.96.4-3", Digest: nexusDigest},
			{Source: postgres, Reference: reg + "/testing/dogu/v3/images/postgres:14.18", Digest: postgresDigest},
		}, result.Images)
		assert.Equal(t, reg+chartReference, result.ChartReference)
		assert.Equal(t, imageDigest(t, reg+chartReference), result.ChartDigest)
	})

	t.Run("uses the stored docker credentials for the source and the given credentials for the target", func(t *testing.T) {
		dockerConfig, _ := isolateCredentialConfig(t)
		src := newTargetRegistryWithBasicAuth(t, "reader", "r34d")
		reg := newTargetRegistryWithBasicAuth(t, "releaser", "s3cret")
		auth := base64.StdEncoding.EncodeToString([]byte("reader:r34d"))
		config := `{"auths":{"` + src + `":{"auth":"` + auth + `"}}}`
		require.NoError(t, os.WriteFile(filepath.Join(dockerConfig, "config.json"), []byte(config), 0o600))
		chartPath, _ := packChart(t, "nexus")
		source, digest := newSourceImage(t, src, "ci/nexus:3.96.4-3")
		target := testTarget(reg)
		target.User = "releaser"
		target.Password = "s3cret"

		_, err := NewReleaser().Release(context.Background(), chartPath, target, Options{Images: []string{source}})

		require.NoError(t, err)

		targetAuth := remote.WithAuth(&authn.Basic{Username: "releaser", Password: "s3cret"})
		assert.Equal(t, digest, imageDigest(t, reg+"/testing/dogu/v3/images/nexus:3.96.4-3", targetAuth))
	})

	t.Run("does not release the chart if pushing an image fails", func(t *testing.T) {
		src := newTargetRegistry(t)
		handler := newRegistryHandler()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodHead && r.Method != http.MethodGet && strings.Contains(r.URL.Path, "/dogu/v3/images/") {
				w.WriteHeader(http.StatusForbidden)

				return
			}

			handler.ServeHTTP(w, r)
		}))
		t.Cleanup(srv.Close)
		reg := strings.TrimPrefix(srv.URL, "http://")
		chartPath, _ := packChart(t, "nexus")
		source, _ := newSourceImage(t, src, "ci/nexus:3.96.4-3")

		_, err := NewReleaser().Release(context.Background(), chartPath, testTarget(reg), Options{Images: []string{source}})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to push image "+source)
		assertNotInRegistry(t, reg+chartReference)
	})
}

// assertNotInRegistry asserts that the given reference does not exist in its registry.
func assertNotInRegistry(t *testing.T, ref string) {
	t.Helper()

	parsed, err := name.ParseReference(ref)
	require.NoError(t, err)

	_, err = remote.Head(parsed)
	assert.Error(t, err, "%s should not exist", ref)
}

// assertRegistryEmpty asserts that nothing has been pushed to the registry.
func assertRegistryEmpty(t *testing.T, reg string) {
	t.Helper()

	r, err := name.NewRegistry(reg)
	require.NoError(t, err)

	repos, err := remote.Catalog(context.Background(), r)
	require.NoError(t, err)
	assert.Empty(t, repos)
}

func TestReleaseImagesInsecureSkipTLSVerifyOnlyForTarget(t *testing.T) {
	srv := httptest.NewUnstartedServer(newRegistryHandler())
	srv.Config.ErrorLog = log.New(io.Discard, "", 0) // the failing TLS handshakes are expected
	srv.StartTLS()
	t.Cleanup(srv.Close)

	src := strings.TrimPrefix(srv.URL, "https://")
	img, err := random.Image(256, 1)
	require.NoError(t, err)
	source := src + "/ci/nexus:3.96.4-3"
	sourceRef, err := name.ParseReference(source)
	require.NoError(t, err)
	require.NoError(t, remote.Write(sourceRef, img, remote.WithTransport(srv.Client().Transport)))

	reg := newTargetRegistry(t)
	chartPath, _ := packChart(t, "nexus")
	target := testTarget(reg)
	target.InsecureSkipTLSVerify = true

	_, err = NewReleaser().Release(context.Background(), chartPath, target, Options{Images: []string{source}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to resolve image "+source)
	assert.Contains(t, err.Error(), "certificate")
	assertRegistryEmpty(t, reg)
}

func TestReleaseImagesAmbiguousEntry(t *testing.T) {
	src := newTargetRegistry(t)
	nexus, _ := newSourceImage(t, src, "ci/nexus:3.96.4-3")
	archive := packChartWith(t, "nexus", withMirrorPatchTemplate(`apiVersion: v1
values:
  images:
    nexus: registry.cloudogu.com/official/nexus:3.96.4-3
    nexusOld: registry.cloudogu.com/official/nexus:3.70.0-1
patches:
  values.yaml:
    nexus:
      image: "{{ .images.nexus }}"
`))
	chartPath := filepath.Join(t.TempDir(), "nexus-3.96.4-3.tgz")
	require.NoError(t, os.WriteFile(chartPath, archive, 0o600))

	t.Run("fails before pushing with a hint to --image-key", func(t *testing.T) {
		reg := newTargetRegistry(t)

		_, err := NewReleaser().Release(context.Background(), chartPath, testTarget(reg), Options{Images: []string{nexus}})

		require.Error(t, err)
		assert.Contains(t, err.Error(), `several entries in values.images of mirror-patch-tpl.yaml have the repository name "nexus"`)
		assert.Contains(t, err.Error(), "choose one with --image-key nexus=<key>")
		assertRegistryEmpty(t, reg)
	})

	t.Run("releases with an explicit key", func(t *testing.T) {
		reg := newTargetRegistry(t)

		_, err := NewReleaser().Release(context.Background(), chartPath, testTarget(reg),
			Options{Images: []string{nexus}, ImageKeys: []string{"nexus=nexus"}})

		require.NoError(t, err)

		images := chartYAML(t, pullReleasedChart(t, reg), mirrorPatchTemplateFile)["values"].(map[string]any)["images"]
		assert.Equal(t, map[string]any{
			"nexus":    reg + "/testing/dogu/v3/images/nexus:3.96.4-3",
			"nexusOld": "registry.cloudogu.com/official/nexus:3.70.0-1",
		}, images)
	})
}

func TestReleaseImagesFailsBeforePushing(t *testing.T) {
	src := newTargetRegistry(t)
	nexus, _ := newSourceImage(t, src, "ci/nexus:3.96.4-3")
	usermgt, _ := newSourceImage(t, src, "ci/usermgt:1.20.0-1")
	otherNexus, _ := newSourceImage(t, src, "other/nexus:3.96.4-3")
	redmine, _ := newSourceImage(t, src, "ci/redmine:6.1.2-dev.1")
	nexusDigest := imageDigest(t, nexus)

	tests := []struct {
		name      string
		chart     string
		images    []string
		imageKeys []string
		wantErr   string
	}{
		{
			name:    "if the chart has no mirror patch template",
			chart:   "redmine-prerelease",
			images:  []string{redmine},
			wantErr: "chart has no mirror-patch-tpl.yaml",
		},
		{
			name:    "if no entry of values.images matches an image",
			chart:   "nexus",
			images:  []string{nexus, usermgt},
			wantErr: `no entry in values.images of mirror-patch-tpl.yaml has the repository name "usermgt"`,
		},
		{
			name:    "if an image does not exist",
			chart:   "nexus",
			images:  []string{nexus, src + "/ci/missing:1.0.0"},
			wantErr: "failed to resolve image " + src + "/ci/missing:1.0.0",
		},
		{
			name:    "if an image has no tag",
			chart:   "nexus",
			images:  []string{src + "/ci/nexus@" + nexusDigest},
			wantErr: "has no tag",
		},
		{
			name:    "if an image reference is invalid",
			chart:   "nexus",
			images:  []string{"Not A Reference"},
			wantErr: "invalid image reference Not A Reference",
		},
		{
			name:    "if two images have the same target name",
			chart:   "nexus",
			images:  []string{nexus, otherNexus},
			wantErr: "images " + nexus + " and " + otherNexus + " would both be released as nexus",
		},
		{
			name:    "if two images have the same explicit target name",
			chart:   "nexus",
			images:  []string{nexus, usermgt + "=nexus:1.0.0"},
			wantErr: "images " + nexus + " and " + usermgt + " would both be released as nexus",
		},
		{
			name:    "if an explicit target has no tag",
			chart:   "nexus",
			images:  []string{nexus + "=nexus"},
			wantErr: "invalid image " + nexus + "=nexus: the target must be given as <name>:<tag>",
		},
		{
			name:    "if an explicit target has an empty name",
			chart:   "nexus",
			images:  []string{nexus + "=:3.96.4-3"},
			wantErr: "the target must be given as <name>:<tag>",
		},
		{
			name:    "if an explicit target has an empty tag",
			chart:   "nexus",
			images:  []string{nexus + "=nexus:"},
			wantErr: "the target must be given as <name>:<tag>",
		},
		{
			name:    "if an explicit target name has a path",
			chart:   "nexus",
			images:  []string{nexus + "=official/nexus:3.96.4-3"},
			wantErr: "the target must be given as <name>:<tag>",
		},
		{
			name:    "if an explicit target is invalid",
			chart:   "nexus",
			images:  []string{nexus + "=Nexus:3.96.4-3"},
			wantErr: "invalid target Nexus:3.96.4-3 for image " + nexus,
		},
		{
			name:      "if an image key is given without images",
			chart:     "nexus",
			imageKeys: []string{"nexus=nexus"},
			wantErr:   "image keys require images to release (--image-key needs -i)",
		},
		{
			name:      "if an image key has no key",
			chart:     "nexus",
			images:    []string{nexus},
			imageKeys: []string{"nexus"},
			wantErr:   `invalid image key "nexus": must be given as <name>=<key>, e.g. nexus=nexus`,
		},
		{
			name:      "if an image key has an empty name",
			chart:     "nexus",
			images:    []string{nexus},
			imageKeys: []string{"=nexus"},
			wantErr:   `invalid image key "=nexus"`,
		},
		{
			name:      "if an image key has an empty key",
			chart:     "nexus",
			images:    []string{nexus},
			imageKeys: []string{"nexus="},
			wantErr:   `invalid image key "nexus="`,
		},
		{
			name:      "if an image key is given twice for the same image",
			chart:     "nexus",
			images:    []string{nexus},
			imageKeys: []string{"nexus=nexus", "nexus=nexusSaManager"},
			wantErr:   `several image keys for image nexus: nexus, nexusSaManager`,
		},
		{
			name:      "if an image key refers to an unknown image",
			chart:     "nexus",
			images:    []string{nexus},
			imageKeys: []string{"usermgt=nexus"},
			wantErr:   `image key usermgt=nexus refers to no released image (released images: nexus)`,
		},
		{
			name:      "if an image key refers to an unknown entry of values.images",
			chart:     "nexus",
			images:    []string{nexus},
			imageKeys: []string{"nexus=unknown"},
			wantErr:   `values.images of mirror-patch-tpl.yaml has no entry "unknown" for image`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := newTargetRegistry(t)
			chartPath, _ := packChart(t, tt.chart)

			_, err := NewReleaser().Release(context.Background(), chartPath, testTarget(reg), Options{Images: tt.images, ImageKeys: tt.imageKeys})

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assertRegistryEmpty(t, reg)
		})
	}
}

// pushImage prepares an existing tag in a registry: it writes a new random image directly to the given reference,
// bypassing the releaser, and returns its digest. The digest differs from that of every source image.
func pushImage(t *testing.T, ref string) string {
	t.Helper()

	img, err := random.Image(256, 1)
	require.NoError(t, err)

	parsed, err := name.ParseReference(ref)
	require.NoError(t, err)
	require.NoError(t, remote.Write(parsed, img))

	d, err := img.Digest()
	require.NoError(t, err)

	return d.String()
}

func TestReleaseExistingImages(t *testing.T) {
	t.Run("skips an image whose tag already exists with the same digest", func(t *testing.T) {
		src := newTargetRegistry(t)
		reg := newTargetRegistry(t)
		chartPath, _ := packChart(t, "nexus")
		nexus, nexusDigest := newSourceImage(t, src, "ci/nexus:3.96.4-3")
		postgres, postgresDigest := newSourceImage(t, src, "ci/postgres:14.18")
		copyImage(t, nexus, reg+"/testing/dogu/v3/images/nexus:3.96.4-3")

		result, err := NewReleaser().Release(context.Background(), chartPath, testTarget(reg), Options{Images: []string{nexus, postgres}})

		require.NoError(t, err)
		assert.Equal(t, []ReleasedImage{
			{Source: nexus, Reference: reg + "/testing/dogu/v3/images/nexus:3.96.4-3", Digest: nexusDigest, Skipped: true},
			{Source: postgres, Reference: reg + "/testing/dogu/v3/images/postgres:14.18", Digest: postgresDigest},
		}, result.Images)
		assert.Equal(t, postgresDigest, imageDigest(t, reg+"/testing/dogu/v3/images/postgres:14.18"))
		assert.Equal(t, imageDigest(t, reg+chartReference), result.ChartDigest)
	})

	t.Run("fails before pushing if an image tag already exists with another digest", func(t *testing.T) {
		src := newTargetRegistry(t)
		reg := newTargetRegistry(t)
		chartPath, _ := packChart(t, "nexus")
		nexus, _ := newSourceImage(t, src, "ci/nexus:3.96.4-3")
		postgres, postgresDigest := newSourceImage(t, src, "ci/postgres:14.18")
		otherDigest := pushImage(t, reg+"/testing/dogu/v3/images/postgres:14.18")

		_, err := NewReleaser().Release(context.Background(), chartPath, testTarget(reg), Options{Images: []string{nexus, postgres}})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "image "+reg+"/testing/dogu/v3/images/postgres:14.18 already exists with digest "+
			otherDigest+" instead of "+postgresDigest)
		assert.Contains(t, err.Error(), "--force")
		assertNotInRegistry(t, reg+"/testing/dogu/v3/images/nexus:3.96.4-3")
		assertNotInRegistry(t, reg+chartReference)
		assert.Equal(t, otherDigest, imageDigest(t, reg+"/testing/dogu/v3/images/postgres:14.18"))
	})

	t.Run("Force overwrites an image tag with another digest", func(t *testing.T) {
		src := newTargetRegistry(t)
		reg := newTargetRegistry(t)
		chartPath, _ := packChart(t, "nexus")
		nexus, nexusDigest := newSourceImage(t, src, "ci/nexus:3.96.4-3")
		pushImage(t, reg+"/testing/dogu/v3/images/nexus:3.96.4-3")

		result, err := NewReleaser().Release(context.Background(), chartPath, testTarget(reg),
			Options{Images: []string{nexus}, Force: true})

		require.NoError(t, err)
		assert.Equal(t, nexusDigest, imageDigest(t, reg+"/testing/dogu/v3/images/nexus:3.96.4-3"))
		assert.Equal(t, []ReleasedImage{
			{Source: nexus, Reference: reg + "/testing/dogu/v3/images/nexus:3.96.4-3", Digest: nexusDigest},
		}, result.Images)
	})

	t.Run("a retry after a failed image push succeeds without Force", func(t *testing.T) {
		src := newTargetRegistry(t)
		handler := newRegistryHandler()
		var failPostgres atomic.Bool
		failPostgres.Store(true)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if failPostgres.Load() && r.Method != http.MethodHead && r.Method != http.MethodGet &&
				strings.Contains(r.URL.Path, "/dogu/v3/images/postgres/") {
				w.WriteHeader(http.StatusInternalServerError)

				return
			}

			handler.ServeHTTP(w, r)
		}))
		t.Cleanup(srv.Close)
		reg := strings.TrimPrefix(srv.URL, "http://")
		chartPath, _ := packChart(t, "nexus")
		nexus, nexusDigest := newSourceImage(t, src, "ci/nexus:3.96.4-3")
		postgres, postgresDigest := newSourceImage(t, src, "ci/postgres:14.18")
		opts := Options{Images: []string{nexus, postgres}}

		_, err := NewReleaser().Release(context.Background(), chartPath, testTarget(reg), opts)
		require.Error(t, err)
		assert.Equal(t, nexusDigest, imageDigest(t, reg+"/testing/dogu/v3/images/nexus:3.96.4-3"))
		assertNotInRegistry(t, reg+chartReference)

		failPostgres.Store(false)
		result, err := NewReleaser().Release(context.Background(), chartPath, testTarget(reg), opts)

		require.NoError(t, err)
		assert.True(t, result.Images[0].Skipped)
		assert.False(t, result.Images[1].Skipped)
		assert.Equal(t, postgresDigest, imageDigest(t, reg+"/testing/dogu/v3/images/postgres:14.18"))
		assert.Equal(t, imageDigest(t, reg+chartReference), result.ChartDigest)
	})
}

func TestReleaseExistingChart(t *testing.T) {
	t.Run("fails if the chart version is already released", func(t *testing.T) {
		reg := newTargetRegistry(t)
		chartPath, _ := packChart(t, "nexus")
		_, err := NewReleaser().Release(context.Background(), chartPath, testTarget(reg), Options{})
		require.NoError(t, err)

		_, err = NewReleaser().Release(context.Background(), chartPath, testTarget(reg), Options{})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "chart "+reg+chartReference+" is already released")
		assert.Contains(t, err.Error(), "--force")
	})

	t.Run("fails before pushing any image if the chart version is already released", func(t *testing.T) {
		src := newTargetRegistry(t)
		reg := newTargetRegistry(t)
		chartPath, _ := packChart(t, "nexus")
		nexus, _ := newSourceImage(t, src, "ci/nexus:3.96.4-3")
		_, err := NewReleaser().Release(context.Background(), chartPath, testTarget(reg), Options{})
		require.NoError(t, err)

		_, err = NewReleaser().Release(context.Background(), chartPath, testTarget(reg), Options{Images: []string{nexus}})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "is already released")
		assertNotInRegistry(t, reg+"/testing/dogu/v3/images/nexus:3.96.4-3")
	})

	t.Run("Force overwrites the released chart version", func(t *testing.T) {
		src := newTargetRegistry(t)
		reg := newTargetRegistry(t)
		chartPath, _ := packChart(t, "nexus")
		nexus, _ := newSourceImage(t, src, "ci/nexus:3.96.4-3")
		_, err := NewReleaser().Release(context.Background(), chartPath, testTarget(reg), Options{})
		require.NoError(t, err)

		result, err := NewReleaser().Release(context.Background(), chartPath, testTarget(reg),
			Options{Images: []string{nexus}, Force: true})

		require.NoError(t, err)
		assert.Equal(t, imageDigest(t, reg+chartReference), result.ChartDigest)

		images := chartYAML(t, pullReleasedChart(t, reg), mirrorPatchTemplateFile)["values"].(map[string]any)["images"]
		assert.Equal(t, reg+"/testing/dogu/v3/images/nexus:3.96.4-3", images.(map[string]any)["nexus"])
	})
}

// copyImage copies the image with the source reference unchanged to the target reference.
func copyImage(t *testing.T, source, target string) {
	t.Helper()

	sourceRef, err := name.ParseReference(source)
	require.NoError(t, err)

	img, err := remote.Image(sourceRef)
	require.NoError(t, err)

	targetRef, err := name.ParseReference(target)
	require.NoError(t, err)
	require.NoError(t, remote.Write(targetRef, img))
}
