package dogu

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/managedfields"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

const testNamespace = "ecosystem"

// devChartNexus is a dev chart of nexus as dev-push applies it.
var devChartNexus = DevChart{
	Name:      "nexus",
	Namespace: "official",
	Version:   "3.96.4-3-dev.20261103143012",
	URL:       "oci://k3d-registry-proxy.localhost:5000/official/dogu/v3/charts/nexus",
}

// newFakeDynamic returns a fake dynamic client that knows dogus and tracks managed fields like server-side apply.
func newFakeDynamic(t *testing.T) *fakedynamic.FakeDynamicClient {
	t.Helper()

	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(GVR.GroupVersion().WithKind("Dogu"), &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(GVR.GroupVersion().WithKind("DoguList"), &unstructured.UnstructuredList{})

	tracker := clienttesting.NewFieldManagedObjectTracker(scheme, serializer.NewCodecFactory(scheme).UniversalDecoder(),
		managedfields.NewDeducedTypeConverter())

	client := fakedynamic.NewSimpleDynamicClient(scheme)
	client.PrependReactor("*", "*", clienttesting.ObjectReaction(tracker))

	return client
}

// newTestClient returns a client for the namespace ecosystem of a fake cluster.
func newTestClient(t *testing.T) (*Client, *fakedynamic.FakeDynamicClient) {
	t.Helper()

	dynamicClient := newFakeDynamic(t)

	return NewClient(dynamicClient, testNamespace), dynamicClient
}

// applyAs applies the dogu object with another field manager, e.g. a user with kubectl.
func applyAs(t *testing.T, client *fakedynamic.FakeDynamicClient, manager string, obj map[string]any) {
	t.Helper()

	u := &unstructured.Unstructured{Object: obj}
	u.SetAPIVersion(GVR.GroupVersion().String())
	u.SetKind("Dogu")

	_, err := client.Resource(GVR).Namespace(testNamespace).Apply(context.Background(), u.GetName(), u,
		metav1.ApplyOptions{FieldManager: manager, Force: true})
	require.NoError(t, err)
}

// getDogu returns the dogu from the fake cluster.
func getDogu(t *testing.T, client *fakedynamic.FakeDynamicClient, name string) *unstructured.Unstructured {
	t.Helper()

	obj, err := client.Resource(GVR).Namespace(testNamespace).Get(context.Background(), name, metav1.GetOptions{})
	require.NoError(t, err)

	return obj
}

func TestObject(t *testing.T) {
	t.Run("contains exactly the fields of dev-push", func(t *testing.T) {
		obj := Object(devChartNexus)

		assert.Equal(t, map[string]any{
			"apiVersion": "k8s.cloudogu.com/v3beta1",
			"kind":       "Dogu",
			"metadata": map[string]any{
				"name": "nexus",
				"annotations": map[string]any{
					"k8s.cloudogu.com/dev-chart-url": "oci://k3d-registry-proxy.localhost:5000/official/dogu/v3/charts/nexus",
				},
			},
			"spec": map[string]any{
				"name":           "nexus",
				"doguNamespace":  "official",
				"version":        "3.96.4-3-dev.20261103143012",
				"doguApiVersion": "v3",
			},
		}, obj.Object)
	})

	t.Run("marks an insecure dev chart", func(t *testing.T) {
		chart := devChartNexus
		chart.Insecure = true

		obj := Object(chart)

		assert.Equal(t, "true", obj.GetAnnotations()[AnnotationDevChartInsecure])
	})
}

func TestYAML(t *testing.T) {
	t.Run("renders the applied dogu resource", func(t *testing.T) {
		got, err := YAML(DevChart{
			Name:      "nexus",
			Namespace: "official",
			Version:   "3.96.4-3-dev.20261103143012",
			URL:       "oci://k3d-registry-proxy.localhost:5000/official/dogu/v3/charts/nexus",
			Insecure:  true,
		})

		require.NoError(t, err)
		assert.Equal(t, `apiVersion: k8s.cloudogu.com/v3beta1
kind: Dogu
metadata:
  annotations:
    k8s.cloudogu.com/dev-chart-insecure: "true"
    k8s.cloudogu.com/dev-chart-url: oci://k3d-registry-proxy.localhost:5000/official/dogu/v3/charts/nexus
  name: nexus
spec:
  doguApiVersion: v3
  doguNamespace: official
  name: nexus
  version: 3.96.4-3-dev.20261103143012
`, string(got))
	})
}

func TestClientApply(t *testing.T) {
	t.Run("creates a missing dogu", func(t *testing.T) {
		client, fake := newTestClient(t)

		require.NoError(t, client.Apply(context.Background(), devChartNexus))

		obj := getDogu(t, fake, "nexus")
		assert.Equal(t, devChartNexus.Version, nestedString(t, obj, "spec", "version"))
		assert.Equal(t, "v3", nestedString(t, obj, "spec", "doguApiVersion"))
		assert.Equal(t, devChartNexus.URL, obj.GetAnnotations()[AnnotationDevChartURL])
		assert.Equal(t, []string{FieldManager}, managers(obj))
	})

	t.Run("updates an existing dogu and keeps the fields of others", func(t *testing.T) {
		client, fake := newTestClient(t)
		applyAs(t, fake, "kubectl", map[string]any{
			"metadata": map[string]any{
				"name":        "nexus",
				"annotations": map[string]any{"example.com/owner": "team"},
			},
			"spec": map[string]any{
				"name":           "nexus",
				"doguNamespace":  "official",
				"version":        "3.96.4-3",
				"doguApiVersion": "v3",
				"stopped":        true,
				"values":         map[string]any{"replicas": int64(2)},
				"mappedValues":   map[string]any{"logLevel": "debug"},
			},
		})

		require.NoError(t, client.Apply(context.Background(), devChartNexus))

		obj := getDogu(t, fake, "nexus")
		assert.Equal(t, devChartNexus.Version, nestedString(t, obj, "spec", "version"))
		assert.Equal(t, devChartNexus.URL, obj.GetAnnotations()[AnnotationDevChartURL])
		assert.Equal(t, "team", obj.GetAnnotations()["example.com/owner"])
		assert.Equal(t, true, obj.Object["spec"].(map[string]any)["stopped"])
		assert.Equal(t, map[string]any{"replicas": int64(2)}, obj.Object["spec"].(map[string]any)["values"])
		assert.Equal(t, map[string]any{"logLevel": "debug"}, obj.Object["spec"].(map[string]any)["mappedValues"])
	})

	t.Run("removes the insecure annotation when it is no longer given", func(t *testing.T) {
		client, fake := newTestClient(t)
		insecure := devChartNexus
		insecure.Insecure = true
		require.NoError(t, client.Apply(context.Background(), insecure))
		require.Equal(t, "true", getDogu(t, fake, "nexus").GetAnnotations()[AnnotationDevChartInsecure])

		require.NoError(t, client.Apply(context.Background(), devChartNexus))

		annotations := getDogu(t, fake, "nexus").GetAnnotations()
		assert.NotContains(t, annotations, AnnotationDevChartInsecure)
		assert.Equal(t, devChartNexus.URL, annotations[AnnotationDevChartURL])
	})

	t.Run("returns the error of the cluster", func(t *testing.T) {
		client, fake := newTestClient(t)
		fake.PrependReactor("patch", "dogus", func(clienttesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("admission webhook denied")
		})

		err := client.Apply(context.Background(), devChartNexus)

		require.ErrorContains(t, err, "admission webhook denied")
		assert.ErrorContains(t, err, `failed to apply dogu nexus in namespace ecosystem`)
	})
}

// nestedString returns the string field at the path of the object.
func nestedString(t *testing.T, obj *unstructured.Unstructured, fields ...string) string {
	t.Helper()

	value, found, err := unstructured.NestedString(obj.Object, fields...)
	require.NoError(t, err)
	require.True(t, found, "field %v not found", fields)

	return value
}

// managers returns the field managers of the object.
func managers(obj *unstructured.Unstructured) []string {
	var names []string

	for _, entry := range obj.GetManagedFields() {
		names = append(names, entry.Manager)
	}

	return names
}
