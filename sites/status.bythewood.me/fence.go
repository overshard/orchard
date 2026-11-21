package main

import (
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"
)

// publicTransport dials only public addresses, since a crawl follows links and
// redirects strangers wrote from a container on orchard-edge. The check is in
// Control, after the name resolves, so rebinding and redirects are caught too.
func publicTransport() *http.Transport {
	d := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return fmt.Errorf("refusing an address that cannot be parsed")
			}
			ip := net.ParseIP(host)
			if ip == nil {
				return fmt.Errorf("refusing an address that is not an ip")
			}
			if !isPublicIP(ip) {
				return fmt.Errorf("refusing to fetch %s, which is on this machine or its network", ip)
			}
			return nil
		},
	}
	return &http.Transport{
		DialContext:         d.DialContext,
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: 10 * time.Second,
		IdleConnTimeout:     90 * time.Second,
	}
}

// isPublicIP refuses loopback, the private ranges (which hold Docker's bridges),
// link local, multicast and CGNAT.
func isPublicIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		// 0.0.0.0/8 reaches this machine on Linux, and 100.64.0.0/10 is carrier
		// grade NAT and Tailscale, neither of which IsPrivate covers.
		if v4[0] == 0 || (v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127) {
			return false
		}
	}
	return true
}
