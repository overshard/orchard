package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"pass.bythewood.me/ntfy"
)

// pusher is ntfy.Client, behind an interface so a test can count the pushes.
type pusher interface {
	Deliver(ctx context.Context, m ntfy.Message) error
}

// watcher turns Vaultwarden's event log and device list into log records and
// pushes. It starts from now, so a restart never repeats an old alert.
type watcher struct {
	vault *Vault
	push  pusher
	http  *http.Client

	mu       sync.Mutex
	since    time.Time
	seen     map[string]time.Time
	devices  map[string]bool
	version  string
	latest   string
	checked  time.Time
	vwUp     bool
	vwErr    string
	notified string
}

func newWatcher(v *Vault, p pusher) *watcher {
	return &watcher{vault: v, push: p, http: &http.Client{Timeout: 10 * time.Second}, since: time.Now().UTC().Truncate(time.Second), seen: map[string]time.Time{}}
}

func (w *watcher) run() {
	w.ask(context.Background())
	w.release(context.Background())
	tick := time.NewTicker(30 * time.Second)
	day := time.NewTicker(6 * time.Hour)
	for {
		select {
		case <-tick.C:
			w.ask(context.Background())
			w.scan(context.Background(), time.Now())
		case <-day.C:
			w.release(context.Background())
		}
	}
}

// scan reads the last ten minutes of events and acts on any it hasn't seen,
// since event times are whole seconds and a later write can share one.
func (w *watcher) scan(ctx context.Context, now time.Time) {
	from := now.Add(-10 * time.Minute)
	if from.Before(w.since) {
		from = w.since
	}
	evs, err := w.vault.Events(from, 500)
	if err != nil {
		if !missingTable(err) {
			slog.Warn("pass events unreadable", slog.Any("err", err))
		}
		return
	}
	for id, at := range w.seen {
		if at.Before(now.Add(-15 * time.Minute)) {
			delete(w.seen, id)
		}
	}
	for i := len(evs) - 1; i >= 0; i-- {
		e := evs[i]
		if _, ok := w.seen[e.ID]; ok {
			continue
		}
		w.seen[e.ID] = e.At
		attrs := []any{slog.String("event", eventWord(e.Type)), slog.String("device", deviceKind(e.Device)), slog.String("ip", e.IP)}
		if e.Loud() {
			slog.Warn("pass "+eventWord(e.Type), attrs...)
			w.send(ctx, ntfy.Message{
				Title:    "pass: " + eventWord(e.Type),
				Body:     fmt.Sprintf("%s from %s at %s UTC", deviceKind(e.Device), orDash(e.IP), e.At.Format("15:04")),
				Priority: priorityFor(e.Type),
				Tags:     "lock",
				Link:     baseURL + "/logins",
				Label:    "Logins",
			})
		} else {
			slog.Info("pass signed in", attrs...)
		}
	}
	ds, err := w.vault.Devices()
	if err != nil {
		return
	}
	w.mu.Lock()
	first := w.devices == nil
	if first {
		w.devices = map[string]bool{}
	}
	var fresh []Device
	for _, d := range ds {
		if !w.devices[d.ID] {
			w.devices[d.ID] = true
			if !first {
				fresh = append(fresh, d)
			}
		}
	}
	w.mu.Unlock()
	for _, d := range fresh {
		slog.Warn("pass new device", slog.String("device", d.Name), slog.String("kind", deviceKind(d.Type)))
		w.send(ctx, ntfy.Message{
			Title:    "pass: a new device signed in",
			Body:     fmt.Sprintf("%s (%s). If that wasn't you, change the master password and sign out every device.", d.Name, deviceKind(d.Type)),
			Priority: "high",
			Tags:     "lock",
			Link:     baseURL + "/devices",
			Label:    "Devices",
		})
	}
}

func priorityFor(t int) string {
	switch t {
	case evFailedLogin, evFailed2FA:
		return "default"
	}
	return "high"
}

func (w *watcher) send(ctx context.Context, m ntfy.Message) {
	if w.push == nil {
		return
	}
	go func() {
		if err := w.push.Deliver(ctx, m); err != nil {
			slog.Warn("pass push failed", slog.String("title", m.Title), slog.Any("err", err))
		}
	}()
}

// ask is Vaultwarden answering and the version it says it is.
func (w *watcher) ask(ctx context.Context) {
	v, err := w.get(ctx, vaultURL+"/api/version")
	w.mu.Lock()
	defer w.mu.Unlock()
	if err != nil {
		w.vwUp, w.vwErr = false, err.Error()
		return
	}
	var s string
	if json.Unmarshal(v, &s) == nil && s != "" {
		w.version = s
	}
	w.vwUp, w.vwErr = true, ""
}

// release is the newest Vaultwarden on GitHub. A push goes out once per new
// release, since it has had fixes that are worth deploying the same day.
func (w *watcher) release(ctx context.Context) {
	b, err := w.get(ctx, releasesURL)
	if err != nil {
		slog.Warn("pass release check failed", slog.Any("err", err))
		return
	}
	var r struct {
		Tag string `json:"tag_name"`
	}
	if json.Unmarshal(b, &r) != nil || r.Tag == "" {
		return
	}
	w.mu.Lock()
	w.latest, w.checked = strings.TrimPrefix(r.Tag, "v"), time.Now()
	behind := w.version != "" && w.version != w.latest && w.notified != w.latest
	if behind {
		w.notified = w.latest
	}
	have, latest := w.version, w.latest
	w.mu.Unlock()
	if behind {
		slog.Warn("pass vaultwarden update out", slog.String("running", have), slog.String("latest", latest))
		w.send(ctx, ntfy.Message{
			Title:    "pass: Vaultwarden " + latest + " is out",
			Body:     "Running " + have + ". Bump the digest in the Dockerfile and deploy.",
			Priority: "default",
			Tags:     "package",
			Link:     "https://github.com/dani-garcia/vaultwarden/releases/tag/" + latest,
			Label:    "Release",
		})
	}
}

func (w *watcher) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "pass.bythewood.me")
	resp, err := w.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// Server is the state of Vaultwarden as the watcher last saw it.
type Server struct {
	Up              bool
	Err             string
	Version, Latest string
	Checked         time.Time
}

func (s Server) Behind() bool { return s.Version != "" && s.Latest != "" && s.Version != s.Latest }

func (w *watcher) server() Server {
	w.mu.Lock()
	defer w.mu.Unlock()
	return Server{Up: w.vwUp, Err: w.vwErr, Version: w.version, Latest: w.latest, Checked: w.checked}
}

func orDash(s string) string {
	if s == "" {
		return "an unknown address"
	}
	return s
}
