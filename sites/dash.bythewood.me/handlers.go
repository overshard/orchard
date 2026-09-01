package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"time"

	"dash.bythewood.me/web"
)

// PageData is what base.html and home.html read.
type PageData struct {
	Title       string
	Description string
	Canonical   string
	Staging     bool
	Year        int
	BaseURL     string
	SourceURL   string
	SiteName    string
	AuthorName  string
	Script      string
	Styles      []string
	Favicon     string
	Analytics   bool
	AnalyticsID string

	// Every upstream the page reads, for the footer. Built from the same list
	// the UPLINK panel is built from, since the hand written version of this
	// named five of the thirteen and there was nothing to notice the other
	// eight arriving.
	Sources []string

	// The tab's own wording, handed to the page so the script that keeps the
	// title live does not carry a second copy of it.
	TitleBase string

	State State
}

func (s *site) home(w http.ResponseWriter, r *http.Request) {
	data := PageData{
		Title:       "",
		Description: "Markets, news and the state of everything " + authorName + " runs, on one page.",
		Canonical:   baseURL + "/",
		Staging:     Staging,
		Year:        time.Now().Year(),
		BaseURL:     baseURL,
		SourceURL:   sourceURL,
		SiteName:    siteName,
		AuthorName:  authorName,
		Script:      s.script,
		Styles:      s.styles,
		Favicon:     faviconHref,
		Analytics:   !Staging,
		AnalyticsID: analyticsID,
		Sources:     sourceNames(),
		TitleBase:   titleBase,
		State:       s.store.Snapshot(),
	}
	s.renderer.Render(w, http.StatusOK, "home.html", data)
}

// state is the same snapshot the page was rendered from, for anything that
// wants to read it without holding an SSE connection open.
func (s *site) state(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(s.store.Snapshot())
}

// events is the live feed. The browser opens one EventSource and every poll
// that changes anything arrives here, so the page never polls this server and
// this server never polls an upstream per viewer.
func (s *site) events(w http.ResponseWriter, r *http.Request) {
	// ResponseController rather than a type assertion for http.Flusher, because
	// the request logger wraps the writer and an assertion would see the
	// wrapper. It follows Unwrap down to the real one.
	rc := http.NewResponseController(w)

	// web/server.go sets no write bound for this site, and this clears any
	// per-connection deadline anyway, so a stream is never cut mid-frame. It
	// doubles as the check that this writer can be flushed at all.
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	// Caddy is configured not to compress this path and Cloudflare streams it,
	// but a proxy that reads this header is one that would otherwise sit on the
	// frames until its buffer filled.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_ = rc.Flush()

	frames, unsubscribe := s.hub.Subscribe()
	defer unsubscribe()

	// Cloudflare drops an idle connection at 100 seconds, so a comment goes out
	// well inside that whenever there is nothing else to send. The browser
	// ignores it and the connection stays open.
	keepalive := time.NewTicker(25 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return

		case frame, open := <-frames:
			if !open {
				return
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", frame); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}

		case <-keepalive.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
		}
	}
}

// healthz stays shallow: this process is serving or it is not, and a failing
// upstream is not fixed by the restart a failing check would cause. The panels
// say so on the page instead.
func (s *site) healthz(w http.ResponseWriter, r *http.Request) {
	if !r.URL.Query().Has("verbose") || !isLoopback(web.ClientIP(r)) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		// EdgeCache fills in the site policy whenever a handler sets no
		// Cache-Control of its own, so saying nothing here means the edge
		// answers a liveness check out of cache long after this process has
		// stopped serving.
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte("ok\n"))
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]any{
		"watching": s.hub.Watching(),
		"guarded":  s.guard.Status(),
		"updated":  s.store.Snapshot().Updated,
	})
}

func isLoopback(ip string) bool {
	addr, err := netip.ParseAddr(ip)
	return err == nil && addr.IsLoopback()
}

func (s *site) notFound(w http.ResponseWriter, r *http.Request) {
	data := PageData{
		Title:       "404",
		Description: "That page does not exist.",
		Canonical:   baseURL + r.URL.Path,
		Staging:     Staging,
		Year:        time.Now().Year(),
		BaseURL:     baseURL,
		SourceURL:   sourceURL,
		SiteName:    siteName,
		AuthorName:  authorName,
		Script:      s.script,
		Styles:      s.styles,
		Favicon:     faviconHref,
		Analytics:   !Staging,
		AnalyticsID: analyticsID,
		Sources:     sourceNames(),
		TitleBase:   titleBase,
	}
	s.renderer.Render(w, http.StatusNotFound, "notfound.html", data)
}

func robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	body := "User-agent: *\nDisallow: /\n"
	if !Staging {
		// /events is a connection a crawler would hold open until it timed out,
		// and /api/state is the same page as JSON.
		body = "User-agent: *\nDisallow: /events\nDisallow: /api/\nAllow: /\n" +
			"Sitemap: " + baseURL + "/sitemap.xml\n"
	}
	_, _ = w.Write([]byte(body))
}

func sitemap(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>%s/</loc></url>
</urlset>
`, baseURL)
}

// faviconHref carries the content hash, so replacing the icon replaces the URL.
var faviconHref = "/favicon.svg?v=" + faviconVersion

// sourceNames is the footer's credit line. Reading it off feedOrder is what
// keeps it honest, since the two are the same list and a source added to one
// has to appear in the other.
func sourceNames() []string {
	out := make([]string, 0, len(feedOrder))
	for _, f := range feedOrder {
		// Isaac's own logging site, which is not an outside source to credit.
		if f.key == "logging" {
			continue
		}
		out = append(out, f.label)
	}
	return out
}
