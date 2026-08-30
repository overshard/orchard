package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Identity, stated once. Hardcode identity, never hardcode credentials: the
// password and nothing else comes from the environment.
const (
	baseURL     = "https://repos.bythewood.me"
	siteName    = "repos"
	siteTagline = "Isaac Bythewood's source code"
	authorName  = "Isaac Bythewood"
	githubUser  = "overshard"

	// The analytics property this site is tracked under, hardcoded exactly as
	// the other five sites hardcode theirs. Identity, not a credential: the id
	// is in the page source of every site here by design.
	analyticsID = "49f89ef6-b0b2-4b47-879e-7e252a067d0c"

	// The container name this site answers to on the Docker bridge. Shown on
	// every repository page as the seeding URL, because the moment somebody
	// needs it is the moment a first push has just failed with a 413, and a
	// URL in a README they would have to go and find is worse than one on the
	// page in front of them.
	//
	// Baked in rather than read at runtime, the same as the other four things
	// in this repo that reference a container by name.
	containerName = "orchard-repos"
)

// cloudflareBodyLimit is the hard ceiling on a single push through the tunnel,
// and it is Cloudflare's rather than this site's: 100MB on Free and Pro alike.
//
// It is a constant here so the UI can do arithmetic with it and warn before a
// push fails rather than after. A tunnel hostname is a CNAME to
// <uuid>.cfargotunnel.com, which is not publicly routable, so the usual
// "un-proxy that hostname" fix cannot apply here: the record has to stay
// proxied for the tunnel to work at all.
const cloudflareBodyLimit = 100 << 20

// Staging keeps a test hostname out of search results.
var Staging = !strings.HasSuffix(baseURL, "//repos.bythewood.me")

// Config is everything the environment sets.
type Config struct {
	// Password gates the UI. The site refuses to start without it rather
	// than defaulting to something guessable.
	Password string
	// RepoRoot holds the bare repositories. A volume in the image.
	RepoRoot string
	// DataDir holds repos.db. The same volume.
	DataDir string
	// MirrorEvery is how often the GitHub lane runs. Hours, not minutes:
	// twenty of the twenty-one repositories upstream are archived and will
	// never change again, so a tighter loop is pure waste.
	MirrorEvery time.Duration
	// GCEvery is how often repositories are repacked, which has to happen
	// somewhere now that receive.autogc is off.
	GCEvery time.Duration
	// MirrorEnabled turns the GitHub lane off, for a dev run that should not
	// clone 250MB before it serves a page.
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

	// Both paths are made absolute here, and it is not tidiness.
	//
	// git-http-backend resolves the repository as GIT_PROJECT_ROOT plus
	// PATH_INFO, and it runs with its working directory set to the project
	// root. A relative root therefore resolves against itself: "./build/repos"
	// becomes ./build/repos/build/repos, and every clone 404s with nothing in
	// the log to say why. The image sets /data and would never have shown
	// this; a dev run is where it bites.
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
