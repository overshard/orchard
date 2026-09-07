package web

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

// ClientIP resolves the real client address. CF-Connecting-IP wins over
// X-Forwarded-For, which is not the usual ordering: behind the tunnel the last
// XFF entry is always cloudflared's own bridge address.
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

// Unwrap is what http.ResponseController follows to reach the real writer.
// Without it a wrapped handler cannot flush, so a server-sent events endpoint
// behind this middleware buffers until the connection closes.
func (w *recorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Logged writes one structured record per request. Duration is a float in
// milliseconds rather than a formatted Duration, because "1.042ms" cannot be
// sorted or compared by a log query and 1.042 can.
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
		}
		slog.Info("request", attrs...)
	})
}

// routeClass buckets a request into something the log store can group by. It
// has to stay small: this is a rollup dimension there, and one that grew with
// the URL space would make that table grow like the raw one it exists to avoid.
//
// A stream is named because its elapsed time measures the visit rather than any
// work done, so nothing downstream should average it in with real requests.
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

// Recovered turns a panic in a handler into a 500 rather than killing the
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

// SecurityHeaders applies the headers that belong to the app rather than the
// edge. No HSTS: Caddy sets it, and this process only ever speaks plaintext on
// a Docker bridge.
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

// Chain applies middleware so the first argument is the outermost layer.
func Chain(h http.Handler, mw ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

// EdgeCache sets a shared cache policy on 200 GETs that have not already chosen
// one; 400 and above get no-store, and a handler's own Cache-Control is left
// alone.
//
// Never use s-maxage here. It carries proxy-revalidate semantics, which makes
// Cloudflare disable stale-while-revalidate and stale-if-error, and
// stale-if-error is what keeps the last good copy served when the tunnel drops.
// Cloudflare also ignores it with Always Online on, so that has to stay off.
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

// edgeCacheWriter defers the decision to WriteHeader, the first point at which
// both the status code and the handler's own choice are known.
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
