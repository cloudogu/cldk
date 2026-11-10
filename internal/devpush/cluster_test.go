package devpush

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudogu/cldk/internal/dogu"
	"github.com/cloudogu/cldk/internal/kube"
)

func TestPush_cluster(t *testing.T) {
	t.Run("applies the dogu with the dev chart after pushing the chart", func(t *testing.T) {
		pusher := newMockedPusher()
		dir := writeProject(t, "namespace: official\n", nil)
		kubeOpts := kube.Options{Context: "k3d-dev1", Namespace: "ecosystem"}

		result, err := pusher.Push(context.Background(), Options{
			Dir: dir, Registry: "localhost:5001", ClusterRegistry: "k3d-registry-proxy.localhost:5000", Kube: kubeOpts,
		})

		require.NoError(t, err)
		assert.Equal(t, []kube.Options{kubeOpts}, pusher.kubeOptions)
		assert.Equal(t, []string{"push " + result.ChartReference, "apply nexus"}, pusher.steps)
		assert.Equal(t, []dogu.DevChart{{
			Name:      "nexus",
			Namespace: "official",
			Version:   wantDevVersion,
			URL:       "oci://k3d-registry-proxy.localhost:5000/official/dogu/v3/charts/nexus",
		}}, pusher.appliedDevCharts())
		assert.Equal(t, "nexus", result.Dogu)
		assert.Equal(t, testCluster, result.Cluster)
	})

	t.Run("applies the dev chart in the namespace of the options", func(t *testing.T) {
		pusher := newMockedPusher()
		dir := writeProject(t, "namespace: official\n", nil)

		_, err := pusher.Push(context.Background(), Options{
			Dir: dir, Registry: "localhost:5001", ClusterRegistry: "localhost:5001", Namespace: "personal",
		})

		require.NoError(t, err)
		require.Len(t, pusher.appliedDevCharts(), 1)
		assert.Equal(t, "personal", pusher.appliedDevCharts()[0].Namespace)
		assert.Equal(t, "oci://localhost:5001/personal/dogu/v3/charts/nexus", pusher.appliedDevCharts()[0].URL)
	})

	for _, opts := range []Options{{PlainHTTP: true}, {Insecure: true}} {
		t.Run(fmt.Sprintf("marks the dev chart as insecure with %+v", opts), func(t *testing.T) {
			pusher := newMockedPusher()
			opts.Dir = writeProject(t, "namespace: official\n", nil)
			opts.Registry = "localhost:5001"

			_, err := pusher.Push(context.Background(), opts)

			require.NoError(t, err)
			require.Len(t, pusher.appliedDevCharts(), 1)
			assert.True(t, pusher.appliedDevCharts()[0].Insecure)
		})
	}

	t.Run("aborts before linting and pushing on an invalid kubeconfig", func(t *testing.T) {
		pusher := newMockedPusher()
		pusher.newDoguClient = func(kube.Options) (DoguClient, kube.Info, error) {
			return nil, kube.Info{}, errors.New(`context "dev3" not found`)
		}
		// The chart has lint errors, connecting to the cluster must abort first.
		dir := writeProject(t, "namespace: official\n", map[string]string{
			"values.schema.json": `{"type": "object", "required": ["missingValue"]}`,
		})

		_, err := pusher.Push(context.Background(), Options{Dir: dir, Registry: "localhost:5001"})

		require.ErrorContains(t, err, `failed to connect to the cluster: context "dev3" not found`)
		assert.Empty(t, pusher.chartPusher.PushChartCalls())
		assert.Empty(t, pusher.appliedDevCharts())
	})

	t.Run("returns the error of the apply after pushing the chart", func(t *testing.T) {
		pusher := newMockedPusher()
		pusher.doguClient.ApplyFunc = func(context.Context, dogu.DevChart) error {
			return errors.New("admission webhook denied")
		}
		dir := writeProject(t, "namespace: official\n", nil)

		_, err := pusher.Push(context.Background(), Options{Dir: dir, Registry: "localhost:5001"})

		require.ErrorContains(t, err, "pushed chart localhost:5001/official/dogu/v3/charts/nexus:"+wantDevVersion+", but admission webhook denied")
		assert.Len(t, pusher.chartPusher.PushChartCalls(), 1)
	})
}
