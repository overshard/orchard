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

// The posts ship inside the binary too, so the release binary is the whole
// blog. `all:` keeps dot-prefixed files under content/images, which embed skips.
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
	if err := realMain(); err != nil {
		slog.Error("exiting", slog.Any("err", err))
		os.Exit(1)
	}
}

// realMain returns instead of exiting so the deferred closes and the log
// shipper's flush run on every path out.
func realMain() error {
	web.SetupLogging()

	// The build-time modes live on the site binary so the post loader and the
	// Typst walker have one copy.
	pdfsOut := flag.String("pdfs", "", "compile every post to a PDF in this directory, then exit")
	ogOut := flag.String("og", "", "compile every social card to a PNG in this directory")
	typstRoot := flag.String("typst-root", ".", "directory Typst resolves absolute paths against")
	typstFonts := flag.String("typst-fonts", "", "directory of extra font files for Typst")
	// The container HEALTHCHECK runs this since a FROM scratch image has no
	// shell for a check to call, so the binary probes itself.
	healthcheck := flag.Bool("healthcheck", false, "probe a running server on this host and exit")
	flag.Parse()

	if *healthcheck {
		if err := web.HealthCheck("http://127.0.0.1:8000/healthz", 3*time.Second); err != nil {
			return fmt.Errorf("healthcheck: %w", err)
		}
		return nil
	}

	// This goes after the healthcheck branch so a HEALTHCHECK doesn't start a
	// queue it will never flush.
	shipper := web.ShipLogs("blog", web.HTTPSink())
	defer shipper.Close()

	content, err := fs.Sub(contentFS, "content")
	if err != nil {
		return err
	}

	lib, err := LoadLibrary(content)
	if err != nil {
		return fmt.Errorf("load posts: %w", err)
	}
	slog.Info(fmt.Sprintf("loaded %d posts", len(lib.All())))

	if *pdfsOut != "" || *ogOut != "" {
		if *pdfsOut != "" {
			if err := GeneratePDFs(lib, *typstRoot, *typstFonts, *pdfsOut); err != nil {
				return fmt.Errorf("generate pdfs: %w", err)
			}
		}
		if *ogOut != "" {
			if err := GenerateOGCards(lib, *typstRoot, *typstFonts, *ogOut); err != nil {
				return fmt.Errorf("generate og cards: %w", err)
			}
		}
		return nil
	}

	dist := distFS()

	assets, err := web.LoadAssets(dist)
	if err != nil {
		return err
	}

	templates, err := fs.Sub(templateFS, "templates")
	if err != nil {
		return err
	}

	renderer, err := web.NewRenderer(
		templates,
		templateFuncs,
		[]string{"base.html", "partials.html"},
		[]string{"home.html", "blog.html", "post.html", "search.html", "notfound.html"},
	)
	if err != nil {
		return err
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

	// Every route above ends in a slash, these cover the slashless forms.
	mux.HandleFunc("GET /blog", redirectSlash)
	mux.HandleFunc("GET /search", redirectSlash)

	mux.HandleFunc("GET /search/{$}", s.search)
	mux.HandleFunc("GET /search/live/{$}", s.searchLive)

	// Read by isaacbythewood.com's home page for its promo slot.
	mux.HandleFunc("GET /latest.json", s.latestJSON)

	// Cards are compiled at build time, and change only with a post's title
	// or tags.
	mux.Handle("GET /og/", http.StripPrefix("/og/", s.ogPublished(ogLegacy(s.og,
		cacheControl("public, max-age=86400", http.FileServer(http.FS(s.og)))))))
	mux.HandleFunc("GET /favicon.ico", favicon)
	mux.HandleFunc("GET /favicon.svg", favicon)
	mux.HandleFunc("GET /robots.txt", robots)
	mux.HandleFunc("GET /sitemap.xml", s.sitemap)
	mux.HandleFunc("GET "+feedPath, s.feed)
	// Paths readers guess at for the feed.
	mux.HandleFunc("GET /feed", redirectFeed)
	mux.HandleFunc("GET /rss.xml", redirectFeed)

	mux.Handle("GET /static/", web.Static(dist, assets))

	// Post images keep their real filenames, so a replaced one has to show up
	// without a hash change and can't be cached immutable for a year.
	contentImages, err := fs.Sub(content, "images")
	if err != nil {
		return err
	}
	images := http.StripPrefix("/content/images/",
		cacheControl("public, max-age=86400",
			web.NoDirs(http.FileServer(http.FS(contentImages)))))
	mux.Handle("GET /content/images/", images)

	// Wagtail rendition URLs, still arriving from feeds and search indexes.
	s.mediaIdx = newMediaIndex(contentImages)
	mux.HandleFunc("GET /media/", s.media)

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		// EdgeCache fills in the site policy when a handler sets no
		// Cache-Control, and then the edge would answer a liveness check out of
		// cache long after this process stopped serving.
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte("ok\n"))
	})

	// "/" with no {$} is Go's catch-all.
	mux.HandleFunc("GET /", s.notFound)

	handler := web.Chain(mux,
		web.Recovered,
		web.Logged,
		web.SecurityHeaders(csp()),
		web.SameOrigin(),
		// No s-maxage: per RFC 9111 it carries proxy-revalidate semantics, so
		// Cloudflare disables stale-while-revalidate and stale-if-error both.
		web.EdgeCache("public, max-age=300, "+
			"stale-while-revalidate=86400, stale-if-error=604800"),
	)

	slog.Info(fmt.Sprintf("blog.bythewood.me serving %s (staging=%t)", baseURL, Staging))
	return web.Serve(listenAddr, handler)
}

func cacheControl(value string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", value)
		next.ServeHTTP(w, r)
	})
}
