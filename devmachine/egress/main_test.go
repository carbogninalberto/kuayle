package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConnectAllowsOnlyTLSPortForApprovedDomains(t *testing.T) {
	p := &proxy{allow: []string{"allowed.example"}, deny: []string{"blocked.allowed.example"}}
	for _, test := range []struct {
		name      string
		authority string
		allowed   bool
	}{
		{name: "exact domain", authority: "allowed.example:443", allowed: true},
		{name: "subdomain", authority: "api.allowed.example:443", allowed: true},
		{name: "port 80 tunnel", authority: "allowed.example:80"},
		{name: "alternate port", authority: "allowed.example:8443"},
		{name: "explicitly denied", authority: "blocked.allowed.example:443"},
		{name: "denied subdomain", authority: "deep.blocked.allowed.example:443"},
		{name: "outside allowlist", authority: "other.example:443"},
		{name: "literal IPv4", authority: "8.8.8.8:443"},
		{name: "literal IPv6", authority: "[2001:4860:4860::8888]:443"},
		{name: "zoned IPv6 literal", authority: "[fe80::1%eth0]:443"},
		{name: "empty host", authority: ":443"},
		{name: "missing port", authority: "allowed.example"},
		{name: "malformed", authority: "allowed.example:443:extra"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if allowed := p.connectAllowed(test.authority); allowed != test.allowed {
				t.Fatalf("connectAllowed(%q)=%t, expected %t", test.authority, allowed, test.allowed)
			}
		})
	}
}

func TestConnectPort80IsRejectedBeforeDNS(t *testing.T) {
	p := &proxy{
		lookupIP: func(context.Context, string) ([]net.IPAddr, error) {
			t.Fatal("port 80 CONNECT reached DNS resolution")
			return nil, nil
		},
	}
	request := httptest.NewRequest(http.MethodConnect, "http://proxy.invalid", nil)
	request.Host = "public.example:80"
	response := httptest.NewRecorder()

	p.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("expected port 80 CONNECT to return 403, got %d", response.Code)
	}
}

func TestDialContextPinsValidatedPublicDNSResultForHTTPAndHTTPS(t *testing.T) {
	dialError := errors.New("dial stopped after address validation")
	for _, port := range []string{"80", "443"} {
		t.Run(port, func(t *testing.T) {
			var dialedAddress string
			p := &proxy{
				lookupIP: func(_ context.Context, host string) ([]net.IPAddr, error) {
					if host != "public.example" {
						t.Fatalf("unexpected lookup host %q", host)
					}
					return []net.IPAddr{
						{IP: net.ParseIP("127.0.0.1")},
						{IP: net.ParseIP("10.0.0.1")},
						{IP: net.ParseIP("100.64.0.1")},
						{IP: net.ParseIP("8.8.8.8")},
					}, nil
				},
				dial: func(_ context.Context, network, address string) (net.Conn, error) {
					if network != "tcp" {
						t.Fatalf("unexpected dial network %q", network)
					}
					dialedAddress = address
					return nil, dialError
				},
			}

			_, err := p.dialContext(context.Background(), "tcp", net.JoinHostPort("public.example", port))

			if !errors.Is(err, dialError) {
				t.Fatalf("expected controlled dial error, got %v", err)
			}
			expected := net.JoinHostPort("8.8.8.8", port)
			if dialedAddress != expected {
				t.Fatalf("dialed %q, expected pinned public address %q", dialedAddress, expected)
			}
		})
	}
}

func TestDialContextRejectsPrivateAndCGNATDNSResults(t *testing.T) {
	p := &proxy{
		lookupIP: func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{
				{IP: net.ParseIP("0.0.0.0")},
				{IP: net.ParseIP("::")},
				{IP: net.ParseIP("::1")},
				{IP: net.ParseIP("169.254.169.254")},
				{IP: net.ParseIP("fe80::1")},
				{IP: net.ParseIP("192.168.1.1")},
				{IP: net.ParseIP("fc00::1")},
				{IP: net.ParseIP("100.127.255.254")},
				{IP: net.ParseIP("ff02::1")},
			}, nil
		},
		dial: func(context.Context, string, string) (net.Conn, error) {
			t.Fatal("private DNS result reached the dialer")
			return nil, nil
		},
	}

	_, err := p.dialContext(context.Background(), "tcp", "private.example:443")
	if err == nil || err.Error() != "no public address for host" {
		t.Fatalf("expected private-address rejection, got %v", err)
	}
}
