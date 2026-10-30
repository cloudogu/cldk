package release

import (
	"fmt"

	"github.com/google/go-containerregistry/pkg/name"
)

// Target is the registry namespace a release is published to, including how to access the registry.
type Target struct {
	// Registry is the registry host, e.g. "registry.cloudogu.com".
	Registry string
	// Namespace is the namespace inside the registry, e.g. "official".
	Namespace string
	// User and Password authenticate against the registry. They are only kept in memory.
	// If both are empty, the stored credentials of helm and docker are used.
	User     string
	Password string
	// PlainHTTP allows unencrypted HTTP connections to the registry.
	PlainHTTP bool
	// InsecureSkipTLSVerify allows TLS connections without certificate verification, e.g. for self-signed certificates.
	InsecureSkipTLSVerify bool
}

// chartRepository returns the repository of a dogu chart in the target.
func (t Target) chartRepository(chartName string) string {
	return fmt.Sprintf("%s/%s/dogu/v3/charts/%s", t.Registry, t.Namespace, chartName)
}

// imageRepository returns the repository of a dogu image in the target.
func (t Target) imageRepository(imageName string) string {
	return fmt.Sprintf("%s/%s/dogu/v3/images/%s", t.Registry, t.Namespace, imageName)
}

// nameOptions returns the options to parse references of the target registry.
func (t Target) nameOptions() []name.Option {
	if t.PlainHTTP {
		return []name.Option{name.Insecure}
	}

	return nil
}
