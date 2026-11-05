package devpush

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudogu/cldk/internal/dogu"
	"github.com/cloudogu/cldk/internal/kube"
)

func TestPlan(t *testing.T) {
	const (
		reg         = "localhost:5001"
		clusterReg  = "k3d-registry-proxy.localhost:5000"
		replacedRef = "registry.example.com/foo/sidecar:1.0"
	)

	t.Run("plans the dev push without building, pushing or applying anything", func(t *testing.T) {
		pusher := newMockedPusher()
		dir := writeProject(t, twoImagesYAML, nil)
		before := snapshotDir(t, dir)
		kubeOpts := kube.Options{Context: "k3d-dev1", Namespace: "ecosystem"}

		plan, err := pusher.Plan(context.Background(), Options{
			Dir: dir, Registry: reg, ClusterRegistry: clusterReg, PlainHTTP: true, Kube: kubeOpts, Builder: "podman",
		})

		require.NoError(t, err)
		assert.Equal(t, Plan{
			Version: wantDevVersion,
			Images: []PushedImage{
				{Name: "nexus", Reference: reg + "/official/dogu/v3/images/nexus:" + wantDevVersion},
				{Name: "sidecar", Reference: reg + "/official/dogu/v3/images/sidecar:" + wantDevVersion},
			},
			ChartReference: reg + "/official/dogu/v3/charts/nexus:" + wantDevVersion,
			DevChart: dogu.DevChart{
				Name:      "nexus",
				Namespace: "official",
				Version:   wantDevVersion,
				URL:       "oci://" + clusterReg + "/official/dogu/v3/charts/nexus",
				Insecure:  true,
			},
			Cluster: testCluster,
		}, plan)

		assert.Empty(t, pusher.steps)
		assert.Empty(t, pusher.builder.BuildCalls())
		assert.Empty(t, pusher.builder.PushCalls())
		assert.Empty(t, pusher.chartPusher.PushChartCalls())
		assert.Empty(t, pusher.doguClient.ApplyCalls())
		assert.Len(t, pusher.builder.CheckCalls(), 1, "the builder should be checked")
		assert.Equal(t, []string{"podman"}, pusher.builderBinaries)
		assert.Equal(t, []kube.Options{kubeOpts}, pusher.kubeOptions, "the cluster should be connected")
		assert.Equal(t, before, snapshotDir(t, dir), "the project directory should be unchanged")
	})

	t.Run("plans replaced and skipped images", func(t *testing.T) {
		pusher := newMockedPusher()
		dir := writeProject(t, twoImagesYAML, nil)

		plan, err := pusher.Plan(context.Background(), Options{
			Dir: dir, Registry: reg,
			ReplaceImages: map[string]string{"sidecar": replacedRef}, SkipImages: []string{"nexus"},
		})

		require.NoError(t, err)
		assert.Empty(t, plan.Images)
		assert.Equal(t, []ReplacedImage{{Name: "sidecar", Reference: replacedRef}}, plan.ReplacedImages)
		assert.Equal(t, []string{"nexus"}, plan.SkippedImages)
		assert.Empty(t, pusher.builder.CheckCalls(), "without images to build, the builder should not be checked")
		assert.Empty(t, pusher.steps)
	})

	t.Run("fails on a missing builder", func(t *testing.T) {
		pusher := newMockedPusher()
		pusher.builder.CheckFunc = func(context.Context) error {
			return errors.New(`build CLI "podman" not found`)
		}
		dir := writeProject(t, twoImagesYAML, nil)

		_, err := pusher.Plan(context.Background(), Options{Dir: dir, Registry: reg})

		require.ErrorContains(t, err, `build CLI "podman" not found`)
		assert.Empty(t, pusher.kubeOptions)
	})

	t.Run("fails on an invalid kubeconfig", func(t *testing.T) {
		pusher := newMockedPusher()
		pusher.newDoguClient = func(kube.Options) (DoguClient, kube.Info, error) {
			return nil, kube.Info{}, errors.New(`context "dev3" not found`)
		}
		dir := writeProject(t, "namespace: official\n", nil)

		_, err := pusher.Plan(context.Background(), Options{Dir: dir, Registry: reg})

		require.ErrorContains(t, err, `failed to connect to the cluster: context "dev3" not found`)
	})

	for _, tt := range []abortCase{
		{name: "a missing project file", wantErr: "cldk.yaml not found"},
		{
			name:       "lint errors",
			cldkYAML:   "namespace: official\nimages:\n  nexus: {}\n",
			chartFiles: map[string]string{"values.schema.json": `{"type": "object", "required": ["missingValue"]}`},
			wantErr:    "missingValue",
		},
	} {
		t.Run("fails on "+tt.name, func(t *testing.T) {
			pusher := newMockedPusher()
			dir := writeProject(t, tt.cldkYAML, tt.chartFiles)

			if tt.cldkYAML == "" {
				require.NoError(t, os.Remove(filepath.Join(dir, "cldk.yaml")))
			}

			_, err := pusher.Plan(context.Background(), Options{Dir: dir, Registry: reg})

			require.ErrorContains(t, err, tt.wantErr)
			assert.Empty(t, pusher.steps)
		})
	}
}
