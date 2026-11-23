package main

// Identity is hardcoded, never configured. Only secrets and paths a Dockerfile
// sets come from the environment.
const (
	baseURL  = "https://pass.bythewood.me"
	siteName = "pass"
	siteDesc = "A Bitwarden server for my own devices, with who signed in to it and from where."

	// Must match the first hostname label, like every other source label.
	selfSource = "pass"

	// The property id from analytics.bythewood.me. It is in the page source by
	// design, so it is not a secret.
	analyticsID = "ef2a9eab-9172-4b49-9264-f6b548c497d9"

	// Vaultwarden answers the Bitwarden apps from its own container, and Caddy
	// sends it their paths. This site only reads its database and asks its version.
	vaultURL    = "http://orchard-pass-vw:8000"
	releasesURL = "https://api.github.com/repos/dani-garcia/vaultwarden/releases/latest"
)

const (
	sourceURL = "https://github.com/overshard/orchard/tree/main/sites/pass.bythewood.me"
	author    = "Isaac Bythewood"
)
