package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// The desktop on its own sits around 10 to 40% and 1.6GB of the 3070 with a
// video playing, and a game takes most of both. 3000MiB is also about where
// Ornith's 7.3GB stops fitting beside it.
const (
	busyUtil = 55
	busyMiB  = 3000
)

// With our model resident a game beside it fills the card. WDDM holds a few
// hundred MiB back, so full means within this much of the top.
const fullHeadroomMiB = 512

// nvidia-smi can hitch a game, so a run of background calls shares a reading.
const probeTTL = 30 * time.Second

// A start is usually a reboot, and the game comes back a few minutes after the
// containers do, so background calls wait this long before the card is read.
const startupGrace = 15 * time.Minute

// What a background caller is told to wait when nothing says how long.
const busyRetry = time.Hour

// backgroundHeader marks a call nobody is waiting on, like a scheduled brief.
// Those are turned away while the desktop has the card and anything else is
// somebody asking, who gets the model regardless.
const backgroundHeader = "X-Background"

type gpuReading struct {
	UtilAvg  int `json:"util_avg"`
	UtilMax  int `json:"util_max"`
	UsedMiB  int `json:"used_mib"`
	TotalMiB int `json:"total_mib"`
}

type gpuAnswer struct {
	Busy    bool   `json:"busy"`
	Reason  string `json:"reason"`
	Loaded  bool   `json:"loaded"`
	Gaming  bool   `json:"gaming"`
	RetryAt int64  `json:"retry_at,omitempty"`
	gpuReading
}

// cardGate is what the gateway knows about the card between calls. Its zero
// value is a gate with no switch, no grace and nothing read yet.
type cardGate struct {
	mu          sync.Mutex
	reading     gpuReading
	readAt      time.Time
	readBusy    bool
	gamingUntil time.Time

	path    string
	started time.Time

	inflight   atomic.Int32
	background atomic.Bool
}

// judge decides from a reading taken with none of our models resident.
func judge(r gpuReading) (bool, string) {
	switch {
	case r.UtilAvg >= busyUtil:
		return true, fmt.Sprintf("card at %d%%", r.UtilAvg)
	case r.UsedMiB >= busyMiB:
		return true, fmt.Sprintf("%d MiB of %d in use", r.UsedMiB, r.TotalMiB)
	}
	return false, "card idle"
}

// judgeLoaded reads the card with our model on it, where most of the memory is
// ours. A busy card with none of our calls running is the desktop's work.
func judgeLoaded(r gpuReading, ours bool) (bool, string) {
	switch {
	case r.TotalMiB > 0 && r.UsedMiB >= r.TotalMiB-fullHeadroomMiB:
		return true, fmt.Sprintf("card full at %d MiB of %d", r.UsedMiB, r.TotalMiB)
	case !ours && r.UtilAvg >= busyUtil:
		return true, fmt.Sprintf("card at %d%% with none of our calls running", r.UtilAvg)
	}
	return false, "a model is on the card and there's room"
}

// cardState is what a background caller is told. The switch and the startup
// grace answer without touching nvidia-smi.
func (s *site) cardState(ctx context.Context, now time.Time) (gpuAnswer, error) {
	var out gpuAnswer
	if until := s.card.gaming(now); !until.IsZero() {
		out.Busy, out.Gaming, out.RetryAt, out.Reason = true, true, until.Unix(), "gaming switch is on"
		return out, nil
	}
	if !s.card.started.IsZero() && now.Before(s.card.started.Add(startupGrace)) {
		out.Busy, out.RetryAt, out.Reason = true, s.card.started.Add(startupGrace).Unix(), "the gateway just started"
		return out, nil
	}

	var running struct {
		Running []swapModel `json:"running"`
	}
	if err := s.swap(ctx, swapRunning, &running); err != nil {
		return out, fmt.Errorf("asking what is loaded: %w", err)
	}
	for _, m := range running.Running {
		out.Loaded = out.Loaded || m.State != "stopping"
	}

	reading, oursThen, err := s.read(ctx, now)
	if err != nil {
		return out, err
	}
	out.gpuReading = reading
	if out.Loaded {
		out.Busy, out.Reason = judgeLoaded(reading, oursThen || s.card.inflight.Load() > 0)
	} else {
		out.Busy, out.Reason = judge(reading)
	}
	return out, nil
}

// read hands back a recent reading, probing when the last one is stale. It also
// says whether one of our calls was running while it was taken.
func (s *site) read(ctx context.Context, now time.Time) (gpuReading, bool, error) {
	g := &s.card
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.readAt.IsZero() && now.Sub(g.readAt) < probeTTL {
		return g.reading, g.readBusy, nil
	}
	before := g.inflight.Load()
	r, err := s.probeGPU(ctx)
	if err != nil {
		return r, false, err
	}
	g.reading, g.readAt, g.readBusy = r, now, before > 0 || g.inflight.Load() > 0
	return g.reading, g.readBusy, nil
}

// gpu tells a background caller whether loading a model now would land on top
// of whatever the desktop is doing. It loads nothing and isn't logged.
func (s *site) gpu(w http.ResponseWriter, r *http.Request, _ Key) {
	out, err := s.cardState(r.Context(), time.Now())
	if err != nil {
		slog.Error("reading the card", "err", err)
		http.Error(w, "the model server or the gpu probe is not answering", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(out)
}

// refuseBackground answers a background call with 503 when the desktop has the
// card, and takes off a model a background caller left there. It says whether
// it answered.
func (s *site) refuseBackground(w http.ResponseWriter, r *http.Request, k Key) bool {
	now := time.Now()
	st, err := s.cardState(r.Context(), now)
	if err != nil {
		// The caller asked /v1/gpu before its run, so a probe that can't answer
		// mid run lets the call through.
		slog.Warn("background gate could not read the card", "err", err, "caller", k.Name)
		return false
	}
	if !st.Busy {
		return false
	}
	if st.Loaded && s.card.inflight.Load() == 0 && s.card.background.Load() {
		if err := s.swap(context.WithoutCancel(r.Context()), swapUnload, nil); err != nil {
			slog.Error("unloading a background model for the desktop", "err", err)
		} else {
			slog.Info("unloaded a background model for the desktop", "caller", k.Name, "reason", st.Reason)
		}
	}
	retry := busyRetry
	if st.RetryAt > 0 {
		retry = time.Unix(st.RetryAt, 0).Sub(now)
	}
	slog.Info("turned a background call away", "caller", k.Name, "reason", st.Reason)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Retry-After", strconv.Itoa(max(1, int(retry.Seconds()))))
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{
		"message": "the desktop is using the card: " + st.Reason, "type": "card_busy",
	}})
	return true
}

// gaming says until when the switch is on, or zero when it's off.
func (g *cardGate) gaming(now time.Time) time.Time {
	g.mu.Lock()
	defer g.mu.Unlock()
	if now.Before(g.gamingUntil) {
		return g.gamingUntil
	}
	return time.Time{}
}

// setGaming turns the switch on until a time, or off with the zero time. It's
// kept in a file beside the database so a reboot mid game doesn't drop it.
func (g *cardGate) setGaming(until time.Time) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.gamingUntil = until
	if g.path == "" {
		return nil
	}
	if until.IsZero() {
		if err := os.Remove(g.path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	tmp := g.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strconv.FormatInt(until.Unix(), 10)), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, g.path)
}

func (g *cardGate) loadGaming(dir string) {
	g.path = filepath.Join(dir, "gaming")
	raw, err := os.ReadFile(g.path)
	if err != nil {
		return
	}
	if n, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64); err == nil {
		g.gamingUntil = time.Unix(n, 0)
	}
}

// gamingSet is the switch on the overview. Turning it on also takes the model
// off now, since it means he's about to use the card.
func (s *site) gamingSet(w http.ResponseWriter, r *http.Request) {
	hours, _ := strconv.Atoi(r.FormValue("hours"))
	hours = min(max(hours, 0), 12)
	var until time.Time
	if hours > 0 {
		until = time.Now().Add(time.Duration(hours) * time.Hour)
	}
	if err := s.card.setGaming(until); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	slog.Info("gaming switch", "hours", hours)
	if hours > 0 {
		if err := s.swap(r.Context(), swapUnload, nil); err != nil {
			slog.Error("unloading for the gaming switch", "err", err)
		}
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *site) probeGPU(ctx context.Context) (gpuReading, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var r gpuReading
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.gpuProbe+"/gpu", nil)
	if err != nil {
		return r, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return r, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return r, fmt.Errorf("the gpu probe answered %d", resp.StatusCode)
	}
	return r, json.NewDecoder(io.LimitReader(resp.Body, 4<<10)).Decode(&r)
}
