package oci

import (
	"context"
	"io"
	"log"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	helmchart "helm.sh/helm/v4/pkg/chart/v2"
	chartutil "helm.sh/helm/v4/pkg/chart/v2/util"
)

// newTestRegistry starts an in-memory OCI registry without TLS and returns a target for its namespace "testing".
func newTestRegistry(t *testing.T) Target {
	t.Helper()

	srv := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	t.Cleanup(srv.Close)

	return Target{Registry: strings.TrimPrefix(srv.URL, "http://"), Namespace: "testing", PlainHTTP: true}
}

func TestClientImages(t *testing.T) {
	isolateCredentialConfig(t)

	target := newTestRegistry(t)
	client, err := NewClient(target)
	require.NoError(t, err)

	ref, err := name.NewTag(target.ImageRepository("nexus")+":3.96.4", target.NameOptions()...)
	require.NoError(t, err)

	_, exists, err := client.ImageExists(context.Background(), ref)
	require.NoError(t, err)
	assert.False(t, exists, "image should not exist before the push")

	img, err := random.Image(64, 1)
	require.NoError(t, err)
	digest, err := img.Digest()
	require.NoError(t, err)

	require.NoError(t, client.PushImage(context.Background(), ref, img))

	existingDigest, exists, err := client.ImageExists(context.Background(), ref)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, digest.String(), existingDigest)
}

func TestClientCharts(t *testing.T) {
	isolateCredentialConfig(t)

	target := newTestRegistry(t)
	client, err := NewClient(target)
	require.NoError(t, err)

	ref := target.ChartRepository("nexus") + ":1.2.3"

	_, exists, err := client.ChartExists(context.Background(), ref)
	require.NoError(t, err)
	assert.False(t, exists, "chart should not exist before the push")

	c := &helmchart.Chart{Metadata: &helmchart.Metadata{APIVersion: helmchart.APIVersionV2, Name: "nexus", Version: "1.2.3"}}
	archivePath, err := chartutil.Save(c, t.TempDir())
	require.NoError(t, err)
	archive, err := os.ReadFile(archivePath)
	require.NoError(t, err)

	digest, err := client.PushChart(context.Background(), archive, ref)
	require.NoError(t, err)

	existingDigest, exists, err := client.ChartExists(context.Background(), ref)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, digest, existingDigest)
}
