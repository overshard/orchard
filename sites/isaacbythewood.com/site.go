package main

import "strings"

// Identity is hardcoded instead of configured.
const (
	baseURL     = "https://isaacbythewood.com"
	siteTitle   = "Isaac Bythewood"
	siteDesc    = "Isaac Bythewood is a Senior Solutions Architect at Craftmaster Furniture located in Elkin, NC."
	themeColor  = "#20232e"
	githubUser  = "overshard"
	monorepo    = "orchard"
	contactMail = "isaac@bythewood.me"
)

// The container name first, since the blog is a sibling on the same bridge and
// the public hostname would route out through Cloudflare and back down the
// tunnel. The public URL is the fallback for `make run`.
var blogLatestSources = []string{
	"http://orchard-blog:8000/latest.json",
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

var aboutWords = []string{
	"AI Agents", "DevOps", "Full-Stack", "Testing", "Cloud",
	"Security", "Developer", "Automation", "Architecture",
}

// Site is one tile in the live section: something in orchard that is running
// right now, as opposed to a repository you can only read.
type Site struct {
	Name        string
	Symbol      string
	Group       string
	Dir         string
	Description string
	Tech        []string
	// Empty for the edge, which has no page of its own to visit.
	Visit string
}

// Group is one row of the live section, in the order the page shows them.
type Group struct {
	Name  string
	Blurb string
}

var groups = []Group{
	{Name: "Edge", Blurb: "keeps things out"},
	{Name: "Watch", Blurb: "knows what's happening"},
	{Name: "Tools", Blurb: "for me"},
	{Name: "Public", Blurb: "for everyone"},
}

// The topology draws them in this order too, so it reads top to bottom the
// same way the tiles do.
var sites = []Site{
	{
		Name:        "gate",
		Symbol:      "Gt",
		Group:       "Edge",
		Dir:         "gate.bythewood.me",
		Description: "The firewall in front of every site. Caddy asks it about each request, rules change live, and bots get a proof of work page.",
		Tech:        []string{"Go", "SQLite"},
		Visit:       "https://gate.bythewood.me",
	},
	{
		Name:        "edge",
		Symbol:      "Ed",
		Group:       "Edge",
		Description: "The cloudflared tunnel that dials out to Cloudflare, the Caddy that routes to every site, and the ntfy every alert goes through.",
		Tech:        []string{"cloudflared", "Caddy", "ntfy"},
	},
	{
		Name:        "auth",
		Symbol:      "Au",
		Group:       "Edge",
		Dir:         "auth.bythewood.me",
		Description: "The front door to the rest of these. One account, a six digit code pushed to my phone over ntfy, and a session the other sites check against this one.",
		Tech:        []string{"Go", "SQLite", "Argon2"},
		Visit:       "https://auth.bythewood.me",
	},
	{
		Name:        "dns",
		Symbol:      "Dn",
		Group:       "Edge",
		Dir:         "dns.bythewood.me",
		Description: "A private resolver for my own devices over DoH and DoT, with blocklists, rules per device and a log of every lookup.",
		Tech:        []string{"Go", "unbound", "SQLite"},
		Visit:       "https://dns.bythewood.me",
	},
	{
		Name:        "status",
		Symbol:      "St",
		Group:       "Watch",
		Dir:         "status.bythewood.me",
		Description: "Uptime checks and public status pages, a daily audit written in Go and an SEO crawler, with alerts to my phone.",
		Tech:        []string{"Go", "SQLite", "Typst"},
		Visit:       "https://status.bythewood.me",
	},
	{
		Name:        "logging",
		Symbol:      "Lg",
		Group:       "Watch",
		Dir:         "logging.bythewood.me",
		Description: "Every site ships its structured logs here along with Caddy's access log, so a request I want to find is in one place instead of spread across every container.",
		Tech:        []string{"Go", "SQLite", "Typst"},
		Visit:       "https://logging.bythewood.me",
	},
	{
		Name:        "analytics",
		Symbol:      "An",
		Group:       "Watch",
		Dir:         "analytics.bythewood.me",
		Description: "Self hosted analytics with dashboards you can share without a login, a world map, real visitor vitals and Typst PDF reports.",
		Tech:        []string{"Go", "SQLite", "GeoIP"},
		Visit:       "https://analytics.bythewood.me",
	},
	{
		Name:        "dash",
		Symbol:      "Da",
		Group:       "Watch",
		Dir:         "dash.bythewood.me",
		Description: "One page of markets, news, the weather and the health of everything else here, all of it pushing itself over server sent events.",
		Tech:        []string{"Go", "SSE", "Vite"},
		Visit:       "https://dash.bythewood.me",
	},
	{
		Name:        "repos",
		Symbol:      "Rp",
		Group:       "Tools",
		Dir:         "repos.bythewood.me",
		Description: "A git remote I push to over HTTPS with a browser on top, commits, diffs, blobs and atom feeds. It mirrors my GitHub account too.",
		Tech:        []string{"Go", "Git", "SQLite"},
		Visit:       "https://repos.bythewood.me",
	},
	{
		Name:        "llm",
		Symbol:      "Ll",
		Group:       "Tools",
		Dir:         "llm.bythewood.me",
		Description: "The model gateway. Claude Haiku behind a key per caller, a monthly budget, and every call written down.",
		Tech:        []string{"Go", "SQLite"},
		Visit:       "https://llm.bythewood.me",
	},
	{
		Name:        "mcp",
		Symbol:      "Mc",
		Group:       "Tools",
		Dir:         "mcp.bythewood.me",
		Description: "Every tool here, read only, as one MCP server for Claude, signed in through auth.",
		Tech:        []string{"Go", "OAuth", "MCP"},
		Visit:       "https://mcp.bythewood.me",
	},
	{
		Name:        "cron",
		Symbol:      "Cr",
		Group:       "Tools",
		Dir:         "cron.bythewood.me",
		Description: "Reminders and scheduled checks, pushed to my phone over ntfy.",
		Tech:        []string{"Go", "SQLite"},
		Visit:       "https://cron.bythewood.me",
	},
	{
		Name:        "pond",
		Symbol:      "Po",
		Group:       "Public",
		Dir:         "pond.bythewood.me",
		Description: "A virtual pet game where everybody who opens it gets a frog of their own, drawn from a seed as pixel art. There are no accounts, a frog is kept by six words, and you raise it, put it to work and spend pebbles on hats. Tap the frog for another one.",
		Tech:        []string{"Go", "SQLite", "Canvas"},
		Visit:       "https://pond.bythewood.me",
	},
	{
		Name:        "blog",
		Symbol:      "Bl",
		Group:       "Public",
		Dir:         "blog.bythewood.me",
		Description: "Markdown files on disk and no database behind them, with live search, and a PDF and a social card built for every post.",
		Tech:        []string{"Go", "Typst", "Vite"},
		Visit:       "https://blog.bythewood.me",
	},
	{
		Name:        "isaacbythewood.com",
		Symbol:      "Ib",
		Group:       "Public",
		Dir:         "isaacbythewood.com",
		Description: "The site you are reading. Go and Vite with no third party Go dependencies at all, canvas animations included.",
		Tech:        []string{"Go", "Vite", "Canvas"},
		Visit:       "https://isaacbythewood.com",
	},
}

// The tile the page opens with.
const openSite = "pond"

// SourcePath is the site's directory inside the monorepo, which is both the
// GitHub path filter for its commits and the tail of its source link.
func (s Site) SourcePath() string {
	if s.Dir == "" {
		return "edge"
	}
	return "sites/" + s.Dir
}

// Host is what the tile prints under its symbol.
func (s Site) Host() string {
	if s.Dir == "" {
		return "edge/"
	}
	return s.Dir
}

func (s Site) CommitTarget() CommitTarget {
	return CommitTarget{Key: s.SourcePath(), Repo: monorepo, Path: s.SourcePath()}
}

// Project is one card on the code page.
type Project struct {
	Name        string
	Slug        string
	Description string
	Tech        []string
	// Archived splits the page so a read-only snapshot doesn't sit in a wall
	// of cards that all look equally alive.
	Archived bool
}

var projects = []Project{
	{
		Name:        "taproot",
		Slug:        "taproot",
		Description: "The container the VPS runs for development, the setup for every thin client that talks to it, and my dotfiles.",
		Tech:        []string{"Docker", "Shell"},
	},

	// Archived, newest first, and named exactly as the repository is on GitHub,
	// so a card title is something you can search for.
	{
		Name:        "isaacbythewood.com-nextjs",
		Slug:        "isaacbythewood.com-nextjs",
		Description: "The Next.js build of this site: a custom cursor, a page transition loader, and full canvas background animations. Replaced by the Go rebuild you are reading now.",
		Tech:        []string{"Next.js", "React"},
		Archived:    true,
	},
	{
		Name:        "blog.bythewood.me-rust",
		Slug:        "blog.bythewood.me-rust",
		Description: "Single binary markdown blog: no database, live search, Typst PDF export, and great SEO.",
		Tech:        []string{"Rust", "Axum", "Typst"},
		Archived:    true,
	},
	{
		Name:        "blog.bythewood.me-flask",
		Slug:        "blog.bythewood.me-flask",
		Description: "The Flask rewrite of my blog: markdown files on disk, no database, and no CMS behind it. Replaced by the Rust build above.",
		Tech:        []string{"Python", "Flask"},
		Archived:    true,
	},
	{
		Name:        "blog.bythewood.me-wagtail",
		Slug:        "blog.bythewood.me-wagtail",
		Description: "The Django and Wagtail build of my blog, with a real CMS behind it. The last handcoded version, and where the blog spent most of its life.",
		Tech:        []string{"Python", "Django", "Wagtail"},
		Archived:    true,
	},
	{
		Name:        "darkfurrow.com-rust",
		Slug:        "darkfurrow.com-rust",
		Description: "A living almanac of seasons, soil, and the quiet knowledge that used to be common. Taken offline, and I no longer own the domain.",
		Tech:        []string{"Rust", "Axum"},
		Archived:    true,
	},
	{
		Name:        "status-django",
		Slug:        "status-django",
		Description: "The Django build of my uptime monitor and status page builder, before it was rewritten in Rust.",
		Tech:        []string{"Python", "Django", "SQLite"},
		Archived:    true,
	},
	{
		Name:        "analytics-django",
		Slug:        "analytics-django",
		Description: "The Django build of my self-hosted website analytics, before it was rewritten in Rust.",
		Tech:        []string{"Python", "Django", "SQLite"},
		Archived:    true,
	},
	{
		Name:        "status-rust",
		Slug:        "status-rust",
		Description: "Single binary uptime monitoring and status pages: HTTP probes, Lighthouse audits, an SEO crawler, and PDF reports.",
		Tech:        []string{"Rust", "Axum", "SQLite"},
		Archived:    true,
	},
	{
		Name:        "analytics-rust",
		Slug:        "analytics-rust",
		Description: "Single binary website analytics: a collector API, dashboards, a world map, and PDF reports.",
		Tech:        []string{"Rust", "Axum", "SQLite"},
		Archived:    true,
	},
	{
		Name:        "timelite-nextjs",
		Slug:        "timelite-nextjs",
		Description: "Time tracking that never leaves your browser. No accounts, no sync, no server side state.",
		Tech:        []string{"Next.js", "React"},
		Archived:    true,
	},
	{
		Name:        "repos-rust",
		Slug:        "repos-rust",
		Description: "A minimal self-hosted git browser. Bare repos as a website: commits, diffs, syntax highlighted blobs, atom feeds, and clone over HTTPS.",
		Tech:        []string{"Rust", "Axum", "gix"},
		Archived:    true,
	},
	{
		Name:        "finance-rust",
		Slug:        "finance-rust",
		Description: "A self-hosted market watcher for stocks, ETFs, indexes and futures, with live charts, key stats, fundamentals and SEC filings.",
		Tech:        []string{"Rust", "Axum", "SQLite"},
		Archived:    true,
	},
	{
		Name:        "dockerfiles",
		Slug:        "dockerfiles",
		Description: "All the Dockerfiles I used for various purposes, each with its usage notes at the top.",
		Tech:        []string{"Docker"},
		Archived:    true,
	},
	{
		Name:        "dotfiles",
		Slug:        "dotfiles",
		Description: "Config files for setting up a new system the way I like it.",
		Tech:        []string{"Shell", "Neovim"},
		Archived:    true,
	},
	{
		Name:        "timestrap-django",
		Slug:        "timestrap-django",
		Description: "Time tracking you can host anywhere, with full export in several formats and an extensible core.",
		Tech:        []string{"Python", "Django"},
		Archived:    true,
	},
	{
		Name:        "alpinefiles",
		Slug:        "alpinefiles",
		Description: "The files I ran on my Alpine Linux servers: Caddy, Docker, borg backups, and ufw.",
		Tech:        []string{"Shell", "Alpine"},
		Archived:    true,
	},
	{
		Name:        "newtab",
		Slug:        "newtab",
		Description: "A clean new tab page extension for Chrome, built to be taken and customized.",
		Tech:        []string{"JavaScript", "Chrome"},
		Archived:    true,
	},
	{
		Name:        "ai-art",
		Slug:        "ai-art",
		Description: "Art generation with VQGAN and CLIP in docker containers. A simplified, updated and expanded take on the original notebooks.",
		Tech:        []string{"Python", "PyTorch"},
		Archived:    true,
	},
	{
		Name:        "docker-teamspeak",
		Slug:        "docker-teamspeak",
		Description: "A nice and easy way to get a TeamSpeak server up and running with Docker.",
		Tech:        []string{"Docker", "Shell"},
		Archived:    true,
	},
	{
		Name:        "docker-minecraft",
		Slug:        "docker-minecraft",
		Description: "An easy way to get a Minecraft server up and running with Docker.",
		Tech:        []string{"Docker", "Shell"},
		Archived:    true,
	},
	{
		Name:        "pinry",
		Slug:        "pinry",
		Description: "A tiling image board system. Development moved on to pinry/pinry.",
		Tech:        []string{"Python"},
		Archived:    true,
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

func sourceURL(slug string) string {
	return "https://github.com/" + githubUser +
		"/orchard/blob/main/sites/isaacbythewood.com/frontend/static_src/js/canvas/" + slug + ".js"
}

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

func (s Site) Stack() string {
	return strings.Join(s.Tech, ", ")
}
