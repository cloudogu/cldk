// Package kube connects to a Kubernetes cluster the way kubectl does.
package kube

import (
	"errors"
	"fmt"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

// Options select the cluster and namespace from the kubeconfig.
type Options struct {
	// Context is the kubeconfig context to use. If empty, the current context is used.
	Context string
	// Namespace is the Kubernetes namespace to use. If empty, the namespace of the context is used, otherwise "default".
	Namespace string
}

// Info describes the cluster and namespace a connection goes to.
type Info struct {
	// Context is the name of the kubeconfig context, e.g. "k3d-dev1".
	Context string
	// Server is the address of the API server, e.g. "https://0.0.0.0:6443".
	Server string
	// Namespace is the Kubernetes namespace, e.g. "ecosystem".
	Namespace string
}

// Cluster is a connection to a Kubernetes cluster.
type Cluster struct {
	// Dynamic accesses arbitrary resources of the cluster.
	Dynamic dynamic.Interface
	// Info describes the cluster and namespace.
	Info Info
}

// Connect creates the clients for the cluster selected by the kubeconfig like kubectl: the files of KUBECONFIG, otherwise
// ~/.kube/config, with the context and namespace of the options. It does not contact the cluster.
func Connect(opts Options) (Cluster, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{CurrentContext: opts.Context}
	overrides.Context.Namespace = opts.Namespace
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides)

	raw, err := clientConfig.RawConfig()
	if err != nil {
		return Cluster{}, fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	contextName := opts.Context

	if contextName == "" {
		contextName = raw.CurrentContext
	}

	if contextName == "" {
		return Cluster{}, errors.New("failed to load kubeconfig: no current context, select a context of the kubeconfig")
	}

	if _, ok := raw.Contexts[contextName]; !ok {
		return Cluster{}, fmt.Errorf("failed to load kubeconfig: context %q not found", contextName)
	}

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return Cluster{}, fmt.Errorf("failed to load kubeconfig of context %q: %w", contextName, err)
	}

	namespace, _, err := clientConfig.Namespace()
	if err != nil {
		return Cluster{}, fmt.Errorf("failed to determine the namespace of context %q: %w", contextName, err)
	}

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return Cluster{}, fmt.Errorf("failed to create client for context %q: %w", contextName, err)
	}

	return Cluster{
		Dynamic: dynamicClient,
		Info:    Info{Context: contextName, Server: restConfig.Host, Namespace: namespace},
	}, nil
}
