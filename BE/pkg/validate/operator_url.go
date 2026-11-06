package validate

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// OperatorBaseURL parses the base URL of a service an instance operator
// configures (for example WEB_SEARCH_SEARXNG_URL). Such a URL may point at a
// private address, so it is accepted only in a plain form: http or https, a
// host and optional numeric port, no credentials, query or fragment, and a
// path of plain segments without dot segments or escapes. The result is
// lowercased in scheme and host and has no trailing slash.
func OperatorBaseURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "\\%?#@ \t\r\n") {
		return nil, errors.New("must be a plain http(s) URL without credentials, query, fragment or escapes")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("must be a valid URL")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, errors.New("must use http or https")
	}
	if parsed.Opaque != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawPath != "" {
		return nil, errors.New("must not contain credentials, a query, a fragment or escapes")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return nil, errors.New("must name a host")
	}
	if port := parsed.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return nil, errors.New("must use a port between 1 and 65535")
		}
		host = net.JoinHostPort(host, port)
	} else if strings.HasSuffix(parsed.Host, ":") {
		return nil, errors.New("must use a port between 1 and 65535")
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	path := strings.TrimSuffix(parsed.Path, "/")
	if path != "" {
		for _, segment := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
			if segment == "" || segment == "." || segment == ".." || strings.IndexFunc(segment, func(r rune) bool {
				return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-._~", r))
			}) >= 0 {
				return nil, errors.New("path must be plain segments of letters, digits, '-', '.', '_' or '~'")
			}
		}
	}
	return &url.URL{Scheme: scheme, Host: host, Path: path}, nil
}

// OperatorHostPort is the host:port a parsed operator URL dials, with the
// scheme's default port when none is given.
func OperatorHostPort(base *url.URL) string {
	port := base.Port()
	if port == "" {
		port = "80"
		if base.Scheme == "https" {
			port = "443"
		}
	}
	return net.JoinHostPort(strings.ToLower(base.Hostname()), port)
}
