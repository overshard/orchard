package main

import "strings"

// Identity, stated once and not made configurable.
//
// decisions/0008 draws the line here: hardcode identity, never hardcode
// credentials. A BASE_URL environment variable would be product scaffolding
// for a deployer who does not exist, and this site has no credentials at all,
// so it needs no .env of any kind.
//
// baseURL went to the real hostname at cutover on 2026-08-27. Staging below
// derives from it, so flipping this one line also turned off the noindex, the
// robots.txt Disallow and the blog feed the home page reads.
const (
	baseURL     = "https://isaacbythewood.com"
	siteTitle   = "Isaac Bythewood"
	siteDesc    = "Isaac Bythewood is a Senior Solutions Architect at Craftmaster Furniture located in Elkin, NC."
	themeColor  = "#20232e"
	githubUser  = "overshard"
	contactMail = "isaac@bythewood.me"
)

// Staging keeps the test hostname out of search results.
//
// Without this, next.isaacbythewood.com would be crawled and indexed as a
// complete duplicate of the real site, which is the one way a staging host can
// actually damage the production one. Both robots.txt and a noindex meta tag
// key off it, because a meta tag alone is useless on a URL robots.txt has
// already told the crawler not to fetch.
var Staging = !strings.HasSuffix(baseURL, "//isaacbythewood.com")

// blogLatestSources feed the home page's promo slot, in order of preference.
//
// The container name comes first, and that ordering is the whole point. The
// blog is a sibling container on the same Docker network, so reading it over
// the public hostname means a request leaving the house, crossing Cloudflare,
// coming back down the tunnel and through Caddy, to reach a process one bridge
// hop away. That is slower, and it makes an internal data fetch depend on
// public DNS, the tunnel and the edge all being healthy.
//
// The cutover proved the point rather than merely suggesting it. Every
// container still had blog.bythewood.me cached as the Linode's address for a
// while after the records moved, so the public URL returned 404 from a server
// that had never heard of /latest.json, and the card silently vanished. The
// container name was answering 200 the entire time.
//
// The public URL stays as a fallback for `make run`, where there is no Docker
// network and no sibling container. If neither answers the card is simply not
// rendered, which is the same graceful degradation the rest of this cache has.
var blogLatestSources = []string{
	"http://blog-next:8000/latest.json",
	"https://blog.bythewood.me/latest.json",
}

// NavPage is one entry in the menu and one value for the sidebar's counter.
type NavPage struct {
	Num   string
	Href  string
	Title string
}

var navPages = []NavPage{
	{Num: "000", Href: "/", Title: "Home"},
	{Num: "001", Href: "/about", Title: "About"},
	{Num: "002", Href: "/code", Title: "Code"},
	{Num: "003", Href: "/art", Title: "Art"},
	{Num: "004", Href: "/contact", Title: "Contact"},
}

// TopLink is one of the small monospace links across the top of the menu.
type TopLink struct {
	Label string
	Href  string
}

var topLinks = []TopLink{
	{Label: "Blog", Href: "https://blog.bythewood.me/"},
	{Label: "Status", Href: "https://status.bythewood.me/a40ff31d-18b0-49c3-9a36-deb02c909204"},
	{Label: "Analytics", Href: "https://analytics.bythewood.me/30e69c06-9beb-4283-8919-8c7a686ab013"},
	{Label: "GitHub", Href: "https://github.com/overshard"},
}

// Word is one entry in an animated word list.
type Word struct {
	Text string
	Slot int
}

var heroWords = []string{"AI Agents", "Automation", "DevOps", "Architecture"}

var aboutWords = []string{
	"AI Agents", "DevOps", "Full-Stack", "Testing", "Cloud",
	"Security", "Developer", "Automation", "Architecture",
}

// Project is one card on the code page.
type Project struct {
	Name        string
	Slug        string
	Description string
	Tech        []string
}

var projects = []Project{
	{
		Name:        "Taproot",
		Slug:        "taproot",
		Description: "Dotfiles, containers, and the configs that make a machine mine.",
		Tech:        []string{"Docker", "Shell"},
	},
	{
		Name:        "darkfurrow.com",
		Slug:        "darkfurrow.com",
		Description: "A living almanac of seasons, soil, and the quiet knowledge that used to be common.",
		Tech:        []string{"Rust", "Axum"},
	},
	{
		Name:        "Repos",
		Slug:        "repos",
		Description: "A minimal self-hosted git browser. Renders bare repos as a website with commits, diffs, syntax-highlighted blobs, and atom feeds, plus clone over HTTPS.",
		Tech:        []string{"Rust", "Axum"},
	},
	{
		Name:        "Analytics",
		Slug:        "analytics",
		Description: "A self-hostable analytics service with a straightforward API to track events from any source.",
		Tech:        []string{"Rust", "Axum", "SQLite"},
	},
	{
		Name:        "Status",
		Slug:        "status",
		Description: "A self-hosted uptime monitor and status page builder, with Lighthouse audits and PDF reports baked in.",
		Tech:        []string{"Rust", "Axum", "SQLite"},
	},
	{
		Name:        "Finance",
		Slug:        "finance",
		Description: "A self-hosted market watcher for stocks, ETFs, indexes, and futures, with live charts, key stats, fundamentals, and SEC filings.",
		Tech:        []string{"Rust", "Axum", "SQLite"},
	},
	{
		Name:        "blog.bythewood.me",
		Slug:        "blog.bythewood.me",
		Description: "A self-hostable markdown blog for developers, with code blocks, syntax highlighting, live search, great SEO, and a clean customizable UI.",
		Tech:        []string{"Rust", "Axum"},
	},
	{
		Name:        "Timelite",
		Slug:        "timelite",
		Description: "A simple time tracker that keeps everything local in your browser. No accounts, no sync, no server-side state.",
		Tech:        []string{"Next.js", "JavaScript"},
	},
	{
		// Updated rather than copied across: describing this page as Next.js
		// while it is being served by Go would be a plainly false statement on
		// the page itself.
		Name:        "isaacbythewood.com",
		Slug:        "isaacbythewood.com",
		Description: "The personal website you are looking at right now. Rebuilt from Next.js onto Go, server-rendered with html/template and a Vite-built frontend, served from home behind a Cloudflare Tunnel.",
		Tech:        []string{"Go", "Vite"},
	},
}

// Pour is one acrylic pour on the art page.
type Pour struct {
	Number   string
	Title    string
	Priority bool
}

var pours = []Pour{
	{Number: "006", Title: "Molten Copper", Priority: true},
	{Number: "005", Title: "Nebulas in Triangulum", Priority: true},
	{Number: "004", Title: "Metal on Mars", Priority: true},
	{Number: "003", Title: "Water on Jupiter", Priority: true},
	{Number: "002", Title: "Cracks in Clay", Priority: false},
	{Number: "001", Title: "Reef Drop-off", Priority: false},
	{Number: "000", Title: "Blood in Waves", Priority: false},
}

// Generative is one canvas piece on the art page.
type Generative struct {
	Number      string
	Title       string
	Slug        string
	Description string
	Source      string
	Autoplay    bool
}

var generative = []Generative{
	{
		Number: "000",
		Title:  "Constellations",
		Slug:   "constellations",
		Description: "Drifting points on a dark canvas that form connections with their nearest " +
			"neighbors. As stars wander closer together lines appear between them, brighter the " +
			"nearer they get, building and dissolving constellations that never repeat.",
		Source:   sourceURL("constellations"),
		Autoplay: true,
	},
	{
		Number: "001",
		Title:  "Retro Stars",
		Slug:   "retrostars",
		Description: "Layered star fields at different depths that drift in response to your cursor, " +
			"creating a parallax effect. Inspired by the pixel art aesthetic of Celeste and the " +
			"feeling of staring into a sky that moves with you.",
		Source: sourceURL("retrostars"),
	},
	{
		Number: "002",
		Title:  "Slime Mold",
		Slug:   "slimemold",
		Description: "Thousands of agents wander a dark field, each leaving a faint chemical trail " +
			"and steering toward the strongest scent ahead. From three simple rules (sense, turn, " +
			"deposit) emerge living networks reminiscent of Physarum slime molds, neurons, and the " +
			"cosmic web. The pattern never settles; trails decay as fast as they form.",
		Source: sourceURL("slimemold"),
	},
}

// sourceURL points at the canvas module in this monorepo rather than at the
// old per-project repo. The em dash in the original slime mold copy is gone
// too: workspace prose does not use them.
func sourceURL(slug string) string {
	return "https://github.com/" + githubUser +
		"/orchard/blob/main/sites/isaacbythewood.com/frontend/static_src/js/canvas/" + slug + ".js"
}

// ContactMethod is one row of the contact page's definition list.
type ContactMethod struct {
	Key      string
	Value    string
	Href     string
	External bool
}

var contactMethods = []ContactMethod{
	{Key: "Email", Value: contactMail, Href: "mailto:" + contactMail},
	{Key: "LinkedIn", Value: "Isaac Bythewood", Href: "https://www.linkedin.com/in/ibythewood/", External: true},
	{Key: "GitHub", Value: "/" + githubUser, Href: "https://github.com/" + githubUser, External: true},
	{Key: "Discord", Value: "Overshard#4907", Href: "https://discordapp.com/", External: true},
}
