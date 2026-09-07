package tools

// The fence between the open web and this machine.
//
// Every public tool here takes a url or a query from a model, and a model will
// happily be talked into fetching whatever it is handed. This site now sits on
// orchard-edge, so an unguarded fetch of http://orchard-auth:8000 or
// http://127.0.0.1 reaches the estate from inside, past Caddy and past the
// tunnel, which is the one place nothing is expecting an untrusted caller.
//
// The check is on the address actually dialled rather than on the string, so a
// redirect to an internal host and a name that resolves to one are both caught.
// The orchard_ tools deliberately do not go through this: they are the sanctioned
// way in, they name their host, and they carry the caller's own session.

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// publicClient refuses to connect to anything that is not a public address.
//
// Control runs after the name is resolved and before the socket is connected,
// which is the only point where the decision can be made on the address that
// will really be used. Checking the hostname instead leaves DNS rebinding open.
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
		Timeout:   timeout,
		Transport: &http.Transport{DialContext: d.DialContext, ForceAttemptHTTP2: true},
	}
}

func isPublicIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return false
	}
	// 100.64.0.0/10, which is not covered by IsPrivate and is where a carrier
	// grade NAT and most of Tailscale live.
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
		return false
	}
	// The cloud metadata address, which is public by every other measure and is
	// the first thing anything like this gets pointed at.
	if ip.Equal(net.IPv4(169, 254, 169, 254)) {
		return false
	}
	return true
}

// publicURL is the check on the string, done before the dial so an obviously
// wrong scheme is refused with something a model can act on rather than with a
// connection error.
func publicURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("that is not a url")
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	case "":
		return "", fmt.Errorf("the url needs to start with https://")
	case "file":
		// The one a model reaches for when it is thinking about an attachment,
		// so the message says where the file actually is.
		return "", fmt.Errorf("there is no filesystem to read here, and an attached file is already in this conversation, so read it there rather than fetching it")
	default:
		return "", fmt.Errorf("only http and https can be fetched, not %s", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("the url has no host")
	}
	// A bare name with no dot is a container on the bridge, and the estate is
	// reached with the orchard tools rather than by fetching it.
	host := u.Hostname()
	if !strings.Contains(host, ".") && net.ParseIP(host) == nil {
		return "", fmt.Errorf("%q is not a public address, and this machine's own services are read with the orchard tools", host)
	}
	return u.String(), nil
}
