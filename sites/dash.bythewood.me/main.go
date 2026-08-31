// Command dash serves dash.bythewood.me: markets, news, weather and the state
// of every site in this repo, on one page that updates itself. Everything it
// shows is fetched by one poller and pushed to every open browser over
// server-sent events, so the page costs the upstreams the same whether nobody
// or everybody is watching.
package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"dash.bythewood.me/web"
)

// Templates are source, so they ship in the binary unconditionally; the Vite
// bundle is build output and only embeds in a release build.
//
//go:embed templates
var templateFS embed.FS

const listenAddr = ":8000"

// waitForListener blocks until this process answers its own health check. The
// health strip probes over loopback, so a first round started before Serve has
// bound the socket reports this site as unknown for a minute after every
// deploy. There is no deadline because a server that never binds has already
// exited on the error.
func waitForListener(ctx context.Context) {
	for {
		if err := web.HealthCheck("http://127.0.0.1:8000/healthz", time.Second); err == nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func dir(env, fallback string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return fallback
}

// csp allows 'unsafe-inline' for the analytics collector snippet and for the
// sparkline paths, which are computed per quote and written as style attributes
// rather than into a stylesheet that would need a nonce per render.
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

type site struct {
	renderer *web.Renderer
	store    *Store
	hub      *Hub
	guard    *Guard

	script string
	styles []string
}

func main() {
	web.SetupLogging()

	healthcheck := flag.Bool("healthcheck", false, "probe a running server on this host and exit")
	flag.Parse()

	if *healthcheck {
		if err := web.HealthCheck("http://127.0.0.1:8000/healthz", 3*time.Second); err != nil {
			slog.Info(fmt.Sprintf("healthcheck: %v", err))
			os.Exit(1)
		}
		return
	}

	shipper := web.ShipLogs(selfSource, web.HTTPSink())
	defer shipper.Close()

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
		[]string{"home.html", "notfound.html"},
	)
	if err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}

	dataDir := dir("SITE_DATA", "build/data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}

	hub := NewHub()
	guard := NewGuard(dataDir)
	store := NewStore(hub)

	s := &site{
		renderer: renderer,
		store:    store,
		hub:      hub,
		guard:    guard,
		script:   assets.Script("index.js"),
		styles:   assets.Styles("index.js"),
	}

	// Cancelled on shutdown so an in-flight fetch is abandoned rather than
	// holding the process past the stop grace period.
	pollCtx, stopPolling := context.WithCancel(context.Background())
	defer stopPolling()

	store.Prime(pollCtx, guard)
	go func() {
		waitForListener(pollCtx)
		store.Run(pollCtx, guard)
	}()

	// The guard's counters only reach disk on Flush, and a container stop that
	// skipped it would forget an open breaker.
	stopping := make(chan os.Signal, 1)
	signal.Notify(stopping, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stopping
		stopPolling()
		guard.Flush()
	}()

	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /events", s.events)
	mux.HandleFunc("GET /api/state", s.state)

	mux.HandleFunc("GET /favicon.ico", favicon)
	mux.HandleFunc("GET /favicon.svg", favicon)
	mux.HandleFunc("GET /robots.txt", robots)
	mux.HandleFunc("GET /sitemap.xml", sitemap)

	mux.Handle("GET /static/", web.Static(dist, assets))

	mux.HandleFunc("GET /healthz", s.healthz)

	mux.HandleFunc("GET /", s.notFound)

	handler := web.Chain(mux,
		web.Recovered,
		web.Logged,
		web.SecurityHeaders(csp()),
	)

	slog.Info(fmt.Sprintf("dash serving %s (staging=%t)", baseURL, Staging))
	if err := web.Serve(listenAddr, handler); err != nil {
		slog.Error("startup failed", slog.Any("err", err))
		os.Exit(1)
	}
}
