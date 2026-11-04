// Package dogu applies the dogu resources (custom resources of the dogu operator) of a dogu v3.
package dogu

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// GVR is the resource of dogus v3.
var GVR = schema.GroupVersionResource{Group: "k8s.cloudogu.com", Version: "v3beta1", Resource: "dogus"}

// Annotations of a dogu that make the dogu operator install a dev chart instead of the chart of the DCC.
const (
	// AnnotationDevChartURL is the OCI URL of the dev chart without tag; the tag is the version of the dogu.
	AnnotationDevChartURL = "k8s.cloudogu.com/dev-chart-url"
	// AnnotationDevChartInsecure is "true" if the dev chart is pulled via plain HTTP or without certificate verification.
	AnnotationDevChartInsecure = "k8s.cloudogu.com/dev-chart-insecure"
)

// FieldManager owns the fields cldk applies.
const FieldManager = "cldk"

// doguAPIVersionV3 is the value of spec.doguApiVersion of a dogu v3.
const doguAPIVersionV3 = "v3"

// DevChart is the dev chart a dogu v3 is installed from.
type DevChart struct {
	// Name is the name of the chart and the simple name of the dogu, e.g. "nexus".
	Name string
	// Namespace is the dogu namespace, e.g. "official".
	Namespace string
	// Version is the dev version, e.g. "3.96.4-3-dev.20261103143012".
	Version string
	// URL is the OCI URL of the dev chart without tag as seen from the cluster.
	URL string
	// Insecure is true if the dev chart is pulled via plain HTTP or without certificate verification.
	Insecure bool
}

// Object returns the dogu resource of a dogu installed from the dev chart with exactly the fields cldk applies.
func Object(chart DevChart) *unstructured.Unstructured {
	annotations := map[string]any{AnnotationDevChartURL: chart.URL}

	if chart.Insecure {
		annotations[AnnotationDevChartInsecure] = "true"
	}

	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": GVR.GroupVersion().String(),
		"kind":       "Dogu",
		"metadata": map[string]any{
			"name":        chart.Name,
			"annotations": annotations,
		},
		"spec": map[string]any{
			"name":           chart.Name,
			"doguNamespace":  chart.Namespace,
			"version":        chart.Version,
			"doguApiVersion": doguAPIVersionV3,
		},
	}}
}

// Client reads and applies dogus in one Kubernetes namespace.
type Client struct {
	dynamic   dynamic.Interface
	namespace string
}

// NewClient creates a client for the dogus in the Kubernetes namespace.
func NewClient(dynamicClient dynamic.Interface, namespace string) *Client {
	return &Client{dynamic: dynamicClient, namespace: namespace}
}

// Apply creates or updates the dogu installed from the dev chart per server-side apply. The fields cldk applied before
// and no longer applies are removed; the fields of other managers are kept.
func (c *Client) Apply(ctx context.Context, chart DevChart) error {
	_, err := c.dynamic.Resource(GVR).Namespace(c.namespace).Apply(ctx, chart.Name, Object(chart),
		metav1.ApplyOptions{FieldManager: FieldManager, Force: true})
	if err != nil {
		return fmt.Errorf("failed to apply dogu %s in namespace %s: %w", chart.Name, c.namespace, err)
	}

	return nil
}
