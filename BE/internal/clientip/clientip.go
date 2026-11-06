// Package clientip resolves the address of the client behind Kuayle's reverse
// proxy for rate limiting, abuse guards and audit logs.
package clientip

import (
	"net/http"

	"github.com/labstack/echo/v5"
)

// extract walks X-Forwarded-For from the TCP peer towards the client and
// returns the first address outside the loopback, link-local and private
// networks that the bundled proxy and Docker Compose networks use. A peer
// outside those networks is the client itself, so the headers it sends are
// ignored; X-Real-IP is never trusted.
var extract = echo.ExtractIPFromXFFHeader(
	echo.TrustLoopback(true),
	echo.TrustLinkLocal(true),
	echo.TrustPrivateNet(true),
)

// FromRequest returns the client address of request. It can be used as an
// echo.IPExtractor.
func FromRequest(request *http.Request) string {
	return extract(request)
}
