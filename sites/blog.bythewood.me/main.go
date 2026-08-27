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
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"bythewood.me/orchard/internal/web"
)

// Templates are source, so they ship inside the binary. dist/ and pdfs/ are
// not: they are build artifacts Docker copies alongside it, which keeps
// `go build ./...` working on a fresh clone before anyone has run Vite.
//
//go:embed templates
var templateFS embed.FS

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
	log.SetFlags(log.LstdFlags | log.LUTC)

	// -pdfs is the build-time mode: compile every post to PDF and exit. It
	// lives on the site binary rather than in a cmd/ of its own so the post
	// loader and the Typst walker have exactly one copy.
	pdfsOut := flag.String("pdfs", "", "compile every post to a PDF in this directory, then exit")
	typstRoot := flag.String("typst-root", ".", "directory Typst resolves absolute paths against")
	flag.Parse()

	contentDir := dir("SITE_CONTENT", "content")

	lib, err := LoadLibrary(contentDir)
	if err != nil {
		log.Fatalf("load posts: %v", err)
	}
	log.Printf("loaded %d posts from %s", len(lib.All()), contentDir)

	if *pdfsOut != "" {
		if err := GeneratePDFs(lib, *typstRoot, *pdfsOut); err != nil {
			log.Fatalf("generate pdfs: %v", err)
		}
		return
	}

	distDir := dir("SITE_DIST", "dist")
	dist := os.DirFS(distDir)

	assets, err := web.LoadAssets(dist)
	if err != nil {
		log.Fatal(err)
	}

	templates, err := fs.Sub(templateFS, "templates")
	if err != nil {
		log.Fatal(err)
	}

	renderer, err := web.NewRenderer(
		templates,
		templateFuncs,
		[]string{"base.html", "partials.html"},
		[]string{"home.html", "blog.html", "post.html", "search.html", "notfound.html"},
	)
	if err != nil {
		log.Fatal(err)
	}

	s := &site{
		renderer: renderer,
		lib:      lib,
		contentD: contentDir,
		pdfDir:   dir("SITE_PDFS", "pdfs"),
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

	mux.HandleFunc("GET /og/{name}", s.ogImage)
	mux.HandleFunc("GET /favicon.ico", favicon)
	mux.HandleFunc("GET /favicon.svg", favicon)
	mux.HandleFunc("GET /robots.txt", robots)
	mux.HandleFunc("GET /sitemap.xml", s.sitemap)

	mux.Handle("GET /static/", web.Static(dist, assets))

	// Post images keep their real filenames, so they get an hour rather than
	// the immutable year the hashed bundles get. Replacing an image has to be
	// visible before 2027.
	images := http.StripPrefix("/content/images/",
		cacheControl("public, max-age=86400",
			http.FileServer(http.Dir(filepath.Join(contentDir, "images")))))
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
	)

	log.Printf("blog.bythewood.me serving %s (staging=%t)", baseURL, Staging)
	if err := web.Serve(listenAddr, handler); err != nil {
		log.Fatal(err)
	}
}

func cacheControl(value string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", value)
		next.ServeHTTP(w, r)
	})
}
