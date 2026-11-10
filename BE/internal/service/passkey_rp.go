package service

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"golang.org/x/net/publicsuffix"
)

// PasskeyRelyingParty is the WebAuthn relying party of an instance: the
// domain passkeys are bound to and the browser origins allowed to use them.
type PasskeyRelyingParty struct {
	ID          string
	DisplayName string
	Origins     []string
}

// NewPasskeyRelyingParty validates the relying party settings. An empty
// origins list falls back to the frontend URL and an empty rpID to the host of
// the first origin. Passkeys only work in a secure context, so an origin must
// be https, or http on localhost; anything else is refused here rather than
// failing later in the browser.
func NewPasskeyRelyingParty(rpID, origins, displayName, frontendURL string) (*PasskeyRelyingParty, error) {
	rawOrigins := splitList(origins)
	if len(rawOrigins) == 0 {
		rawOrigins = []string{frontendURL}
	}
	rp := &PasskeyRelyingParty{ID: strings.ToLower(strings.TrimSpace(rpID)), DisplayName: strings.TrimSpace(displayName)}
	if rp.DisplayName == "" {
		rp.DisplayName = "Kuayle"
	}
	hosts := make([]string, 0, len(rawOrigins))
	for _, raw := range rawOrigins {
		origin, host, err := passkeyOrigin(raw)
		if err != nil {
			return nil, err
		}
		rp.Origins = append(rp.Origins, origin)
		hosts = append(hosts, host)
	}
	if rp.ID == "" {
		rp.ID = hosts[0]
	}
	if net.ParseIP(rp.ID) != nil {
		return nil, fmt.Errorf("passkey relying party ID %q is an IP address; browsers require a host name", rp.ID)
	}
	if strings.ContainsAny(rp.ID, "/:@ ") {
		return nil, fmt.Errorf("passkey relying party ID %q must be a bare host name", rp.ID)
	}
	if suffix, icann := publicsuffix.PublicSuffix(rp.ID); icann && suffix == rp.ID {
		return nil, fmt.Errorf("passkey relying party ID %q is a public suffix", rp.ID)
	}
	for i, host := range hosts {
		if host != rp.ID && !strings.HasSuffix(host, "."+rp.ID) {
			return nil, fmt.Errorf("passkey origin %q is outside the relying party ID %q", rp.Origins[i], rp.ID)
		}
	}
	return rp, nil
}

// passkeyOrigin normalizes one origin to scheme://host[:port] and returns its host name.
func passkeyOrigin(raw string) (origin, host string, err error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" {
		return "", "", fmt.Errorf("passkey origin %q is not an absolute URL", raw)
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", "", fmt.Errorf("passkey origin %q must be scheme://host[:port] only", raw)
	}
	scheme := strings.ToLower(u.Scheme)
	host = strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	switch scheme {
	case "https":
	case "http":
		if host != "localhost" && !strings.HasSuffix(host, ".localhost") {
			return "", "", fmt.Errorf("passkey origin %q is plain http outside localhost; passkeys require https", raw)
		}
	default:
		return "", "", fmt.Errorf("passkey origin %q must use https", raw)
	}
	origin = scheme + "://" + host
	if port := u.Port(); port != "" && !(scheme == "https" && port == "443") && !(scheme == "http" && port == "80") {
		origin += ":" + port
	}
	return origin, host, nil
}

// allows reports whether a browser-reported origin is one of the relying party's.
func (rp *PasskeyRelyingParty) allows(origin string) bool {
	normalized, _, err := passkeyOrigin(origin)
	if err != nil {
		return false
	}
	for _, allowed := range rp.Origins {
		if allowed == normalized {
			return true
		}
	}
	return false
}

func splitList(value string) []string {
	var out []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
