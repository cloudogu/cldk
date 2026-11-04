package devpush

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cloudogu/cldk/internal/oci"
)

func TestPush(t *testing.T) {
	t.Run("pushes to the namespace of the options instead of cldk.yaml", func(t *testing.T) {
		pusher := newMockedPusher()
		dir := writeProject(t, "namespace: official\n", nil)

		result, err := pusher.Push(context.Background(), Options{Dir: dir, Registry: "localhost:5001", Namespace: "personal"})

		require.NoError(t, err)
		assert.Equal(t, "localhost:5001/personal/dogu/v3/charts/nexus:"+wantDevVersion, result.ChartReference)
		assert.Equal(t, []string{result.ChartReference}, pusher.pushedRefs())
	})

	t.Run("pushes with plain HTTP and without certificate verification as given", func(t *testing.T) {
		pusher := newMockedPusher()
		dir := writeProject(t, "namespace: official\n", nil)

		_, err := pusher.Push(context.Background(), Options{
			Dir: dir, Registry: "localhost:5001", ClusterRegistry: "k3d-registry.localhost:5000", PlainHTTP: true, Insecure: true,
		})

		require.NoError(t, err)
		assert.Equal(t, []oci.Target{{Registry: "localhost:5001", Namespace: "official", PlainHTTP: true, InsecureSkipTLSVerify: true}},
			pusher.targets)
	})

	t.Run("returns the error of the chart push", func(t *testing.T) {
		pusher := newTestPusher()
		pusher.newChartPusher = func(oci.Target) (ChartPusher, error) {
			return nil, errors.New("no registry client")
		}
		dir := writeProject(t, "namespace: official\n", nil)

		_, err := pusher.Push(context.Background(), Options{Dir: dir, Registry: "localhost:5001"})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no registry client")
	})

	for _, tt := range []abortCase{
		{name: "a missing project file", wantErr: "cldk.yaml not found"},
	} {
		t.Run("aborts without pushing on "+tt.name, func(t *testing.T) {
			assertAbortsWithoutPushing(t, tt)
		})
	}
}
