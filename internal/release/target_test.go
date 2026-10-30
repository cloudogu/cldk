package release

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTargetChartRepository(t *testing.T) {
	tests := []struct {
		name      string
		target    Target
		chartName string
		want      string
	}{
		{"official namespace", Target{Registry: "registry.cloudogu.com", Namespace: "official"}, "nexus", "registry.cloudogu.com/official/dogu/v3/charts/nexus"},
		{"other registry", Target{Registry: "staging-registry.cloudogu.com", Namespace: "testing"}, "redmine", "staging-registry.cloudogu.com/testing/dogu/v3/charts/redmine"},
		{"registry with port", Target{Registry: "localhost:5000", Namespace: "partner"}, "usermgt", "localhost:5000/partner/dogu/v3/charts/usermgt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.target.chartRepository(tt.chartName))
		})
	}
}
