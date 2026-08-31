package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

// A mistake in the client IP rule is invisible: it does not error, it
// attributes every visitor to the wrong address. Pinned rather than reasoned
// about.
func TestClientIP(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		remote  string
		want    string
	}{
		{
			// Behind the tunnel, cloudflared sets XFF to the real client
			// and Caddy appends its own peer, so the last entry is a
			// bridge address on every request.
			name: "cloudflare wins over a trailing proxy hop",
			headers: map[string]string{
				"X-Forwarded-For":  "203.0.113.7, 172.18.0.4",
				"CF-Connecting-IP": "203.0.113.7",
			},
			remote: "172.18.0.5:41234",
			want:   "203.0.113.7",
		},
		{
			// Without Cloudflare the last entry is the hop that actually
			// connected; earlier entries are attacker controlled.
			name:    "falls back to the last forwarded entry",
			headers: map[string]string{"X-Forwarded-For": "203.0.113.7, 198.51.100.2"},
			remote:  "172.18.0.5:41234",
			want:    "198.51.100.2",
		},
		{
			name:    "bare connection uses the peer without its port",
			headers: nil,
			remote:  "198.51.100.9:51000",
			want:    "198.51.100.9",
		},
		{
			// A spoofed XFF does beat the real peer on a direct request.
			// Accepted, because Caddy is always in front in production.
			name:    "single forwarded entry is taken at face value",
			headers: map[string]string{"X-Forwarded-For": "203.0.113.7"},
			remote:  "172.18.0.5:41234",
			want:    "203.0.113.7",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tt.remote
			for k, v := range tt.headers {
				r.Header.Set(k, v)
			}
			if got := ClientIP(r); got != tt.want {
				t.Errorf("ClientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Caching the wrong file for a year cannot be undone from the server side:
// every browser that saw it holds it until the year is up.
func TestStaticCachePolicy(t *testing.T) {
	dist := fstest.MapFS{
		".vite/manifest.json": &fstest.MapFile{Data: []byte(`{
			"index.js": {"file": "base-AAAA.js", "src": "index.js", "isEntry": true,
			             "css": ["base-BBBB.css"]}
		}`)},
		"base-AAAA.js":  &fstest.MapFile{Data: []byte("//js")},
		"base-BBBB.css": &fstest.MapFile{Data: []byte("/*css*/")},
		"pdfs/cv.pdf":   &fstest.MapFile{Data: []byte("%PDF")},
		"images/a.webp": &fstest.MapFile{Data: []byte("webp")},
	}

	assets, err := LoadAssets(dist)
	if err != nil {
		t.Fatalf("LoadAssets: %v", err)
	}
	handler := Static(dist, assets)

	const immutable = "public, max-age=31536000, immutable"
	const short = "public, max-age=3600"

	tests := []struct {
		path string
		want string
		code int
	}{
		{"/static/base-AAAA.js", immutable, http.StatusOK},
		{"/static/base-BBBB.css", immutable, http.StatusOK},
		// Copied through from publicDir unhashed, so a year-long cache
		// would mean an updated resume never reaching anybody.
		{"/static/pdfs/cv.pdf", short, http.StatusOK},
		{"/static/images/a.webp", short, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if w.Code != tt.code {
				t.Fatalf("status = %d, want %d", w.Code, tt.code)
			}
			if got := w.Header().Get("Cache-Control"); got != tt.want {
				t.Errorf("Cache-Control = %q, want %q", got, tt.want)
			}
		})
	}

	// Build metadata. The server reads it; nothing else has a use for it.
	t.Run("manifest is not served", func(t *testing.T) {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/static/.vite/manifest.json", nil))
		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", w.Code)
		}
	})
}

// EdgeCache fills in the site policy for a handler that sets no Cache-Control,
// which is what makes a silent handler cacheable. A handler that has an opinion
// keeps it, and every /healthz in this repo relies on that to say no-store:
// without it the edge answers a liveness check out of cache long after the
// origin has stopped serving, which is exactly what blog.bythewood.me did.
func TestEdgeCacheYieldsToTheHandler(t *testing.T) {
	const policy = "public, max-age=14400"

	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    string
	}{
		{
			name: "silent handler takes the site policy",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("ok"))
			},
			want: policy,
		},
		{
			name: "no-store survives",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Cache-Control", "no-store")
				_, _ = w.Write([]byte("ok"))
			},
			want: "no-store",
		},
		{
			name: "an explicit policy survives",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Cache-Control", "private, max-age=30")
				w.WriteHeader(http.StatusOK)
			},
			want: "private, max-age=30",
		},
		{
			// Cloudflare stamps its own TTL on a header-less response, so an
			// error has to say no-store or a 404 is held at the edge.
			name: "an error is never cacheable",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			want: "no-store",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			EdgeCache(policy)(tt.handler).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))

			if got := w.Header().Get("Cache-Control"); got != tt.want {
				t.Errorf("Cache-Control = %q, want %q", got, tt.want)
			}
		})
	}
}

// component is a rollup dimension in the log store, so this set has to stay
// small and every request has to land in it. An empty component was why the
// dashboard could group requests by status but never by what kind of route
// served them.
func TestRouteClassIsBoundedAndTotal(t *testing.T) {
	seen := map[string]bool{}

	for _, tc := range []struct {
		path        string
		contentType string
		want        string
	}{
		{"/", "text/html", "page"},
		{"/posts/some-slug/", "text/html", "page"},
		{"/orchard/blob/main/go.mod", "text/html", "page"},
		{"/login", "text/html", "page"},
		{"/static/base-abc123.js", "text/javascript", "static"},
		{"/content/images/avatar.webp", "image/webp", "static"},
		{"/og/post.png", "image/png", "static"},
		{"/media/images/old.webp", "text/html", "static"},
		{"/_next/image", "text/html", "static"},
		{"/robots.txt", "text/plain", "asset"},
		{"/sitemap.xml", "application/xml", "asset"},
		{"/favicon.ico", "image/x-icon", "asset"},
		{"/healthz", "text/plain", "healthz"},
		{"/events", "text/event-stream", "stream"},
		// Content type wins: a stream is a stream whatever it is served from.
		{"/api/live", "text/event-stream; charset=utf-8", "stream"},
	} {
		rec := &recorder{ResponseWriter: httptest.NewRecorder()}
		rec.Header().Set("Content-Type", tc.contentType)
		got := routeClass(rec, tc.path)
		if got != tc.want {
			t.Errorf("routeClass(%q, %q) = %q, want %q", tc.path, tc.contentType, got, tc.want)
		}
		seen[got] = true
	}

	if len(seen) > 5 {
		t.Errorf("routeClass produced %d values, and it is a rollup key: %v", len(seen), seen)
	}
}
