package main

// Identity is hardcoded, never configured. Only secrets and paths a Dockerfile
// sets come from the environment.
const (
	baseURL  = "https://gate.bythewood.me"
	siteName = "gate"

	// Must match the first hostname label, like every other source label.
	selfSource = "gate"
)

const (
	sourceURL = "https://github.com/overshard/orchard/tree/main/sites/gate.bythewood.me"
	author    = "Isaac Bythewood"
)
