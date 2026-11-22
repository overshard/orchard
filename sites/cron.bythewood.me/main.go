// cron.bythewood.me keeps reminders and scheduled checks and pushes them to the
// phone over ntfy. A job is a message to send or a prompt for the model, on a
// cron line or once at a time, and it is managed here or over the API.
package main

import (
	"context"
	"flag"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"

	"cron.bythewood.me/ntfy"
	"cron.bythewood.me/suite"
	"cron.bythewood.me/web"
)

const (
	listenAddr = ":8000"
	baseURL    = "https://cron.bythewood.me"
	sourceURL  = "https://github.com/overshard/orchard/tree/main/sites/cron.bythewood.me"
	selfSource = "cron"
	author     = "Isaac Bythewood"
	zone       = "America/New_York"
	topic      = "reminders"
)

func csp() string {
	return strings.Join([]string{
		"default-src 'self'",
		"script-src 'self' https://analytics.bythewood.me/static/collector.js",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data:",
		"font-src 'self'",
		"connect-src 'self' https://analytics.bythewood.me https://pond.bythewood.me",
		"base-uri 'self'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	}, "; ")
}

type site struct {
	auth  *web.Authenticator
	store *Store
	sched *Scheduler
	loc   *time.Location
	tpl   *template.Template
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	if err := realMain(); err != nil {
		slog.Error("exiting", slog.Any("err", err))
		os.Exit(1)
	}
}

// realMain returns instead of exiting so the deferred closes and the log
// shipper's flush run on every path out.
func realMain() error {
	var (
		dbPath = flag.String("db", env("CRON_DB", "data/cron.db"), "database path")
		check  = flag.Bool("healthcheck", false, "probe this process and exit")
		newKey = flag.String("newkey", "", "mint an api key with this name, print it, and exit")
	)
	flag.Parse()

	if *check {
		if err := web.HealthCheck("http://127.0.0.1"+listenAddr+"/healthz", 3*time.Second); err != nil {
			return fmt.Errorf("healthcheck: %w", err)
		}
		return nil
	}

	web.SetupLogging()
	shipper := web.ShipLogs(selfSource, web.HTTPSink())
	defer shipper.Close()

	loc, err := time.LoadLocation(zone)
	if err != nil {
		return err
	}
	store, err := OpenStore(*dbPath)
	if err != nil {
		return fmt.Errorf("opening the database: %w", err)
	}
	defer store.Close()

	if *newKey != "" {
		secret, err := store.NewKey(*newKey)
		if err != nil {
			return err
		}
		fmt.Println(secret)
		return nil
	}

	push := ntfy.New(topic, os.Getenv("NTFY_TOKEN"))
	if !push.Configured() {
		slog.Warn("no NTFY_TOKEN, every push will be refused")
	}
	model := NewModel(env("LLM_URL", "http://orchard-llm:8000"), os.Getenv("LLM_KEY"))
	if model == nil {
		slog.Warn("no LLM_KEY, prompt jobs will fail")
	}

	s := &site{auth: web.NewAuthenticator(), store: store, loc: loc}
	s.sched = NewScheduler(store, push, model, loc)
	if err := s.loadTemplates(); err != nil {
		return fmt.Errorf("templates: %w", err)
	}

	ctx, stop := context.WithCancel(context.Background())
	var bg sync.WaitGroup
	defer bg.Wait()
	defer stop()
	bg.Go(func() { s.sched.Loop(ctx) })
	bg.Go(func() {
		t := time.NewTicker(24 * time.Hour)
		defer t.Stop()
		for {
			store.Prune(time.Now())
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	})

	slog.Info("cron listening", "addr", listenAddr, "zone", zone, "topic", topic)
	return web.Serve(listenAddr, web.Chain(s.routes(), web.Recovered, web.Logged, web.SecurityHeaders(csp()), web.SameOrigin()))
}

// routes is a method so the tests drive the real mux.
func (s *site) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.root)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) { s.notFound(w, r, "That page does not exist.") })
	mux.HandleFunc("GET /jobs/new", s.auth.RequireAuth(s.newJobPage))
	mux.HandleFunc("GET /jobs/{id}", s.auth.RequireAuth(s.jobPage))
	mux.HandleFunc("POST /jobs", s.auth.RequireAuth(s.jobCreate))
	mux.HandleFunc("POST /jobs/{id}", s.auth.RequireAuth(s.jobSave))
	mux.HandleFunc("POST /jobs/{id}/run", s.auth.RequireAuth(s.jobRunNow))
	mux.HandleFunc("POST /jobs/{id}/toggle", s.auth.RequireAuth(s.jobToggle))
	mux.HandleFunc("POST /jobs/{id}/delete", s.auth.RequireAuth(s.jobDelete))
	mux.HandleFunc("POST /jobs/{id}/pause", s.auth.RequireAuth(s.setEnabled(false)))
	mux.HandleFunc("POST /jobs/{id}/resume", s.auth.RequireAuth(s.setEnabled(true)))
	mux.HandleFunc("POST /jobs/{id}/try", s.auth.RequireAuth(s.jobTry))
	mux.HandleFunc("POST /jobs/{id}/move", s.auth.RequireAuth(s.jobMove))
	mux.HandleFunc("POST /jobs/bulk", s.auth.RequireAuth(s.jobBulk))
	mux.HandleFunc("GET /runs", s.auth.RequireAuth(s.runsPage))
	mux.HandleFunc("GET /keys", s.auth.RequireAuth(s.keysPage))
	mux.HandleFunc("POST /keys", s.auth.RequireAuth(s.keyCreate))
	mux.HandleFunc("POST /keys/{id}/delete", s.auth.RequireAuth(s.keyDelete))
	mux.Handle("GET /static/", s.static())
	mux.Handle("GET /suite/state", suite.StateHandler(s.auth.Authenticated, s.suiteState))
	mux.Handle("GET /favicon.svg", suite.Favicon("cron"))
	mux.Handle("GET /favicon.ico", suite.Favicon("cron"))
	mux.Handle("GET /suite/", suite.Handler())
	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, web.LoginURL(r), http.StatusSeeOther)
	})

	// The phone presses this from a notification, so the token in the path is
	// the only credential it has.
	mux.HandleFunc("POST /snooze/{token}", s.snooze)

	mux.HandleFunc("GET /api/jobs", s.api(s.apiList))
	mux.HandleFunc("POST /api/jobs", s.api(s.apiCreate))
	mux.HandleFunc("GET /api/jobs/{id}", s.api(s.apiGet))
	mux.HandleFunc("PATCH /api/jobs/{id}", s.api(s.apiPatch))
	mux.HandleFunc("DELETE /api/jobs/{id}", s.api(s.apiDelete))
	mux.HandleFunc("POST /api/jobs/{id}/run", s.api(s.apiRun))
	mux.HandleFunc("POST /api/jobs/{id}/try", s.api(s.apiTry))
	mux.HandleFunc("GET /api/runs", s.api(s.apiRuns))
	mux.HandleFunc("GET /api/preview", s.api(s.apiPreview))

	mux.HandleFunc("GET /robots.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}
