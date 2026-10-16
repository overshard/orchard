// Command dash serves dash.bythewood.me, markets, news, weather and the state
// of every site in this repo, on one page that updates itself. Each poll is
// pushed to every open browser, so another viewer costs the upstreams nothing.
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

// Templates are source, so they ship in the binary unconditionally. The Vite
// bundle is build output and only embeds in a release build.
//
//go:embed templates
var templateFS embed.FS

const listenAddr = ":8000"

// waitForListener blocks until this process answers its own health check, since
// the strip probes it over loopback. There's no deadline, since a server that
// never binds has already exited on the error.
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

// csp allows 'unsafe-inline' for the analytics snippet and the sparkline paths,
// which are computed per quote and written as style attributes instead of a
// stylesheet that would need a nonce per render.
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

	// Only the quick chat and the briefs rerun read these. Nothing else on the
	// page knows or cares who is looking at it.
	auth    *web.Authenticator
	chat    string
	briefer *Briefer

	script string
	styles []string
}

func main() {
	if err := realMain(); err != nil {
		slog.Error("exiting", slog.Any("err", err))
		os.Exit(1)
	}
}

// realMain returns instead of exiting, so its deferred closes and the log
// shipper's flush run on every path out.
func realMain() error {
	web.SetupLogging()

	healthcheck := flag.Bool("healthcheck", false, "probe a running server on this host and exit")
	flag.Parse()

	if *healthcheck {
		if err := web.HealthCheck("http://127.0.0.1:8000/healthz", 3*time.Second); err != nil {
			return fmt.Errorf("healthcheck: %w", err)
		}
		return nil
	}

	shipper := web.ShipLogs(selfSource, web.HTTPSink())
	defer shipper.Close()

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
		[]string{"home.html", "notfound.html"},
	)
	if err != nil {
		return err
	}

	dataDir := dir("SITE_DATA", "build/data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}

	hub := NewHub()
	guard := NewGuard(dataDir)
	store := NewStore(hub)
	store.chart = openSteamChart(dataDir)

	s := &site{
		renderer: renderer,
		store:    store,
		hub:      hub,
		guard:    guard,
		auth:     web.NewAuthenticator(),
		chat:     chatSendURL,
		script:   assets.Script("index.js"),
		styles:   assets.Styles("index.js"),
	}

	// Cancelled on shutdown so an in-flight fetch is abandoned instead of
	// holding the process past the stop grace period.
	pollCtx, stopPolling := context.WithCancel(context.Background())
	defer stopPolling()

	briefer := NewBriefer(store, guard, NewModel(dir("LLM_URL", "http://orchard-llm:8000"), os.Getenv("LLM_KEY")), dataDir)
	s.briefer = briefer

	store.Prime(pollCtx, guard)
	go func() {
		waitForListener(pollCtx)
		go briefer.Run(pollCtx)
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
	mux.HandleFunc("GET /api/quick", s.quick)
	mux.HandleFunc("POST /api/quick/send", s.quickSend)
	mux.HandleFunc("GET /api/rebuild", s.rebuildState)
	mux.HandleFunc("POST /api/rebuild", s.rebuildStart)

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
		web.SameOrigin(),
	)

	slog.Info(fmt.Sprintf("dash serving %s (staging=%t)", baseURL, Staging))
	return web.Serve(listenAddr, handler)
}
