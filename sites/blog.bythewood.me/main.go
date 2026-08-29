// blog.bythewood.me, rebuilt from Rust onto Go.
//
// A Markdown blog with no database: posts are files under content/posts,
// parsed once at startup and served from memory. Second site through the
// cloudflared -> Caddy -> Go path, after isaacbythewood.com, and the second
// step of decisions/0008's move off Rust.
//
// Nothing here is configurable and nothing here is secret. The identity is
// hardcoded in site.go on purpose; see decisions/0008. The Rust version read
// PORT and BLOG_ROOT out of a .env, which is exactly the scaffolding that
// decision deletes.
package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"blog.bythewood.me/web"
)

// Templates are source, so they ship inside the binary.
//
//go:embed templates
var templateFS embed.FS

// So are the posts. They are this site's data, they are committed, and they
// are already parsed once at startup and never re-read, so embedding them
// costs nothing a dev notices and is what lets the release binary be the whole
// blog rather than a binary plus a directory of Markdown.
//
// `all:` keeps content/images intact: it holds files embed would otherwise
// skip for starting with a dot.
//
//go:embed all:content
var contentFS embed.FS

const listenAddr = ":8000"

// dirs resolve relative to the working directory in dev and are set to
// absolute paths in the image.
func dir(env, fallback string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return fallback
}

// Content-Security-Policy.
//
// 'unsafe-inline' survives in both script-src and style-src, and both are load
// bearing rather than left over: the analytics collector is a literal inline
// script, and the Bootstrap templates carry inline style attributes on a dozen
// elements. Removing either means changing the markup, which is a separate job
// from porting it.
func csp() string {
	return strings.Join([]string{
		"default-src 'self'",
		"script-src 'self' 'unsafe-inline' https://analytics.bythewood.me",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data:",
		"font-src 'self'",
		"connect-src 'self' https://analytics.bythewood.me",
		"base-uri 'self'",
		"form-action 'self'",
		"frame-ancestors 'self'",
	}, "; ")
}

func main() {
	// JSON to stdout, installed before anything else can log.
	web.SetupLogging()

	// -pdfs is the build-time mode: compile every post to PDF and exit. It
	// lives on the site binary rather than in a cmd/ of its own so the post
	// loader and the Typst walker have exactly one copy.
	pdfsOut := flag.String("pdfs", "", "compile every post to a PDF in this directory, then exit")
	ogOut := flag.String("og", "", "compile every social card to a PNG in this directory")
	typstRoot := flag.String("typst-root", ".", "directory Typst resolves absolute paths against")
	typstFonts := flag.String("typst-fonts", "", "directory of extra font files for Typst")
	// -healthcheck turns the binary into its own health probe and exits. The
	// container HEALTHCHECK runs this: two of these images are FROM scratch and
	// have no shell or curl for a check to shell out to, so the binary has to
	// be the thing that probes.
	healthcheck := flag.Bool("healthcheck", false, "probe a running server on this host and exit")
	flag.Parse()

	if *healthcheck {
		if err := web.HealthCheck("http://127.0.0.1:8000/healthz", 3*time.Second); err != nil {
			slog.Info(fmt.Sprintf("healthcheck: %v", err))
			os.Exit(1)
		}
		return
	}

	content, err := fs.Sub(contentFS, "content")
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}

	lib, err := LoadLibrary(content)
	if err != nil {
		slog.Error(fmt.Sprintf("load posts: %v", err))
		os.Exit(1)
	}
	slog.Info(fmt.Sprintf("loaded %d posts", len(lib.All())))

	// The build-time modes. Both take the same Typst root and font path, and
	// either one on its own ends the process: this binary is the site server
	// or it is the asset compiler, never both at once.
	if *pdfsOut != "" || *ogOut != "" {
		if *pdfsOut != "" {
			if err := GeneratePDFs(lib, *typstRoot, *typstFonts, *pdfsOut); err != nil {
				slog.Error(fmt.Sprintf("generate pdfs: %v", err))
				os.Exit(1)
			}
		}
		if *ogOut != "" {
			if err := GenerateOGCards(lib, *typstRoot, *typstFonts, *ogOut); err != nil {
				slog.Error(fmt.Sprintf("generate og cards: %v", err))
				os.Exit(1)
			}
		}
		return
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
		[]string{"base.html", "partials.html"},
		[]string{"home.html", "blog.html", "post.html", "search.html", "notfound.html"},
	)
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}

	s := &site{
		renderer: renderer,
		lib:      lib,
		content:  content,
		pdfs:     pdfsFS(),
		og:       ogFS(),
		script:   assets.Script("index.js"),
		styles:   assets.Styles("index.js"),
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /blog/{$}", s.blogIndex)
	mux.HandleFunc("GET /blog/tag/{tag}/{$}", s.blogByTag)
	mux.HandleFunc("GET /blog/year/{year}/{$}", s.blogByYear)

	mux.HandleFunc("GET /posts/{slug}/{$}", s.post)
	mux.HandleFunc("GET /posts/{slug}/pdf/{$}", s.postPDF)
	mux.HandleFunc("GET /posts/{slug}/md/{$}", s.postMarkdown)

	// The pre-2026 URLs, kept alive because one of the posts they serve is
	// about not breaking URLs.
	//
	// The export forms are one pattern rather than two, because
	// "GET /blog/{slug}/pdf/{$}" and "GET /blog/tag/{tag}/{$}" genuinely
	// overlap (on /blog/tag/pdf/) with neither more specific, and Go's mux
	// panics at registration rather than picking one. A single
	// {slug}/{format} pattern is strictly less specific than the literal
	// /blog/tag/ and /blog/year/ routes, so those keep winning and the
	// ambiguity is gone.
	mux.HandleFunc("GET /blog/{slug}/{$}", s.redirectPost)
	mux.HandleFunc("GET /blog/{slug}/{format}/{$}", s.redirectPostFormat)

	// Every route above ends in a slash, and the Rust router answered 404 for
	// the slashless form of all of them. These two cover the ones a person
	// actually types.
	mux.HandleFunc("GET /blog", redirectSlash)
	mux.HandleFunc("GET /search", redirectSlash)

	mux.HandleFunc("GET /search/{$}", s.search)
	mux.HandleFunc("GET /search/live/{$}", s.searchLive)

	// Read by isaacbythewood.com's home page for its promo slot. See
	// latestJSON for why it exists and why the shape is hand written.
	mux.HandleFunc("GET /latest.json", s.latestJSON)

	// Cards are compiled at build time, so this serves files. A day of cache
	// is plenty: a card only changes when its post's title or tags do, and the
	// scrapers that read it re-fetch on their own schedule anyway.
	mux.Handle("GET /og/", http.StripPrefix("/og/",
		cacheControl("public, max-age=86400", http.FileServer(http.FS(s.og)))))
	mux.HandleFunc("GET /favicon.ico", favicon)
	mux.HandleFunc("GET /favicon.svg", favicon)
	mux.HandleFunc("GET /robots.txt", robots)
	mux.HandleFunc("GET /sitemap.xml", s.sitemap)
	mux.HandleFunc("GET "+feedPath, s.feed)
	// The two paths readers guess at when a site does not advertise one.
	mux.HandleFunc("GET /feed", redirectFeed)
	mux.HandleFunc("GET /rss.xml", redirectFeed)

	mux.Handle("GET /static/", web.Static(dist, assets))

	// Post images keep their real filenames, so they get an hour rather than
	// the immutable year the hashed bundles get. Replacing an image has to be
	// visible before 2027.
	contentImages, err := fs.Sub(content, "images")
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
	images := http.StripPrefix("/content/images/",
		cacheControl("public, max-age=86400",
			http.FileServer(http.FS(contentImages))))
	mux.Handle("GET /content/images/", images)

	// Not logged and not behind the security headers: it exists for the
	// container health check, and a line per probe would bury real traffic.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})

	// Anything unmatched. "/" with no {$} is Go's catch-all.
	mux.HandleFunc("GET /", s.notFound)

	handler := web.Chain(mux,
		web.Recovered,
		web.Logged,
		web.SecurityHeaders(csp()),
		// Five minutes fresh, then a day of serving stale while revalidating
		// behind the request, then a week of serving stale if the origin is
		// down.
		//
		// Deliberately no s-maxage, which is the trap here: per RFC 9111 it
		// carries proxy-revalidate semantics, so Cloudflare treats it as
		// "never serve stale without asking me first" and it silently
		// disables stale-while-revalidate and stale-if-error both. Splitting
		// the browser and edge lifetimes is not worth losing the two
		// directives that are the whole reason this header exists.
		web.EdgeCache("public, max-age=300, "+
			"stale-while-revalidate=86400, stale-if-error=604800"),
	)

	slog.Info(fmt.Sprintf("blog.bythewood.me serving %s (staging=%t)", baseURL, Staging))
	if err := web.Serve(listenAddr, handler); err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
}

func cacheControl(value string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", value)
		next.ServeHTTP(w, r)
	})
}
