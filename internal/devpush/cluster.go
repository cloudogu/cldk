package devpush

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/cloudogu/cldk/internal/dogu"
	"github.com/cloudogu/cldk/internal/kube"
)

// connectCluster connects to the cluster and logs it, so that the dev push can be aborted before anything is pushed.
func (p *Pusher) connectCluster(opts kube.Options) (DoguClient, kube.Info, error) {
	doguClient, cluster, err := p.newDoguClient(opts)
	if err != nil {
		return nil, kube.Info{}, fmt.Errorf("failed to connect to the cluster: %w", err)
	}

	slog.Info("target cluster", "context", cluster.Context, "server", cluster.Server, "namespace", cluster.Namespace)

	return doguClient, cluster, nil
}

// applyDogu applies the dogu with the dev chart in the cluster.
func applyDogu(ctx context.Context, doguClient DoguClient, devChart dogu.DevChart) error {
	slog.Info("applying dogu", "name", devChart.Name, "version", devChart.Version, "chart", devChart.URL)

	return doguClient.Apply(ctx, devChart)
}
