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
// The order matters and it is not the obvious one. Behind a Cloudflare Tunnel
// the last X-Forwarded-For entry is always the cloudflared container's bridge
// address, because cloudflared sets XFF to the real client and Caddy then
// appends its peer. That was measured through the tunnel test on 2026-08-24,
// not guessed: the app saw "X-Forwarded-For: <client>, 172.18.0.4".
//
// CF-Connecting-IP is written by Cloudflare's edge and cannot be spoofed from
// outside, because behind a tunnel there is no non-tunnel inbound path to this
// process at all. So it wins whenever it is present, and XFF is only the
// fallback for local requests that never crossed the edge.
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

// Logged writes one record per request. The tunnel test shipped without this
// and was blind to every request it served, so it is on by default here rather
// than something a site opts into.
//
// Structured, via log/slog, rather than a formatted line. These run in
// containers whose logs are read with `docker logs` and grep, and the
// difference between a string and a set of typed fields is the difference
// between eyeballing and filtering: status>=500, or every request from one IP,
// or the slow tail, are all one jq away and none of them are a regex over
// prose.
func Logged(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &recorder{ResponseWriter: w}

		next.ServeHTTP(rec, r)

		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		// Duration in milliseconds as a float, not a formatted Duration
		// string: "1.042ms" cannot be compared or sorted by a log query and
		// 1.042 can.
		attrs := []any{
			slog.Int("status", rec.status),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("host", r.Host),
			slog.String("ip", ClientIP(r)),
			slog.Int("bytes", rec.bytes),
			slog.Float64("ms", float64(time.Since(start).Microseconds())/1000),
		}
		// Present only when the request actually came through the tunnel, so
		// its absence is the signal that something reached the origin
		// directly.
		if ray := r.Header.Get("CF-Ray"); ray != "" {
			attrs = append(attrs, slog.String("cf_ray", ray))
		}
		slog.Info("request", attrs...)
	})
}

// Recovered turns a panic in a handler into a 500 instead of killing the
// process and taking every other in-flight request with it.
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

// SecurityHeaders applies the headers that are this app's job rather than the
// edge's.
//
// HSTS is deliberately absent: Caddy sets it, and behind the tunnel this
// process only ever speaks plaintext HTTP on a Docker bridge, so announcing a
// TLS policy from here would be a lie about a connection it cannot see.
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

// Chain applies middleware so that the first argument is the outermost layer,
// which is the order they read in on the page.
func Chain(h http.Handler, mw ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

// EdgeCache sets a shared cache policy on successful GET responses that have
// not already chosen one.
//
// This matters more here than it would on a rented server. The origin is a
// desktop at the end of a Cloudflare Tunnel, so every uncached HTML request is
// a round trip to a machine in a house, and every minute the tunnel is down is
// a minute of 530s. Both halves of that are addressed by one header:
//
//	max-age=<short>            how long a copy is simply fresh, for browser
//	                           and edge alike. Not s-maxage: that directive
//	                           implies proxy-revalidate, and Cloudflare
//	                           therefore lets it disable both stale
//	                           directives below, which are the point.
//	stale-while-revalidate     a stale hit is served instantly and refreshed
//	                           behind the request, so nobody waits for the
//	                           tunnel on a cache miss that just expired
//	stale-if-error=<long>      and this is the one that earns its keep: when
//	                           the desktop or the tunnel is down, the edge is
//	                           allowed to keep serving the last good copy
//	                           rather than the 530 it would otherwise render.
//	                           Cloudflare ignores it when Always Online is
//	                           on, so Always Online has to stay off.
//
// Cloudflare still will not cache HTML on a free plan without a Cache Rule
// saying so. This header is the half that lives in the repo; the Cache Rule is
// the half that lives in the dashboard, and neither does much alone.
//
// Handlers that set their own Cache-Control keep it: the static bundle wants a
// year and immutable, a PDF wants a day, a logged in dashboard wants none of
// this at all. Only 200s get the caching policy; anything 400 and above is
// given an explicit no-store, so an error page cannot be pinned at the edge.
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

// edgeCacheWriter defers the decision to WriteHeader, which is the only point
// at which both the status code and whatever the handler chose are known.
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
				// Errors are refused the cache explicitly rather than by
				// saying nothing. Silence is not neutral once a Cache Rule
				// has marked the zone eligible: Cloudflare stamps its own
				// browser TTL on a header-less response and will happily
				// hold a 404 at the edge, so publishing a post at a URL
				// somebody already missed would serve them the 404 for
				// hours. Measured, not guessed: a 404 went MISS then HIT.
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
