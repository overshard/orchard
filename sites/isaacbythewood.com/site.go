package main

import "strings"

// Identity, stated once and not made configurable: hardcode identity, never
// hardcode credentials. This site has no credentials at all, so it needs no
// .env of any kind.
//
// Staging below derives from baseURL, so this one line also controls the
// noindex, the robots.txt Disallow and the blog feed the home page reads.
const (
	baseURL     = "https://isaacbythewood.com"
	siteTitle   = "Isaac Bythewood"
	siteDesc    = "Isaac Bythewood is a Senior Solutions Architect at Craftmaster Furniture located in Elkin, NC."
	themeColor  = "#20232e"
	githubUser  = "overshard"
	contactMail = "isaac@bythewood.me"
)

// Staging keeps a test hostname out of search results, so it is not indexed as
// a duplicate of the real site. Both robots.txt and a noindex meta tag key off
// it, because a meta tag alone is useless on a URL robots.txt has already told
// the crawler not to fetch.
var Staging = !strings.HasSuffix(baseURL, "//isaacbythewood.com")

// blogLatestSources feed the home page's promo slot, in order of preference.
//
// The container name comes first. The blog is a sibling container on the same
// Docker network, so reading it over the public hostname would send a request
// out of the house, across Cloudflare and back down the tunnel to reach a
// process one bridge hop away, and would make an internal fetch depend on
// public DNS, the tunnel and the edge all being healthy.
//
// The public URL is the fallback for `make run`, where there is no Docker
// network and no sibling container. If neither answers, the card is not
// rendered.
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
	// Archived splits the page, so a read-only snapshot does not sit in a wall
	// of cards that all look equally alive. See the archived section in
	// code.html.
	Archived bool
}

var projects = []Project{
	{
		Name:        "orchard",
		Slug:        "orchard",
		Description: "Every site I run, in one repo, with the shared Cloudflare Tunnel and Caddy that front them. Served from a desktop at home, this site included.",
		Tech:        []string{"Go", "Vite", "SQLite"},
	},
	{
		Name:        "taproot",
		Slug:        "taproot",
		Description: "Dotfiles, containers, and the configs that make a machine mine.",
		Tech:        []string{"Docker", "Shell"},
	},

	// Archived, newest first, and named exactly as the repository is named on
	// GitHub, so a card title is something you can search for rather than a
	// friendly name that resolves to nothing.
	{
		Name:        "isaacbythewood.com-nextjs",
		Slug:        "isaacbythewood.com-nextjs",
		Description: "The Next.js build of this site: a custom cursor, a page transition loader, and full canvas background animations. Replaced by the Go rebuild you are reading now.",
		Tech:        []string{"Next.js", "React"},
		Archived:    true,
	},
	{
		Name:        "blog.bythewood.me-python",
		Slug:        "blog.bythewood.me-python",
		Description: "The last handcoded Python build of my blog. Markdown files on disk, no database, rolled back off its Rust rewrite.",
		Tech:        []string{"Python", "Flask"},
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
		Name:        "darkfurrow.com",
		Slug:        "darkfurrow.com",
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
		Name:        "timelite",
		Slug:        "timelite",
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
		Name:        "timestrap",
		Slug:        "timestrap",
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

// sourceURL points at the canvas module for this piece.
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
