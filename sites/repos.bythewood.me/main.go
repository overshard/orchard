// repos.bythewood.me is a git remote with a browse UI on top: push to it over
// HTTPS with a token, or let it mirror the GitHub account. Everything git is a
// subprocess; see git.go for the rules that keep shelling out safe.
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

// csp allows 'unsafe-inline' in style-src because chroma emits inline style
// attributes, https: in img-src for README badges, and analytics.bythewood.me
// plus inline script for the collector loader in base.html.
func csp() string {
	return strings.Join([]string{
		"default-src 'self'",
		"script-src 'self' 'unsafe-inline' https://analytics.bythewood.me",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data: https:",
		"font-src 'self'",
		"connect-src 'self' https://analytics.bythewood.me",
		"base-uri 'self'",
		"form-action 'self'",
		"frame-ancestors 'self'",
	}, "; ")
}

// The template sets, shared with the tests, which parse them for real. A page
// listed here with no file behind it parses at boot and not at build, so
// nothing but doing it catches a template that was deleted and left listed.
var (
	layoutTemplates = []string{"base.html", "partials.html", "pushhelp.html"}
	pageTemplates   = []string{
		"index.html", "repo.html", "tree.html", "blob.html",
		"log.html", "commit.html", "branches.html", "tags.html",
		"settings.html", "notfound.html",
	}
)

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

	// A missing http-backend is not fatal; the browse half still works.
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

	renderer, err := web.NewRenderer(templates, templateFuncs, layoutTemplates, pageTemplates)
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
		auth:     web.NewAuthenticator(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Seeded once; after that the mirror list is edited on the settings page.
	if err := db.SeedMirrorSources(githubUser); err != nil {
		slog.Error("seed mirror sources", slog.Any("err", err))
	}
	mirror := NewMirror(store, db)
	s.mirror = mirror
	if cfg.MirrorEnabled {
		go mirror.Run(ctx, cfg.MirrorEvery)
	} else {
		slog.Info("mirror lane disabled")
	}
	// receive.autogc is false on every repository here, so a push never repacks
	// while Cloudflare counts to 100. This lane is where that work went.
	go RunGC(ctx, store, cfg.GCEvery)

	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", s.index)

	// Signing in happens on auth.bythewood.me. This stays so an old bookmark
	// and every "sign in" link in the templates land somewhere useful.
	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, web.LoginURL(r), http.StatusSeeOther)
	})

	mux.HandleFunc("GET /settings", s.requireLogin(s.settings))
	mux.HandleFunc("POST /settings/tokens", s.requireLogin(s.createToken))
	mux.HandleFunc("POST /settings/tokens/{id}/revoke", s.requireLogin(s.revokeToken))
	mux.HandleFunc("POST /settings/mirrors", s.requireLogin(s.addMirrorSource))
	mux.HandleFunc("POST /settings/mirrors/{id}/delete", s.requireLogin(s.deleteMirrorSource))
	mux.HandleFunc("POST /settings/mirrors/sync", s.requireLogin(s.syncMirrors))

	// ServeMux matches most specific first, so registration order does not matter.
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
	// The format rides as a file extension so a browser and a shell both get a
	// usable filename.
	mux.HandleFunc("GET /{name}/archive/{rev}", s.archive)
	mux.HandleFunc("GET /{name}/atom.xml", s.atom)

	mux.HandleFunc("GET /favicon.ico", favicon)
	mux.HandleFunc("GET /favicon.svg", favicon)
	mux.HandleFunc("GET /robots.txt", robots)

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		// EdgeCache fills in the site policy whenever a handler sets no
		// Cache-Control of its own, so saying nothing here means the edge
		// answers a liveness check out of cache long after this process has
		// stopped serving.
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte("ok\n"))
	})

	mux.HandleFunc("GET /", s.notFound)

	wr := &wire{store: store, db: db, backend: backend, allowCreate: true}

	// Neither the wire nor /static/ can be a mux pattern: a wildcard matches a
	// whole segment so "{name}.git" is illegal, and "GET /static/" conflicts
	// with "GET /{name}/tree/{rev}/{path...}" and panics at registration.
	handler := web.Chain(staticRouter(dist, assets, wr.Router(mux)),
		web.Recovered,
		web.Logged,
		web.SecurityHeaders(csp()),
		// Browse pages branch on LoggedIn and the logged-in half names internal
		// container topology, so an operator response must never be shared-cacheable.
		privateWhenSignedIn,
		// Short, because a repository page changes the moment something is pushed.
		// The wire is exempt: git's responses carry no-cache and EdgeCache only
		// fills in a policy where a handler chose none.
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

// privateWhenSignedIn stamps no-store on any response to a request carrying a
// session cookie, so EdgeCache leaves it alone.
//
// It tests for the cookie rather than for a live session, which is both cheaper
// and safer: whether the cookie is still valid is a question for auth, and a
// response to somebody holding an expired one still must not be shared cached.
func privateWhenSignedIn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(web.SessionCookie); err == nil && c.Value != "" {
			w.Header().Set("Cache-Control", "private, no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// staticRouter claims /static/ before the browse mux can see it.
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
	// A crawler walking every blob at every revision is one git subprocess per
	// request, and a repository page already costs about eleven. Every route
	// below the repository name carries the name as its first segment, so these
	// need the wildcard: a bare "/raw/" matches nothing this site serves.
	//
	// tree and blob stay open, since the browse UI is the point of the site.
	// raw, archive, commit and log are the expensive ones nobody searches for.
	_, _ = fmt.Fprint(w, "User-agent: *\n"+
		"Disallow: /*/raw/\n"+
		"Disallow: /*/archive/\n"+
		"Disallow: /*/commit/\n"+
		"Disallow: /*/log\n"+
		"Disallow: /settings\n"+
		"Disallow: /login\n"+
		"Crawl-delay: 10\n")
}

func favicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write([]byte(faviconSVG))
}

const faviconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">` +
	`<rect width="32" height="32" rx="7" fill="#17151a"/>` +
	`<circle cx="16" cy="16" r="7" fill="#a99bf5"/></svg>`
