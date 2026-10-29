package release

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewTransport(t *testing.T) {
	tests := []struct {
		name              string
		target            Target
		wantSkipTLSVerify bool
	}{
		{"verifies certificates by default", Target{}, false},
		{"skips certificate verification with InsecureSkipTLSVerify", Target{InsecureSkipTLSVerify: true}, true},
		{"PlainHTTP does not affect certificate verification", Target{PlainHTTP: true}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := newTransport(tt.target)

			skipTLSVerify := transport.TLSClientConfig != nil && transport.TLSClientConfig.InsecureSkipVerify
			assert.Equal(t, tt.wantSkipTLSVerify, skipTLSVerify)
			assert.NotNil(t, transport.Proxy, "proxy settings of the environment should be kept")
		})
	}

	t.Run("does not change the default transport", func(t *testing.T) {
		transport := newTransport(Target{InsecureSkipTLSVerify: true})

		assert.NotSame(t, http.DefaultTransport, transport)

		defaultTLS := http.DefaultTransport.(*http.Transport).TLSClientConfig
		assert.False(t, defaultTLS != nil && defaultTLS.InsecureSkipVerify)
	})
}
