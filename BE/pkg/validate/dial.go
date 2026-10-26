package validate

import (
	"errors"
	"net"
	"syscall"
)

// ErrNonPublicAddress reports an outbound connection to a non-public address.
var ErrNonPublicAddress = errors.New("destination address is not public")

var carrierGradeNAT = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// IsPublicIP reports whether ip is routable on the public internet. It rejects
// loopback, private (including IPv6 unique local), link-local (including cloud
// metadata), unspecified, multicast and carrier-grade NAT addresses.
func IsPublicIP(ip net.IP) bool {
	return ip != nil && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast() && !ip.IsInterfaceLocalMulticast() && !ip.IsUnspecified() &&
		!ip.IsMulticast() && !carrierGradeNAT.Contains(ip)
}

// PublicAddressControl is a net.Dialer Control function that refuses
// connections to non-public addresses. It runs after DNS resolution, so a
// hostname that resolves or rebinds to an internal address is refused too.
func PublicAddressControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil || !IsPublicIP(net.ParseIP(host)) {
		return ErrNonPublicAddress
	}
	return nil
}
