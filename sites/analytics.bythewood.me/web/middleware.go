package web

import (
	"log"
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

// Logged writes one line per request. The tunnel test shipped without this and
// was blind to every request it served, so it is on by default here rather
// than something a site opts into.
func Logged(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &recorder{ResponseWriter: w}

		next.ServeHTTP(rec, r)

		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		via := "direct"
		if ray := r.Header.Get("CF-Ray"); ray != "" {
			via = "cloudflare ray=" + ray
		}
		log.Printf("%d %s %s host=%s ip=%s bytes=%d %s %s",
			rec.status, r.Method, r.URL.Path, r.Host,
			ClientIP(r), rec.bytes, time.Since(start).Round(time.Microsecond), via)
	})
}

// Recovered turns a panic in a handler into a 500 instead of killing the
// process and taking every other in-flight request with it.
func Recovered(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("panic serving %s %s: %v", r.Method, r.URL.Path, err)
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
