package release

import (
	"crypto/tls"
	"net/http"
)

// newTransport creates the HTTP transport for all connections to the registry of the target.
func newTransport(target Target) *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()

	if target.InsecureSkipTLSVerify {
		t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicitly requested with --insecure
	}

	return t
}
