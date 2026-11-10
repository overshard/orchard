package web

import (
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// ClientIP prefers CF-Connecting-IP over X-Forwarded-For and takes the last XFF
// entry instead of the first, both backwards from the usual order. Behind the
// tunnel that last entry is always cloudflared's own bridge address.
func ClientIP(r *http.Request) string {
	if ip := r.Header.Get("CF-Connecting-IP"); ip != "" {
		return ip
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[len(parts)-1])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type recorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *recorder) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *recorder) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

// Unwrap lets http.ResponseController reach the real writer. Without it a
// server-sent events endpoint buffers until the connection closes.
func (w *recorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Logged writes one structured record per request. Duration is a float in
// milliseconds so a log query can sort and compare it.
func Logged(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &recorder{ResponseWriter: w}

		next.ServeHTTP(rec, r)

		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		attrs := []any{
			slog.Int("status", rec.status),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("host", r.Host),
			slog.String("ip", ClientIP(r)),
			slog.Int("bytes", rec.bytes),
			slog.Float64("ms", float64(time.Since(start).Microseconds())/1000),
			slog.String("component", routeClass(rec, r.URL.Path)),
		}
		// Absent means the request never crossed the tunnel.
		if ray := r.Header.Get("CF-Ray"); ray != "" {
			attrs = append(attrs, slog.String("cf_ray", ray))
			// Only for requests from outside, since the probes and container to
			// container calls aren't anybody worth naming.
			if ua := r.Header.Get("User-Agent"); ua != "" {
				attrs = append(attrs, slog.String("ua", truncateUA(ua)))
			}
		}
		slog.Info("request", attrs...)
	})
}

// routeClass buckets a request for the log store's rollups, so it has to stay
// small and never grow with the URL space. A stream gets its own name so it can
// be left out, since its elapsed time measures the visit and not any work.
func routeClass(w *recorder, path string) string {
	if strings.HasPrefix(w.Header().Get("Content-Type"), "text/event-stream") {
		return "stream"
	}
	switch path {
	case "/healthz":
		return "healthz"
	case "/favicon.ico", "/favicon.svg", "/robots.txt", "/sitemap.xml",
		"/manifest.json", "/latest.json", "/rss.xml", "/feed":
		return "asset"
	}
	for _, prefix := range []string{"/static/", "/static_maps/", "/content/", "/og/", "/media/", "/_next/"} {
		if strings.HasPrefix(path, prefix) {
			return "static"
		}
	}
	return "page"
}

// Recovered turns a panic in a handler into a 500 instead of killing the
// process and every other in-flight request with it.
func Recovered(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				slog.Error("panic serving request",
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.Any("panic", err),
				)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// SecurityHeaders applies the headers that belong to the app instead of the
// edge. No HSTS here, since Caddy sets it and this process only speaks plaintext
// on a Docker bridge.
func SecurityHeaders(csp string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "SAMEORIGIN")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), interest-cohort=()")
			if csp != "" {
				h.Set("Content-Security-Policy", csp)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SameOrigin refuses a state-changing request a browser says came from another
// origin, since SameSite=Strict counts every *.bythewood.me host as one site. A
// request with no Sec-Fetch-Site is a container or git and passes.
func SameOrigin(exempt ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			if slices.Contains(exempt, r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			switch r.Header.Get("Sec-Fetch-Site") {
			case "", "same-origin", "none":
				next.ServeHTTP(w, r)
			default:
				http.Error(w, "cross origin request refused", http.StatusForbidden)
			}
		})
	}
}

// NoDirs keeps a file server from answering a directory with an index of every
// file in it. It also refuses a name that isn't UTF-8, which fs.Sub fails as
// invalid instead of missing and FileServer turns into a 500.
func NoDirs(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "" || strings.HasSuffix(r.URL.Path, "/") || !utf8.ValidString(r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Chain applies middleware so the first argument is the outermost layer.
func Chain(h http.Handler, mw ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

// EdgeCache sets policy on a 200 GET with no Cache-Control of its own, and
// no-store on 400 and up. Never put s-maxage in it, Cloudflare then drops
// stale-if-error and stops serving the last good copy when the tunnel is down.
func EdgeCache(policy string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				next.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(&edgeCacheWriter{ResponseWriter: w, policy: policy}, r)
		})
	}
}

// edgeCacheWriter defers the decision to WriteHeader, the first point where both
// the status code and the handler's own choice are known.
type edgeCacheWriter struct {
	http.ResponseWriter
	policy string
	done   bool
}

func (w *edgeCacheWriter) WriteHeader(code int) {
	if !w.done {
		w.done = true
		if w.Header().Get("Cache-Control") == "" {
			switch {
			case code == http.StatusOK:
				w.Header().Set("Cache-Control", w.policy)
			case code >= 400:
				// Cloudflare stamps its own TTL on a header-less
				// response and will hold a 404 at the edge.
				w.Header().Set("Cache-Control", "no-store")
			}
		}
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *edgeCacheWriter) Write(b []byte) (int, error) {
	if !w.done {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func (w *edgeCacheWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// uaMaxLen keeps a user agent to the part that names the client, since this is
// stored on every request for the whole retention window.
const uaMaxLen = 120

func truncateUA(ua string) string {
	if len(ua) <= uaMaxLen {
		return ua
	}
	return ua[:uaMaxLen]
}
