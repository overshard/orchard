package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	baseURL     = "https://repos.bythewood.me"
	siteName    = "repos"
	siteTagline = "Isaac Bythewood's source code"
	authorName  = "Isaac Bythewood"
	githubUser  = "overshard"

	// analyticsID is in the page source of every site; it is identity, not a credential.
	analyticsID = "49f89ef6-b0b2-4b47-879e-7e252a067d0c"

	// containerName is baked in, not read at runtime. Repository pages show it
	// as the bridge URL to seed a first push through, bypassing Cloudflare.
	containerName = "orchard-repos"
)

// cloudflareBodyLimit is Cloudflare's ceiling on a proxied request body, 100MB on
// Free and Pro. A tunnel hostname must stay proxied, so nothing here can raise it.
const cloudflareBodyLimit = 100 << 20

// Staging keeps a test hostname out of search results.
var Staging = !strings.HasSuffix(baseURL, "//repos.bythewood.me")

// Config is everything the environment sets.
type Config struct {
	// Password gates the UI. The site refuses to start without it.
	Password string
	// RepoRoot holds the bare repositories.
	RepoRoot string
	// DataDir holds repos.db.
	DataDir string
	// MirrorEvery is how often the GitHub mirror lane runs.
	MirrorEvery time.Duration
	// GCEvery is how often repositories are repacked, which nothing else does
	// now that receive.autogc is off.
	GCEvery time.Duration
	// MirrorEnabled turns the GitHub lane off for dev runs.
	MirrorEnabled bool
}

func LoadConfig() Config {
	c := Config{
		Password:      os.Getenv("REPOS_PASSWORD"),
		RepoRoot:      env("REPOS_ROOT", "./build/repos"),
		DataDir:       env("REPOS_DATA", "./build/data"),
		MirrorEvery:   6 * time.Hour,
		GCEvery:       24 * time.Hour,
		MirrorEnabled: os.Getenv("REPOS_MIRROR") != "0",
	}

	// git-http-backend runs with its working directory set to GIT_PROJECT_ROOT
	// and resolves repositories under it, so a relative root resolves against
	// itself and every clone 404s.
	if abs, err := filepath.Abs(c.RepoRoot); err == nil {
		c.RepoRoot = abs
	}
	if abs, err := filepath.Abs(c.DataDir); err == nil {
		c.DataDir = abs
	}
	return c
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// FooterLink is one entry in the footer.
type FooterLink struct {
	Label string
	Href  string
}

var footerLinks = []FooterLink{
	{Label: "Portfolio", Href: "https://isaacbythewood.com/"},
	{Label: "Blog", Href: "https://blog.bythewood.me/"},
	{Label: "GitHub", Href: "https://github.com/" + githubUser},
	{Label: "Status", Href: "https://status.bythewood.me/"},
}

const sourceURL = "https://github.com/" + githubUser + "/orchard/tree/main/sites/repos.bythewood.me"
