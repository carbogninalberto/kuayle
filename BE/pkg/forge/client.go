// Package forge holds pieces shared by the forge provider integrations.
package forge

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"

	"github.com/kuayle/kuayle-backend/pkg/validate"
)

// NewHTTPClient builds the client used for outbound forge API calls. Unless
// private networks are explicitly allowed, every connection is dialled through
// validate.PublicAddressControl and redirects are not followed, so an
// admin-supplied base URL cannot reach internal services.
func NewHTTPClient(allowPrivateNetwork bool) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	if !allowPrivateNetwork {
		dialer.Control = validate.PublicAddressControl
	}
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
			TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
