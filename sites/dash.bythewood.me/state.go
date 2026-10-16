package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// State is the whole page as data. The template renders it on a cold load and
// the SSE connection sends it on every update, so there's one shape to get
// right instead of a server one and a browser one that drift.
type State struct {
	Market   Market       `json:"market"`
	Rates    Rates        `json:"rates"`
	Sectors  []SectorCell `json:"sectors"`
	Signal   Signal       `json:"signal"`
	Earnings Earnings     `json:"earnings"`
	Briefs   Briefs       `json:"briefs"`
	HN       []Story      `json:"hn"`
	Lobsters []Story      `json:"lobsters"`

	HNPulse       Pulse      `json:"hn_pulse"`
	LobstersPulse Pulse      `json:"lobsters_pulse"`
	Weather       Weather    `json:"weather"`
	Air           Air        `json:"air"`
	Alerts        []Alert    `json:"alerts"`
	OnAir         OnAir      `json:"onair"`
	Outlook       Outlook    `json:"outlook"`
	Steam         []Game     `json:"steam"`
	SteamUsual    string     `json:"steam_usual"`
	Streaming     []Title    `json:"streaming"`
	Systems       Systems    `json:"systems"`
	Feeds         []Feed     `json:"feeds"`
	Updated       string     `json:"updated"`
	Guarded       []string   `json:"guarded"`
	Ahead         []Upcoming `json:"ahead"`
	NextBrief     string     `json:"next_brief"`

	// What a browser with notifications on is told about, rebuilt by each
	// poller from what it just fetched.
	Notices []Notice `json:"notices"`
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

	// His archive and schedule as last fetched, which every banner poll
	// rebuilds the ON AIR panel from.
	past *twitchPast

	// The BLS and BEA calendars, which the footer reads every minute and the
	// markets brief reads four times a day.
	releases []release

	// Which game held the top of Steam each day, nil in tests.
	chart *steamChart

	// The top comments on each feed row, nil in tests.
	comments *commentBook

	hub *Hub
}

func NewStore(hub *Hub) *Store { return &Store{hub: hub, comments: newCommentBook()} }

func (s *Store) Snapshot() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// update applies one change and pushes the whole state out. Taking a mutator
// keeps every write on one path, so the broadcast can't be forgotten.
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

	// Sent to without blocking when the first browser of a quiet spell
	// connects, so the idle source doesn't make them sit out its interval.
	// Buffered by one so an arrival mid-fetch isn't lost.
	wake chan struct{}
}

func NewHub() *Hub {
	return &Hub{
		clients: map[chan []byte]struct{}{},
		wake:    make(chan struct{}, 1),
	}
}

// Subscribe returns a channel of frames and the function that closes it. A
// browser that can't keep up with a state this size is gone, so a full buffer
// drops the frame instead of holding the broadcast.
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

	// A new connection gets the current state at once, so a reconnect doesn't
	// wait out a poll interval.
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

	// Both calendars are set months ahead, so twice a day only catches a
	// rescheduled release.
	calendarEvery = 12 * time.Hour
	aheadCount    = 4

	// The check between full earnings polls, which only asks whether anything
	// has printed since the last one, so a report is heard about within the
	// quarter hour instead of up to six hours later.
	reportsEvery = 15 * time.Minute
)

// Prime fetches what the first render needs before the server listens. The
// history goes first since the market poll builds the conditions panel from it,
// or the panel starts a row short after every deploy.
func (s *Store) Prime(ctx context.Context, g *Guard) {
	s.refreshSignal(ctx, g)
	s.refreshMarket(ctx, g)
	s.update(func(st *State) { st.NextBrief = nextBrief(time.Now()) })
}

// awaitFirstRound holds until the pollers Prime doesn't run have filled in,
// since a slot owed at start would otherwise write the glance without weather
// or Steam. A source that never answers only costs the wait.
func (s *Store) awaitFirstRound(ctx context.Context, most time.Duration) {
	ctx, cancel := context.WithTimeout(ctx, most)
	defer cancel()
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		st := s.Snapshot()
		if (st.Weather.Condition != "" || st.Weather.Unavailable) && len(st.Steam) > 0 && len(st.HN) > 0 {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Run starts one goroutine per source and blocks until ctx is done. It has to
// wait for the listener, since the health strip probes this process over
// loopback and would report it unknown until the next round.
func (s *Store) Run(ctx context.Context, g *Guard) {
	for _, src := range sources {
		go s.loop(ctx, g, src)
	}

	// The guard writes itself out on a timer instead of on every call, so a
	// thirty second poll isn't a file write per tick.
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
				s.mu.RLock()
				rs := s.releases
				s.mu.RUnlock()
				s.update(func(st *State) {
					st.Guarded = g.Status()
					st.Feeds = g.Feeds(time.Now())
					st.Ahead = ahead(rs, time.Now(), aheadCount)
					st.NextBrief = nextBrief(time.Now())
				})
			}
		}
	}()

	<-ctx.Done()
}

func (s *Store) refreshMarket(ctx context.Context, g *Guard) {
	quotes, err := fetchStrip(ctx, g)
	if err != nil {
		pollFailed(ctx, "market", "market", err)
		// The previous cards stay up and the panel marks itself stale, so the
		// page says so instead of quietly lying.
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
		st.setNotices("market", m.notices)
	})
}

// refreshBoard is the rates and sectors poll, kept off the fast market poll
// since neither moves on that scale.
func (s *Store) refreshBoard(ctx context.Context, g *Guard) {
	quotes, err := fetchQuotes(ctx, g, rateAndSectorSymbols(), sessionRange)
	if err != nil {
		pollFailed(ctx, "board", "board", err)
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
		pollFailed(ctx, "history", "signal", err)
		return
	}
	s.mu.Lock()
	s.history = h
	s.mu.Unlock()
}

func (s *Store) refreshEarnings(ctx context.Context, g *Guard) {
	known := s.Snapshot().Earnings.recent
	rows, err := fetchEarnings(ctx, g, time.Now())
	if err != nil {
		pollFailed(ctx, "earnings", "earnings", err)
		return
	}
	logReports(changedReports(rows.recent, known))
	s.update(func(st *State) {
		st.Earnings = rows
	})
}

// refreshReports asks whether anything printed or was revised since the last
// full poll, at two requests against up to twenty, and runs the full poll only
// when something has.
func (s *Store) refreshReports(ctx context.Context, g *Guard) {
	now := time.Now().In(easternTime())
	e := s.Snapshot().Earnings
	if e.caps == nil || !reportingHours(now) {
		return
	}
	day := func(d time.Time) ([]Earning, bool) { return earningsDay(ctx, g, d, now, e.caps) }
	if len(changedReports(recentReports(day, now), e.recent)) > 0 {
		s.refreshEarnings(ctx, g)
	}
}

func reportingHours(t time.Time) bool {
	if wd := t.Weekday(); wd == time.Saturday || wd == time.Sunday {
		return false
	}
	return t.Hour() >= 6 && t.Hour() < 20
}

// An empty alert list is a result and not a failure, so it's written and the
// panel says all clear instead of showing yesterday's warning.
func (s *Store) refreshAlerts(ctx context.Context, g *Guard) {
	alerts, err := fetchAlerts(ctx, g)
	if err != nil {
		pollFailed(ctx, "alerts", "local", err)
		return
	}
	s.update(func(st *State) {
		st.Alerts = alerts
		st.setNotices("weather", alertNotices(alerts))
	})
}

func (s *Store) refreshOnAir(ctx context.Context, g *Guard) {
	now := time.Now()
	fresh, err := pickOnAir(
		func() (OnAir, error) { return fetchYouTube(ctx, g) },
		func() (OnAir, error) { return fetchTwitch(ctx, g) },
	)
	if err != nil {
		pollFailed(ctx, "on air", "onair", err)
		return
	}
	s.mu.RLock()
	past := s.past
	s.mu.RUnlock()

	s.update(func(st *State) {
		o := carryOnAir(fresh, st.OnAir, now)
		o.Broadcasts = st.OnAir.Broadcasts
		if past != nil {
			o.Broadcasts = buildBroadcasts(*past, o, now)
		}
		st.OnAir = o
		st.setNotices("live", onAirNotices(st.OnAir))
	})
}

// refreshBroadcasts keeps the last good archive when Twitch fails, and the
// banner goes on without it.
func (s *Store) refreshBroadcasts(ctx context.Context, g *Guard) {
	past, err := fetchBroadcasts(ctx, g, twitchGQL)
	if err != nil {
		pollFailed(ctx, "broadcasts", "onair", err)
		return
	}
	s.mu.Lock()
	s.past = &past
	s.mu.Unlock()

	now := time.Now()
	s.update(func(st *State) { st.OnAir.Broadcasts = buildBroadcasts(past, st.OnAir, now) })
}

func (s *Store) refreshAir(ctx context.Context, g *Guard) {
	air, err := fetchAir(ctx, g)
	if err != nil {
		pollFailed(ctx, "air", "local", err)
		return
	}

	// Pollen is the unofficial source here, so it losing its footing costs its
	// own row and not the air quality beside it.
	if index, top, err := fetchPollen(ctx, g); err != nil {
		pollFailed(ctx, "pollen", "local", err)
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
		pollFailed(ctx, "streaming", "streaming", err)
		return
	}
	s.update(func(st *State) { st.Streaming = titles })
}

func (s *Store) refreshOutlook(ctx context.Context, g *Guard) {
	outlook, err := fetchOutlook(ctx, g, time.Now())
	if err != nil {
		pollFailed(ctx, "outlook", "local", err)
		return
	}
	s.update(func(st *State) { st.Outlook = outlook })
}

func (s *Store) refreshSteam(ctx context.Context, g *Guard) {
	games, err := fetchSteam(ctx, g)
	if err != nil {
		pollFailed(ctx, "steam", "steam", err)
		return
	}
	s.update(func(st *State) {
		if keepSteam(games, st.Steam) {
			st.Steam = games
		}
		if s.chart != nil {
			st.SteamUsual = s.chart.record(st.Steam, time.Now())
		}
	})
}

func (s *Store) refreshNews(ctx context.Context, g *Guard) {
	now := time.Now()

	if stories, pulse, err := fetchHackerNews(ctx, g, now); err != nil {
		pollFailed(ctx, "hacker news", "news", err)
	} else {
		s.update(func(st *State) {
			st.HN, st.HNPulse = stories, pulse
			st.attachFeedReads()
			s.comments.attach(st.HN)
		})
	}

	if stories, pulse, err := fetchLobsters(ctx, g, now); err != nil {
		pollFailed(ctx, "lobsters", "news", err)
	} else {
		s.update(func(st *State) {
			st.Lobsters, st.LobstersPulse = stories, pulse
			st.attachFeedReads()
			s.comments.attach(st.Lobsters)
		})
	}

	s.refreshComments(ctx, g)
}

func (s *Store) refreshWeather(ctx context.Context, g *Guard) {
	w, err := fetchWeather(ctx, g)
	if err != nil {
		pollFailed(ctx, "weather", "weather", err)
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

func (s *Store) refreshCalendar(ctx context.Context, g *Guard) {
	rs := fetchReleases(ctx, g)
	if len(rs) == 0 {
		return
	}
	s.mu.Lock()
	s.releases = rs
	s.mu.Unlock()
	s.update(func(st *State) { st.Ahead = ahead(rs, time.Now(), aheadCount) })
}

func (s *Store) releasesOrFetch(ctx context.Context, g *Guard) []release {
	s.mu.RLock()
	rs := s.releases
	s.mu.RUnlock()
	if len(rs) == 0 {
		s.refreshCalendar(ctx, g)
		s.mu.RLock()
		rs = s.releases
		s.mu.RUnlock()
	}
	return rs
}

// pollFailed stays quiet when the poll only failed because the site is
// shutting down, or every deploy writes a warning per poller.
func pollFailed(ctx context.Context, what, component string, err error) {
	if ctx.Err() != nil {
		return
	}
	slog.Warn(what+" poll failed", slog.String("component", component), slog.Any("err", err))
}
