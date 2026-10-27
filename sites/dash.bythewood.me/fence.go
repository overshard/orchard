package main

import (
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"
)

// articleClient fetches links strangers submit, from a container on orchard-edge,
// so it refuses any address that isn't public. The check is in Control, after the
// name resolves, since checking the hostname leaves rebinding and redirects open.
var articleClient = publicClient(12 * time.Second)

func publicClient(timeout time.Duration) *http.Client {
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
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext:       d.DialContext,
			ForceAttemptHTTP2: true,
			IdleConnTimeout:   90 * time.Second,
		},
	}
}

func isPublicIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return false
	}
	// 100.64.0.0/10, which IsPrivate does not cover and is where a carrier
	// grade NAT and most of Tailscale live.
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
		return false
	}
	return true
}
