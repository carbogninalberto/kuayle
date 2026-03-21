package validate

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ValidateWebhookURL checks that a URL is safe to use as a webhook target.
// It blocks private, loopback, link-local, and metadata IP ranges to prevent SSRF.
func ValidateWebhookURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL")
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" && scheme != "http" {
		return fmt.Errorf("webhook URL must use http or https")
	}

	hostname := u.Hostname()
	if hostname == "" {
		return fmt.Errorf("webhook URL must have a hostname")
	}

	// Block well-known metadata hostnames
	blockedHosts := []string{
		"metadata.google.internal",
		"metadata.google.com",
	}
	lower := strings.ToLower(hostname)
	for _, blocked := range blockedHosts {
		if lower == blocked {
			return fmt.Errorf("webhook URL hostname is not allowed")
		}
	}

	// Resolve hostname and check IP ranges
	ips, err := net.LookupHost(hostname)
	if err != nil {
		return fmt.Errorf("cannot resolve webhook URL hostname")
	}

	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if isBlockedIP(ip) {
			return fmt.Errorf("webhook URL must not point to a private or reserved IP address")
		}
	}

	return nil
}

func isBlockedIP(ip net.IP) bool {
	// Private ranges (RFC 1918)
	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
	}
	// Loopback
	if ip.IsLoopback() {
		return true
	}
	// Link-local
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	// Unspecified (0.0.0.0)
	if ip.IsUnspecified() {
		return true
	}

	for _, cidr := range privateRanges {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if network.Contains(ip) {
			return true
		}
	}

	// AWS/cloud metadata endpoint (169.254.169.254)
	metadata := net.ParseIP("169.254.169.254")
	if ip.Equal(metadata) {
		return true
	}

	return false
}
