package main

// Identity is hardcoded, never configured. Only secrets and paths a Dockerfile
// sets come from the environment.
const (
	baseURL  = "https://dns.bythewood.me"
	siteName = "dns"

	// Must match the first hostname label, like every other source label.
	selfSource = "dns"

	// Each device's DoT name is its token under this zone, and the probe the
	// setup page fires is a fresh label under probeZone.
	dotZone   = "dns.bythewood.me"
	probeZone = "check.dns.bythewood.me"

	// The box's own address, which the DoT record points at since port 853
	// can't go through the tunnel.
	publicIP = "15.204.248.200"

	upstreamAddr = "orchard-dns-unbound:5353"
	gateCheckURL = "http://orchard-gate:8000/check"
	appURL       = "http://orchard-dns:8000"
)

const (
	sourceURL = "https://github.com/overshard/orchard/tree/main/sites/dns.bythewood.me"
	author    = "Isaac Bythewood"
)
