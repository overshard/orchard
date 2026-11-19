package main

// Identity is hardcoded, never configured. Only secrets and paths a Dockerfile
// sets come from the environment.
const (
	baseURL  = "https://mcp.bythewood.me"
	siteName = "mcp"

	// Must match the first hostname label, like every other source label.
	selfSource = "mcp"
)

const (
	sourceURL = "https://github.com/overshard/orchard/tree/main/sites/mcp.bythewood.me"
	author    = "Isaac Bythewood"
)
