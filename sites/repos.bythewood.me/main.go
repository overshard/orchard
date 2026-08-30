// repos.bythewood.me: a git remote with a browser on top.
//
// Two ways in. Push to it over HTTPS and it is a real remote, authenticated by
// a token; that is what the site is for. Or let it mirror the GitHub account,
// which is a backup for the repositories that exist only there. One browse UI
// over both.
//
// Everything git is a subprocess. `git http-backend` carries the wire, and
// plumbing commands answer every page. See git.go for why, and for the rules
// that make shelling out safe rather than merely possible.
package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"repos.bythewood.me/web"
)

//go:embed templates
var templateFS embed.FS

const listenAddr = ":8000"

// Content-Security-Policy.
//
// 'unsafe-inline' is in style-src because chroma emits inline style attributes
// on every highlighted span, and turning that off means switching chroma to
// class-based output and shipping a stylesheet per theme. img-src allows https:
// because READMEs are full of badge images from shields.io and friends, and a
// README that renders with broken images is worse than one that loads a badge.
func csp() string {
	return strings.Join([]string{
		"default-src 'self'",
		"script-src 'self'",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data: https:",
		"font-src 'self'",
		"connect-src 'self'",
		"base-uri 'self'",
		"form-action 'self'",
		"frame-ancestors 'self'",
	}, "; ")
}

func main() {
	web.SetupLogging()

	healthcheck := flag.Bool("healthcheck", false, "probe a running server on this host and exit")
	flag.Parse()

	if *healthcheck {
		if err := web.HealthCheck("http://127.0.0.1:8000/healthz", 3*time.Second); err != nil {
			slog.Info(fmt.Sprintf("healthcheck: %v", err))
			os.Exit(1)
		}
		return
	}

	shipper := web.ShipLogs("repos", web.HTTPSink())
	defer shipper.Close()

	cfg := LoadConfig()
	if cfg.Password == "" {
		slog.Error("REPOS_PASSWORD is not set; refusing to start with an unguessable-by-luck UI")
		os.Exit(1)
	}
	if err := os.MkdirAll(cfg.RepoRoot, 0o755); err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}

	db, err := OpenDB(cfg.DataDir)
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
	defer db.Close()

	store := NewStore(cfg.RepoRoot)
	defer store.Close()

	// Resolved once. A missing http-backend is not fatal, because the browse
	// half of the site still works without it and a site that refuses to
	// start is worse than one that cannot be cloned from.
	backend := gitHTTPBackend()
	if backend == "" {
		slog.Warn("git-http-backend not found; clone and push are unavailable")
	}

	dist := distFS()
	assets, err := web.LoadAssets(dist)
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}

	templates, err := fs.Sub(templateFS, "templates")
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}

	renderer, err := web.NewRenderer(
		templates,
		templateFuncs,
		[]string{"base.html", "partials.html", "pushhelp.html"},
		[]string{
			"index.html", "repo.html", "tree.html", "blob.html",
			"log.html", "commit.html", "branches.html", "tags.html",
			"login.html", "settings.html", "notfound.html",
		},
	)
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}

	s := &site{
		renderer: renderer,
		store:    store,
		db:       db,
		cfg:      cfg,
		backend:  backend,
		script:   assets.Script("index.js"),
		styles:   assets.Styles("index.js"),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The background lanes. Both are tickers in this process rather than
	// sidecar containers: it keeps the site to one image, and lets the index
	// render its own sync state, which a sidecar cannot do without a side
	// channel.
	if cfg.MirrorEnabled {
		go NewMirror(store, db, githubUser).Run(ctx, cfg.MirrorEvery)
	} else {
		slog.Info("mirror lane disabled")
	}
	// receive.autogc is false on every repository here, so a push never
	// repacks while Cloudflare counts to 100. This is where that work went.
	go RunGC(ctx, store, cfg.GCEvery)

	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", s.index)

	mux.HandleFunc("GET /login", s.login)
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("POST /logout", s.logout)

	mux.HandleFunc("GET /settings", s.requireLogin(s.settings))
	mux.HandleFunc("POST /settings/tokens", s.requireLogin(s.createToken))
	mux.HandleFunc("POST /settings/tokens/{id}/revoke", s.requireLogin(s.revokeToken))

	// The repository pages. Every one of these is more specific than the
	// bare /{name} below it, and Go 1.22 ServeMux matches most specific
	// first, so registration order does not matter. That is the footgun the
	// Rust build's router merge had and this does not.
	mux.HandleFunc("GET /{name}", s.repo)
	mux.HandleFunc("POST /{name}/edit", s.requireLogin(s.editRepo))

	mux.HandleFunc("GET /{name}/tree/{rev}", s.tree)
	mux.HandleFunc("GET /{name}/tree/{rev}/{path...}", s.tree)
	mux.HandleFunc("GET /{name}/blob/{rev}/{path...}", s.blob)
	mux.HandleFunc("GET /{name}/raw/{rev}/{path...}", s.raw)

	mux.HandleFunc("GET /{name}/log", s.log)
	mux.HandleFunc("GET /{name}/log/{rev}", s.log)
	mux.HandleFunc("GET /{name}/log/{rev}/{path...}", s.log)

	mux.HandleFunc("GET /{name}/commit/{sha}", s.commit)
	mux.HandleFunc("GET /{name}/branches", s.refsPage("branches.html"))
	mux.HandleFunc("GET /{name}/tags", s.refsPage("tags.html"))
	// The archive URL carries its format as a file extension, so a browser
	// and a shell both get a filename they can use.
	mux.HandleFunc("GET /{name}/archive/{rev}", s.archive)
	mux.HandleFunc("GET /{name}/atom.xml", s.atom)

	mux.HandleFunc("GET /favicon.ico", favicon)
	mux.HandleFunc("GET /favicon.svg", favicon)
	mux.HandleFunc("GET /robots.txt", robots)

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})

	mux.HandleFunc("GET /", s.notFound)

	wr := &wire{store: store, db: db, backend: backend, allowCreate: true}

	// Two routers sit in front of the browse mux, both for the same reason:
	// a repository is addressed as /<name>, so the first path segment is a
	// namespace shared with everything else this site serves.
	//
	// The wire cannot be a mux pattern at all, because a ServeMux wildcard
	// matches a whole segment and "{name}.git" is not legal.
	//
	// /static/ cannot either, but for a subtler reason worth writing down:
	// ServeMux rejects "GET /static/" and "GET /{name}/tree/{rev}/{path...}"
	// as conflicting at registration, because both match /static/tree/rev/
	// and neither pattern is more specific than the other. It panics on
	// startup rather than picking one, which is the good version of the
	// footgun the Rust build had. Claiming the prefix ahead of the mux is
	// the fix, and it also makes "static" a name no repository can take.
	handler := web.Chain(staticRouter(dist, assets, wr.Router(mux)),
		web.Recovered,
		web.Logged,
		web.SecurityHeaders(csp()),
		// A logged-in response is never shared-cacheable. Every browse page
		// branches on LoggedIn, and the partials behind it carry the push
		// guidance, which names internal container topology deliberately kept
		// off the anonymous page. EdgeCache below would otherwise stamp
		// "public, max-age=60, stale-if-error=86400" on the operator's copy,
		// so a shared cache holding HTML could serve it to a stranger for a
		// day. /login and /settings already set no-store themselves; this
		// covers the rest without a per-handler edit.
		privateWhenLoggedIn(cfg.Password),
		// Short, because a repository page changes the moment something is
		// pushed and a stale clone URL page is a page that lies about what
		// is in the repository.
		//
		// The wire is exempt: git's own responses carry no-cache and
		// EdgeCache only fills in a policy where a handler chose none.
		web.EdgeCache("public, max-age=60, "+
			"stale-while-revalidate=600, stale-if-error=86400"),
	)

	slog.Info(fmt.Sprintf("repos.bythewood.me serving %s (staging=%t, mirror=%t)",
		baseURL, Staging, cfg.MirrorEnabled))
	if err := web.Serve(listenAddr, handler); err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
}

// privateWhenLoggedIn stamps no-store on any response to a request carrying a
// valid session, so EdgeCache leaves it alone. Vary: Cookie is the alternative
// and is worse here: it would split the cache for every visitor carrying any
// cookie at all, which is most of them, and lose the edge caching the anonymous
// pages actually want.
func privateWhenLoggedIn(password string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if validSession(r, password) {
				w.Header().Set("Cache-Control", "private, no-store")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// staticRouter claims /static/ before the browse mux can see it. See the
// comment at the Chain call for why this is not a mux pattern.
func staticRouter(dist fs.FS, assets *web.Assets, next http.Handler) http.Handler {
	static := web.Static(dist, assets)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/static/") {
			static.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if Staging {
		_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
		return
	}
	// The wire and the archive endpoints are not pages, and a crawler that
	// walks every blob at every revision is a crawler generating a git
	// subprocess per request.
	_, _ = fmt.Fprintf(w, "User-agent: *\n"+
		"Disallow: /raw/\n"+
		"Disallow: /archive/\n"+
		"Disallow: /settings\n"+
		"Disallow: /login\n"+
		"Crawl-delay: 10\n\n"+
		"Sitemap: %s/robots.txt\n", baseURL)
}

func favicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write([]byte(faviconSVG))
}

// The same single node the header and footer draw, so the tab icon and the
// wordmark are one mark rather than two. Inline rather than a file, because it
// is under 200 bytes and a file would be a build step.
const faviconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">` +
	`<rect width="32" height="32" rx="7" fill="#17151a"/>` +
	`<circle cx="16" cy="16" r="7" fill="#a99bf5"/></svg>`
