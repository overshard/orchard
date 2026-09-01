package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// State is the whole page as data. Every panel is rebuilt into this and the
// same struct is what the template renders on a cold load and what goes down
// the SSE connection on every update, so there is one shape to get right rather
// than a server one and a browser one that drift.
type State struct {
	Market    Market       `json:"market"`
	Rates     Rates        `json:"rates"`
	Sectors   []SectorCell `json:"sectors"`
	Signal    Signal       `json:"signal"`
	Earnings  []Earning    `json:"earnings"`
	Wire      []Headline   `json:"wire"`
	HN        []Story      `json:"hn"`
	Lobsters  []Story      `json:"lobsters"`
	Weather   Weather      `json:"weather"`
	Air       Air          `json:"air"`
	Alerts    []Alert      `json:"alerts"`
	Outlook   Outlook      `json:"outlook"`
	Steam     []Game       `json:"steam"`
	Streaming []Title      `json:"streaming"`
	Systems   Systems      `json:"systems"`
	Feeds     []Feed       `json:"feeds"`
	Updated   string       `json:"updated"`
	Guarded   []string     `json:"guarded"`
}

// Store holds the latest of everything and hands out snapshots. Each poller
// writes its own field, so the lock is only ever held for a struct copy.
type Store struct {
	mu    sync.RWMutex
	state State

	// The daily series the conditions panel reads, kept here because the market
	// poll rebuilds that panel every thirty seconds and this only refreshes
	// hourly.
	history *history

	hub *Hub
}

func NewStore(hub *Hub) *Store { return &Store{hub: hub} }

func (s *Store) Snapshot() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// update applies one change and pushes the whole state out. Taking a mutator
// rather than a field keeps every write on one path, which is what makes the
// broadcast unconditional and impossible to forget.
func (s *Store) update(f func(*State)) {
	s.mu.Lock()
	f(&s.state)
	s.state.Updated = time.Now().UTC().Format(time.RFC3339)
	snapshot := s.state
	s.mu.Unlock()

	b, err := json.Marshal(snapshot)
	if err != nil {
		slog.Error("state unmarshalable", slog.Any("err", err))
		return
	}
	s.hub.Broadcast(b)
}

// Hub is the SSE fan-out. One poller writes and every open browser reads, so
// ten tabs on this page still cost one request upstream.
type Hub struct {
	mu      sync.Mutex
	clients map[chan []byte]struct{}
	last    []byte

	// Closed on nothing; sent to when the first browser of a quiet spell
	// connects. The markets loop waits on it alongside its timer, so somebody
	// opening the page does not have to sit out the rest of an idle interval.
	// Buffered by one and sent without blocking, so an arrival while the loop
	// is mid-fetch is remembered rather than lost.
	wake chan struct{}
}

func NewHub() *Hub {
	return &Hub{
		clients: map[chan []byte]struct{}{},
		wake:    make(chan struct{}, 1),
	}
}

// Subscribe returns a channel of frames and the function that closes it. The
// buffer is small: a browser that cannot keep up with a state this size is
// gone, and dropping the frame is better than holding the broadcast for it.
func (h *Hub) Subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, 4)

	h.mu.Lock()
	first := len(h.clients) == 0
	h.clients[ch] = struct{}{}
	last := h.last
	h.mu.Unlock()

	if first {
		select {
		case h.wake <- struct{}{}:
		default:
		}
	}

	// A new connection gets the current state at once rather than waiting out
	// the poll interval, which is what makes a reconnect invisible.
	if last != nil {
		ch <- last
	}

	return ch, func() {
		h.mu.Lock()
		if _, ok := h.clients[ch]; ok {
			delete(h.clients, ch)
			close(ch)
		}
		h.mu.Unlock()
	}
}

func (h *Hub) Broadcast(b []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.last = b
	for ch := range h.clients {
		select {
		case ch <- b:
		default:
		}
	}
}

func (h *Hub) Watching() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// Poll intervals. Markets move and the rest do not, so only markets gets a fast
// one, and it only gets it while somebody is looking.
const (
	marketWatched = 30 * time.Second

	// Yahoo's edge sends cache-control: max-age=10 on these responses, so
	// anything under about ten seconds is asking for bytes it already has.
	// Thirty is comfortably past that and still live enough to watch.

	marketIdle = 5 * time.Minute
	newsEvery  = 5 * time.Minute
)

// Prime fetches what the first page render needs, synchronously, before the
// server starts listening.
//
// The daily history comes first because the conditions panel is built by the
// market poll out of it, and priming them the other way round renders a panel
// with one of its four rows for the first thirty seconds after every deploy.
func (s *Store) Prime(ctx context.Context, g *Guard) {
	s.refreshSignal(ctx, g)
	s.refreshMarket(ctx, g)
}

// Run starts one goroutine per source and blocks until ctx is done. It must not
// start before the listener is up: the health strip probes this process over
// loopback, and a first round against a socket nobody is listening on reports
// the site the dashboard is running on as unknown until the next round a minute
// later, which is every deploy.
func (s *Store) Run(ctx context.Context, g *Guard) {
	go s.loop(ctx, "market", func() time.Duration {
		if s.hub.Watching() > 0 {
			return marketWatched
		}
		// Nobody is on the page, so this drops to a keep-warm poll. It does not
		// stop outright because the first visitor after a quiet night should
		// open onto real numbers, not a spinner.
		return marketIdle
	}, func() { s.refreshMarket(ctx, g) })

	go s.loop(ctx, "news", nil, func() { s.refreshNews(ctx, g) })
	go s.loop(ctx, "wire", nil, func() { s.refreshWire(ctx, g) })
	go s.loop(ctx, "signal", nil, func() { s.refreshSignal(ctx, g) })
	go s.loop(ctx, "board", nil, func() { s.refreshBoard(ctx, g) })
	go s.loop(ctx, "earnings", nil, func() { s.refreshEarnings(ctx, g) })
	go s.loop(ctx, "alerts", nil, func() { s.refreshAlerts(ctx, g) })
	go s.loop(ctx, "air", nil, func() { s.refreshAir(ctx, g) })
	go s.loop(ctx, "outdoors", nil, func() { s.refreshOutlook(ctx, g) })
	go s.loop(ctx, "steam", nil, func() { s.refreshSteam(ctx, g) })
	go s.loop(ctx, "streaming", nil, func() { s.refreshStreaming(ctx, g) })
	go s.loop(ctx, "weather", nil, func() { s.refreshWeather(ctx, g) })
	go s.loop(ctx, "systems", nil, func() { s.refreshSystems(ctx, g) })

	// The guard writes itself out on a timer rather than on every call, so a
	// thirty second poll does not mean a file write per tick.
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				g.Flush()
				return
			case <-t.C:
				g.Flush()
				s.update(func(st *State) {
					st.Guarded = g.Status()
					st.Feeds = g.Feeds(time.Now())
				})
			}
		}
	}()

	<-ctx.Done()
}

// loop runs work on an interval. every is a function so the markets loop can
// change its mind about the cadence between ticks; a nil one means use the
// fixed interval the source was registered with.
//
// The markets loop also wakes when the first browser of a quiet spell connects.
// Without it a visitor arriving a second after an idle timer was set would wait
// out the whole five minutes watching a page that says it is live.
func (s *Store) loop(ctx context.Context, name string, every func() time.Duration, work func()) {
	fixed := map[string]time.Duration{
		"news":      newsEvery,
		"wire":      marketNewsEvery,
		"signal":    signalEvery,
		"board":     boardEvery,
		"earnings":  earningsEvery,
		"alerts":    alertsEvery,
		"air":       airEvery,
		"outdoors":  outdoorsEvery,
		"steam":     steamEvery,
		"streaming": streamingEvery,
		"weather":   weatherEvery,
		"systems":   probeEvery,
	}[name]

	if every == nil {
		every = func() time.Duration { return fixed }
	}

	var wake <-chan struct{}
	if name == "market" {
		wake = s.hub.wake
	}

	// The first run is immediate for everything Prime did not already do.
	last := time.Now()
	if name != "market" && name != "signal" {
		work()
	}

	for {
		t := time.NewTimer(every())
		select {
		case <-ctx.Done():
			t.Stop()
			return

		case <-t.C:
			work()
			last = time.Now()

		case <-wake:
			t.Stop()
			// A viewer arriving right after a fetch does not need another one,
			// and the hub has already replayed the current state to them.
			if time.Since(last) >= marketWatched {
				work()
				last = time.Now()
			}
		}
	}
}

func (s *Store) refreshMarket(ctx context.Context, g *Guard) {
	quotes, err := fetchQuotes(ctx, g, sparkSymbols())
	if err != nil {
		slog.Warn("market poll failed", slog.String("component", "market"), slog.Any("err", err))
		// The previous cards stay up rather than being blanked, and the panel
		// marks itself stale so the page says so instead of quietly lying.
		s.update(func(st *State) { st.Market.Stale = true })
		return
	}
	m := buildMarket(quotes, time.Now())
	m.Updated = time.Now().UTC().Format("15:04:05")
	m = carrySparks(m, s.Snapshot().Market)

	s.mu.RLock()
	h := s.history
	s.mu.RUnlock()

	sig := buildSignal(h, quotes)

	s.update(func(st *State) {
		st.Market = m
		st.Signal = sig
	})
}

// refreshBoard is the rates and sectors poll. Neither moves on the scale the
// market strip does, and riding the fast poll cost a third of every Yahoo
// request this site makes for numbers that had not changed.
func (s *Store) refreshBoard(ctx context.Context, g *Guard) {
	quotes, err := fetchQuotes(ctx, g, rateAndSectorSymbols())
	if err != nil {
		slog.Warn("board poll failed", slog.String("component", "board"), slog.Any("err", err))
		return
	}

	rates := buildRates(quotes)
	sectors := buildSectors(quotes)
	s.update(func(st *State) {
		st.Rates = rates
		st.Sectors = sectors
	})
}

func (s *Store) refreshSignal(ctx context.Context, g *Guard) {
	h, err := fetchHistory(ctx, g, signalSymbol)
	if err != nil {
		slog.Warn("history poll failed", slog.String("component", "signal"), slog.Any("err", err))
		return
	}
	s.mu.Lock()
	s.history = h
	s.mu.Unlock()
}

func (s *Store) refreshEarnings(ctx context.Context, g *Guard) {
	rows, err := fetchEarnings(ctx, g, time.Now())
	if err != nil {
		slog.Warn("earnings poll failed", slog.String("component", "earnings"), slog.Any("err", err))
		return
	}
	s.update(func(st *State) { st.Earnings = rows })
}

// An empty alert list is a result, not a failure, so this writes it: the panel
// has to be able to say all clear rather than showing yesterday's warning.
func (s *Store) refreshAlerts(ctx context.Context, g *Guard) {
	alerts, err := fetchAlerts(ctx, g)
	if err != nil {
		slog.Warn("alerts poll failed", slog.String("component", "local"), slog.Any("err", err))
		return
	}
	s.update(func(st *State) { st.Alerts = alerts })
}

func (s *Store) refreshAir(ctx context.Context, g *Guard) {
	air, err := fetchAir(ctx, g)
	if err != nil {
		slog.Warn("air poll failed", slog.String("component", "local"), slog.Any("err", err))
		return
	}

	// Pollen is the unofficial source here, so it losing its footing costs its
	// own row and not the air quality beside it.
	if index, top, err := fetchPollen(ctx, g); err != nil {
		slog.Warn("pollen poll failed", slog.String("component", "local"), slog.Any("err", err))
	} else {
		air.Pollen = fmt.Sprintf("%.1f", index)
		air.PollenState = pollenBand(index)
		air.PollenTop = top
		air.PollenKnown = true
		air.PollenFill, air.PollenLevel = gauge(index, 9.7, 2.5, 4.9, 7.3)
	}

	s.update(func(st *State) { st.Air = air })
}

func (s *Store) refreshStreaming(ctx context.Context, g *Guard) {
	titles, err := fetchStreaming(ctx, g)
	if err != nil {
		slog.Warn("streaming poll failed", slog.String("component", "streaming"), slog.Any("err", err))
		return
	}
	s.update(func(st *State) { st.Streaming = titles })
}

func (s *Store) refreshOutlook(ctx context.Context, g *Guard) {
	outlook, err := fetchOutlook(ctx, g, time.Now())
	if err != nil {
		slog.Warn("outlook poll failed", slog.String("component", "local"), slog.Any("err", err))
		return
	}
	s.update(func(st *State) { st.Outlook = outlook })
}

func (s *Store) refreshSteam(ctx context.Context, g *Guard) {
	games, err := fetchSteam(ctx, g)
	if err != nil {
		slog.Warn("steam poll failed", slog.String("component", "steam"), slog.Any("err", err))
		return
	}
	s.update(func(st *State) { st.Steam = games })
}

func (s *Store) refreshWire(ctx context.Context, g *Guard) {
	wire, err := fetchMarketNews(ctx, g, time.Now())
	if err != nil {
		slog.Warn("market news poll failed", slog.String("component", "wire"), slog.Any("err", err))
		return
	}
	s.update(func(st *State) { st.Wire = wire })
}

func (s *Store) refreshNews(ctx context.Context, g *Guard) {
	now := time.Now()

	if stories, err := fetchHackerNews(ctx, g, now); err != nil {
		slog.Warn("hacker news poll failed", slog.String("component", "news"), slog.Any("err", err))
	} else {
		s.update(func(st *State) { st.HN = stories })
	}

	if stories, err := fetchLobsters(ctx, g, now); err != nil {
		slog.Warn("lobsters poll failed", slog.String("component", "news"), slog.Any("err", err))
	} else {
		s.update(func(st *State) { st.Lobsters = stories })
	}
}

func (s *Store) refreshWeather(ctx context.Context, g *Guard) {
	w, err := fetchWeather(ctx, g)
	if err != nil {
		slog.Warn("weather poll failed", slog.String("component", "weather"), slog.Any("err", err))
		return
	}
	s.update(func(st *State) { st.Weather = w })
}

func (s *Store) refreshSystems(ctx context.Context, g *Guard) {
	sys := buildSystems(ctx, g, time.Now())
	// Guarded and Feeds are the same fact told two ways, in the status line and
	// in the SIGNAL panel, so they are written together or the page contradicts
	// itself for up to a minute.
	s.update(func(st *State) {
		st.Systems = sys
		st.Feeds = g.Feeds(time.Now())
		st.Guarded = g.Status()
	})
}
