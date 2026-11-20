package validate

import "net"

var carrierGradeNAT = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// IsPublicIP reports whether ip is routable on the public internet. It rejects
// loopback, private (including IPv6 unique local), link-local (including cloud
// metadata), unspecified, multicast and carrier-grade NAT addresses.
func IsPublicIP(ip net.IP) bool {
	return ip != nil && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast() && !ip.IsInterfaceLocalMulticast() && !ip.IsUnspecified() &&
		!ip.IsMulticast() && !carrierGradeNAT.Contains(ip)
}
