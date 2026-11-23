package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"pass.bythewood.me/ntfy"
)

// pusher is ntfy.Client, behind an interface so a test can count the pushes.
type pusher interface {
	Deliver(ctx context.Context, m ntfy.Message) error
}

// downAfter is how many missed asks in a row read as Vaultwarden down, so one
// slow answer during a deploy doesn't push.
const downAfter = 2

// watcher turns Vaultwarden's event log and device list into log records and
// pushes, says when Vaultwarden stops answering, and checks for a release.
type watcher struct {
	vault *Vault
	push  pusher
	http  *http.Client
	// signupsOff switches signups off once a new account appears. Nil in tests
	// that don't care.
	signupsOff func(context.Context) (bool, error)

	mu       sync.Mutex
	since    time.Time
	seen     map[string]time.Time
	devices  map[string]bool
	users    int
	version  string
	latest   string
	checked  time.Time
	misses   int
	vwErr    string
	notified string
}

func newWatcher(v *Vault, p pusher) *watcher {
	// A couple of minutes back, so an event written while the page restarted
	// still alerts. The seen set keeps it to once.
	since := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Second)
	return &watcher{vault: v, push: p, http: &http.Client{Timeout: 10 * time.Second}, since: since, seen: map[string]time.Time{}, devices: map[string]bool{}, users: -1}
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

// scan reads the last ten minutes of events and acts on any it hasn't seen.
// The window overlaps scans so a late commit is still caught.
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
		if !e.Loud() {
			slog.Info("pass signed in", attrs...)
			continue
		}
		slog.Warn("pass "+eventWord(e.Type), attrs...)
		w.send(ctx, ntfy.Message{
			Title:    "pass: " + eventWord(e.Type),
			Body:     fmt.Sprintf("%s from %s at %s UTC", deviceKind(e.Device), orDash(e.IP), e.At.Format("15:04")),
			Priority: priorityFor(e.Type),
			Tags:     "lock",
			Link:     baseURL + "/logins",
			Label:    "Logins",
		})
	}
	ds, err := w.vault.Devices()
	if err == nil {
		for _, d := range ds {
			if d.Created.Before(w.since) || w.devices[d.ID] {
				continue
			}
			w.devices[d.ID] = true
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
	w.closeSignups(ctx)
}

// closeSignups switches signups off the moment the user count goes up, since
// they're only ever on to make one account.
func (w *watcher) closeSignups(ctx context.Context) {
	n, err := w.vault.Users()
	if err != nil {
		return
	}
	prev := w.users
	w.users = n
	if prev < 0 || n <= prev || w.signupsOff == nil {
		return
	}
	switched, err := w.signupsOff(ctx)
	if err != nil {
		slog.Error("pass signups left on after a new account", slog.Any("err", err))
		w.send(ctx, ntfy.Message{Title: "pass: signups are still on", Body: "A new account was made and switching signups off failed: " + err.Error(),
			Priority: "high", Tags: "lock", Link: baseURL + "/account", Label: "Account"})
		return
	}
	if switched {
		slog.Warn("pass signups switched off after a new account")
		w.send(ctx, ntfy.Message{Title: "pass: signups switched off", Body: "A new account was made, so signups are off again.",
			Priority: "default", Tags: "lock", Link: baseURL + "/account", Label: "Account"})
	}
}

func priorityFor(t int) string {
	switch t {
	case evFailedLogin, evFailed2FA:
		return "default"
	}
	return "high"
}

// send never takes a request's context, since the push outlives the request.
func (w *watcher) send(ctx context.Context, m ntfy.Message) {
	if w.push == nil {
		return
	}
	ctx = context.WithoutCancel(ctx)
	go func() {
		if err := w.push.Deliver(ctx, m); err != nil {
			slog.Warn("pass push failed", slog.String("title", m.Title), slog.Any("err", err))
		}
	}()
}

// ask is Vaultwarden answering and the version it says it is. Going down and
// coming back each push once.
func (w *watcher) ask(ctx context.Context) {
	v, err := w.get(ctx, vaultURL+"/api/version")
	w.mu.Lock()
	wasDown := w.misses >= downAfter
	if err != nil {
		w.misses++
		w.vwErr = err.Error()
	} else {
		var s string
		if json.Unmarshal(v, &s) == nil && s != "" {
			w.version = s
		}
		w.misses, w.vwErr = 0, ""
	}
	isDown := w.misses >= downAfter
	msg := w.vwErr
	w.mu.Unlock()
	switch {
	case isDown && !wasDown:
		slog.Error("pass vaultwarden not answering", slog.String("err", msg))
		w.send(ctx, ntfy.Message{Title: "pass: Vaultwarden is down", Body: "The apps are on their cached copy and can't save anything until it's back. " + msg,
			Priority: "high", Tags: "warning", Link: baseURL + "/", Label: "Overview"})
	case wasDown && !isDown:
		slog.Info("pass vaultwarden answering again")
		w.send(ctx, ntfy.Message{Title: "pass: Vaultwarden is back", Body: "It's answering the apps again.", Priority: "default", Tags: "white_check_mark"})
	}
}

// release is the newest Vaultwarden on GitHub. The first check after a start
// only records it, so a deploy doesn't repeat the push, and a release that
// lands while running pushes once.
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
	first := w.checked.IsZero()
	w.latest, w.checked = strings.TrimPrefix(r.Tag, "v"), time.Now()
	have, latest := w.version, w.latest
	push := !first && older(have, latest) && w.notified != latest
	if push || first {
		w.notified = latest
	}
	w.mu.Unlock()
	if older(have, latest) {
		slog.Warn("pass vaultwarden update out", slog.String("running", have), slog.String("latest", latest))
	}
	if push {
		w.send(ctx, ntfy.Message{
			Title:    "pass: Vaultwarden " + latest + " is out",
			Body:     "Running " + have + ". Bump the tag and digest in the Dockerfile and deploy.",
			Priority: "default",
			Tags:     "package",
			Link:     "https://github.com/dani-garcia/vaultwarden/releases/tag/" + latest,
			Label:    "Release",
		})
	}
}

// older is a running x.y.z behind the latest. Anything unparsable isn't.
func older(have, latest string) bool {
	h, l := versionParts(have), versionParts(latest)
	return h != nil && l != nil && slices.Compare(h, l) < 0
}

func versionParts(v string) []int {
	if v == "" {
		return nil
	}
	var out []int
	for _, p := range strings.Split(strings.TrimPrefix(v, "v"), ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
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
	Asked           bool
	Err             string
	Version, Latest string
	Checked         time.Time
}

func (s Server) Behind() bool { return older(s.Version, s.Latest) }

func (w *watcher) server() Server {
	w.mu.Lock()
	defer w.mu.Unlock()
	return Server{Up: w.misses < downAfter, Asked: w.version != "" || w.misses > 0, Err: w.vwErr,
		Version: w.version, Latest: w.latest, Checked: w.checked}
}

func orDash(s string) string {
	if s == "" {
		return "an unknown address"
	}
	return s
}
