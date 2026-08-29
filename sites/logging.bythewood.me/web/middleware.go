package web

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

// ClientIP resolves the real client address.
//
// CF-Connecting-IP wins over X-Forwarded-For, which is not the usual ordering.
// Behind the tunnel the last XFF entry is always the cloudflared container's
// bridge address, because cloudflared sets XFF to the real client and Caddy
// then appends its own peer. CF-Connecting-IP is written by Cloudflare's edge,
// and there is no inbound path to this process that bypasses the tunnel, so it
// cannot be spoofed. XFF is the fallback for requests that never crossed the
// edge.
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

// Logged writes one structured record per request, so that status, IP and the
// slow tail are all one jq filter away rather than a regex over prose.
//
// Duration is milliseconds as a float rather than a formatted Duration, because
// "1.042ms" cannot be sorted or compared by a log query and 1.042 can.
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
		}
		// Present only when the request came through the tunnel, so its
		// absence means something reached the origin directly.
		if ray := r.Header.Get("CF-Ray"); ray != "" {
			attrs = append(attrs, slog.String("cf_ray", ray))
		}
		slog.Info("request", attrs...)
	})
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
// edge. HSTS is absent because Caddy sets it: this process only ever speaks
// plaintext HTTP on a Docker bridge and cannot see the connection it would be
// making a claim about.
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

// EdgeCache sets a shared cache policy on successful GET responses that have
// not already chosen one. The origin is a desktop at the end of a Cloudflare
// Tunnel, so the policy is written around the tunnel being down:
//
//	max-age                 how long a copy is fresh, for browser and edge
//	                        alike. Never s-maxage: it carries proxy-revalidate
//	                        semantics, which makes Cloudflare disable both
//	                        directives below.
//	stale-while-revalidate  a stale hit is served immediately and refreshed
//	                        behind the request.
//	stale-if-error          the edge keeps serving the last good copy instead
//	                        of a 530. Cloudflare ignores this when Always
//	                        Online is on, so Always Online has to stay off.
//
// Cloudflare will not cache HTML on a free plan without a Cache Rule, which
// lives in the dashboard rather than here.
//
// Handlers that set their own Cache-Control keep it. Only 200s get the policy;
// 400 and above get an explicit no-store.
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

// edgeCacheWriter defers the decision to WriteHeader, the only point at which
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
				// Explicit rather than silent. Once a Cache Rule marks
				// the zone eligible, Cloudflare stamps its own TTL on a
				// header-less response and will hold a 404 at the edge.
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
