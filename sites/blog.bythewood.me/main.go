// blog.bythewood.me: a Markdown blog with no database. Posts are files under
// content/posts, parsed once at startup and served from memory.
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

// Templates are source, so they always ship inside the binary.
//
//go:embed templates
var templateFS embed.FS

// The posts too, which is what makes the release binary the whole blog. `all:`
// keeps dot-prefixed files under content/images, which embed would skip.
//
//go:embed all:content
var contentFS embed.FS

const listenAddr = ":8000"

// Paths resolve against the working directory in dev, and are absolute in the
// image.
func dir(env, fallback string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return fallback
}

// 'unsafe-inline' is needed in script-src for the inline analytics snippet and
// in style-src for Bootstrap's style attributes. Neither can drop it without a
// markup change.
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
	web.SetupLogging()

	// The build-time modes live on the site binary so the post loader and the
	// Typst walker have one copy.
	pdfsOut := flag.String("pdfs", "", "compile every post to a PDF in this directory, then exit")
	ogOut := flag.String("og", "", "compile every social card to a PNG in this directory")
	typstRoot := flag.String("typst-root", ".", "directory Typst resolves absolute paths against")
	typstFonts := flag.String("typst-fonts", "", "directory of extra font files for Typst")
	// The container HEALTHCHECK runs this: a FROM scratch image has no shell
	// for a check to call, so the binary probes itself.
	healthcheck := flag.Bool("healthcheck", false, "probe a running server on this host and exit")
	flag.Parse()

	if *healthcheck {
		if err := web.HealthCheck("http://127.0.0.1:8000/healthz", 3*time.Second); err != nil {
			slog.Info(fmt.Sprintf("healthcheck: %v", err))
			os.Exit(1)
		}
		return
	}

	// Tees stdout records to logging.bythewood.me; see web/shipper.go. It goes
	// after the healthcheck branch so a HEALTHCHECK does not start a queue it
	// will never flush.
	shipper := web.ShipLogs("blog", web.HTTPSink())
	defer shipper.Close()

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

	// Either mode ends the process: this binary serves the site or compiles
	// assets, never both.
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

	// The export forms are one {slug}/{format} pattern because "/blog/{slug}/pdf/"
	// and "/blog/tag/{tag}/" overlap with neither more specific, which the mux
	// panics on at registration.
	mux.HandleFunc("GET /blog/{slug}/{$}", s.redirectPost)
	mux.HandleFunc("GET /blog/{slug}/{format}/{$}", s.redirectPostFormat)

	// Every route above ends in a slash; these cover the slashless forms.
	mux.HandleFunc("GET /blog", redirectSlash)
	mux.HandleFunc("GET /search", redirectSlash)

	mux.HandleFunc("GET /search/{$}", s.search)
	mux.HandleFunc("GET /search/live/{$}", s.searchLive)

	// Read by isaacbythewood.com's home page for its promo slot.
	mux.HandleFunc("GET /latest.json", s.latestJSON)

	// Cards are compiled at build time, and change only with a post's title
	// or tags.
	mux.Handle("GET /og/", http.StripPrefix("/og/",
		cacheControl("public, max-age=86400", http.FileServer(http.FS(s.og)))))
	mux.HandleFunc("GET /favicon.ico", favicon)
	mux.HandleFunc("GET /favicon.svg", favicon)
	mux.HandleFunc("GET /robots.txt", robots)
	mux.HandleFunc("GET /sitemap.xml", s.sitemap)
	mux.HandleFunc("GET "+feedPath, s.feed)
	// The two paths readers guess at.
	mux.HandleFunc("GET /feed", redirectFeed)
	mux.HandleFunc("GET /rss.xml", redirectFeed)

	mux.Handle("GET /static/", web.Static(dist, assets))

	// Post images keep their real filenames, so replacing one has to become
	// visible without a hash change; no immutable year here.
	contentImages, err := fs.Sub(content, "images")
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
	images := http.StripPrefix("/content/images/",
		cacheControl("public, max-age=86400",
			http.FileServer(http.FS(contentImages))))
	mux.Handle("GET /content/images/", images)

	// Not logged: a line per probe would bury real traffic.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})

	// "/" with no {$} is Go's catch-all.
	mux.HandleFunc("GET /", s.notFound)

	handler := web.Chain(mux,
		web.Recovered,
		web.Logged,
		web.SecurityHeaders(csp()),
		// No s-maxage: per RFC 9111 it carries proxy-revalidate semantics, so
		// Cloudflare disables stale-while-revalidate and stale-if-error both.
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
