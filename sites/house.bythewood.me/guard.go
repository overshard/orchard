package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Every fact on a card comes from somebody else's free server: FEMA, NCDOT, the
// Census geocoder, OSRM, a county GIS box that is one machine in a county
// building. A ban on any of them is hard to undo, so the guard goes in front of
// the client rather than on top of it.
//
// It paces requests with jitter, counts failures and opens a breaker whose state
// lives in SQLite so a restart does not hand a struggling upstream a fresh round,
// backs off further each time the breaker reopens, honours 429 and 503 and
// Retry-After in both formats, and keeps a hard per-window budget as the backstop
// for a bug that would otherwise loop.
type Guard struct {
	db   *sql.DB
	name string

	minInterval time.Duration
	budget      int
	window      time.Duration
	tripAfter   int
	openFor     time.Duration
	userAgent   string

	mu       sync.Mutex
	lastCall time.Time
	client   *http.Client
	rand     *rand.Rand
}

// GuardOpts keeps the per-endpoint numbers in one place at the call site, since
// a county ArcGIS box and the Census geocoder do not deserve the same pace.
type GuardOpts struct {
	MinInterval time.Duration
	Budget      int
	Window      time.Duration
	TripAfter   int
	OpenFor     time.Duration
	Timeout     time.Duration

	// Empty means the browser string. Set it to clientUA for an API that would
	// rather know who is calling, which some of them enforce.
	UserAgent string
}

func NewGuard(db *sql.DB, name string, o GuardOpts) *Guard {
	if o.MinInterval == 0 {
		o.MinInterval = 500 * time.Millisecond
	}
	if o.Budget == 0 {
		o.Budget = 2000
	}
	if o.Window == 0 {
		o.Window = time.Hour
	}
	if o.TripAfter == 0 {
		o.TripAfter = 5
	}
	if o.OpenFor == 0 {
		o.OpenFor = 10 * time.Minute
	}
	if o.Timeout == 0 {
		o.Timeout = 20 * time.Second
	}
	if o.UserAgent == "" {
		o.UserAgent = browserUA
	}
	return &Guard{
		db:   db,
		name: name,
		// Seeded per guard rather than from the global source, so two guards
		// created in the same millisecond do not jitter in lockstep.
		rand:        rand.New(rand.NewSource(time.Now().UnixNano() + int64(len(name)))),
		minInterval: o.MinInterval,
		budget:      o.Budget,
		window:      o.Window,
		tripAfter:   o.TripAfter,
		openFor:     o.OpenFor,
		userAgent:   o.UserAgent,
		client:      &http.Client{Timeout: o.Timeout},
	}
}

// Two user agents, because the two kinds of upstream here want opposite things.
//
// A public records server usually sits behind a WAF that answers 403 to anything
// that does not look like a browser, and the DHSR roster does exactly that. An
// API meant for programs wants the opposite, and Overpass answers 406 Not
// Acceptable to a browser string.
//
// So it is per endpoint. The browser string is the default because more of these
// are WAF'd records servers than APIs.
const browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// An honest one, with somewhere to write to. Services that publish usage terms
// ask for this and are entitled to it.
const clientUA = "house.bythewood.me/1.0 (self-hosted house search; isaac@bythewood.me)"

// ErrGuarded is returned without a request being sent. It is a distinct error so
// a caller can tell "the upstream said no" from "we did not ask".
type ErrGuarded struct {
	Endpoint string
	Why      string
}

func (e *ErrGuarded) Error() string { return e.Endpoint + " guarded: " + e.Why }

type guardState struct {
	failures    int
	openedAt    int64
	windowStart int64
	windowCount int
	// How many times the breaker has opened without a clean run in between. It is
	// what makes the backoff grow, and a successful request resets it.
	trips int
}

// backoff doubles per consecutive trip and stops at eight hours. A service that
// has been down all afternoon should be asked once an hour, not six times, and
// the ceiling is there so a run the next morning is not still waiting.
func (g *Guard) backoff(trips int) time.Duration {
	const ceiling = 8 * time.Hour
	d := g.openFor
	for i := 1; i < trips && d < ceiling; i++ {
		d *= 2
	}
	if d > ceiling {
		d = ceiling
	}
	return d
}

func (g *Guard) load() (guardState, error) {
	var s guardState
	err := g.db.QueryRow(
		`SELECT failures, opened_at, window_start, window_count, trips FROM guards WHERE endpoint = ?`,
		g.name).Scan(&s.failures, &s.openedAt, &s.windowStart, &s.windowCount, &s.trips)
	if err == sql.ErrNoRows {
		return s, nil
	}
	return s, err
}

// redactURL strips a query string value that is a key. An http transport error
// stringifies the whole URL it was given, so saving the error verbatim writes an
// API key into the database and into anything that renders guard state.
var keyInURL = regexp.MustCompile(`(?i)([?&](?:api_?key|key|token|access_token)=)[^&"\s]+`)

func redactURL(s string) string {
	return keyInURL.ReplaceAllString(s, "${1}REDACTED")
}

func (g *Guard) save(s guardState, lastErr string) {
	lastErr = redactURL(lastErr)
	if _, err := g.db.Exec(`
		INSERT INTO guards (endpoint, failures, opened_at, window_start, window_count, trips, last_error)
		VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(endpoint) DO UPDATE SET
			failures = excluded.failures, opened_at = excluded.opened_at,
			window_start = excluded.window_start, window_count = excluded.window_count,
			trips = excluded.trips, last_error = excluded.last_error`,
		g.name, s.failures, s.openedAt, s.windowStart, s.windowCount, s.trips, lastErr); err != nil {
		slog.Error("guard state write failed", slog.String("endpoint", g.name), slog.Any("err", err))
	}
}

// Get sends one guarded GET and decodes JSON into out. Everything external here
// is a JSON GET, so there is one path rather than a general purpose client.
func (g *Guard) Get(ctx context.Context, url string, out any) error {
	body, err := g.GetRaw(ctx, url)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%s: decode: %w", g.name, err)
	}
	return nil
}

func (g *Guard) GetRaw(ctx context.Context, url string) ([]byte, error) {
	return g.GetWithHeaders(ctx, url, nil)
}

// GetWithHeaders is the same guarded GET with extra request headers, which is
// what an upstream that authenticates with a key needs.
func (g *Guard) GetWithHeaders(ctx context.Context, url string, headers map[string]string) ([]byte, error) {
	var body []byte
	err := g.stream(ctx, url, headers, func(r io.Reader) error {
		// 8MB is past any of these responses and short of a runaway one.
		b, err := io.ReadAll(io.LimitReader(r, 8<<20))
		body = b
		return err
	})
	return body, err
}

// Stream hands the response body to a reader rather than buying it into memory,
// for the one source here that is measured in tens of megabytes. Everything the
// guard does about pacing, budgets and backoff is the same.
func (g *Guard) Stream(ctx context.Context, url string, consume func(io.Reader) error) error {
	return g.stream(ctx, url, nil, consume)
}

func (g *Guard) stream(ctx context.Context, url string, headers map[string]string, consume func(io.Reader) error) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now()
	state, err := g.load()
	if err != nil {
		return err
	}

	if state.openedAt > 0 {
		open := time.Unix(state.openedAt, 0)
		wait := g.backoff(state.trips)
		if now.Sub(open) < wait {
			return &ErrGuarded{g.name, fmt.Sprintf("backed off, %s left",
				(wait - now.Sub(open)).Round(time.Second))}
		}
		// Half open: one request decides whether it closes or reopens. The trip
		// count is left alone, so a service that fails again waits twice as long.
		state.openedAt = 0
		state.failures = 0
	}

	if state.windowStart == 0 || now.Sub(time.Unix(state.windowStart, 0)) > g.window {
		state.windowStart = now.Unix()
		state.windowCount = 0
	}
	if state.windowCount >= g.budget {
		return &ErrGuarded{g.name, fmt.Sprintf("budget spent, %d in this %s", state.windowCount, g.window)}
	}

	// Jitter, up to a fifth of the interval on top. A fleet of identical clients
	// pacing to the same round number arrives in step, which is the shape that
	// looks like an attack from the far end.
	pace := g.minInterval + time.Duration(g.rand.Int63n(int64(g.minInterval/5)+1))
	if wait := pace - now.Sub(g.lastCall); wait > 0 && !g.lastCall.IsZero() {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	// Whichever of the two this endpoint wants. Either way it is about getting a
	// public record out of a server that would otherwise refuse one, and never
	// about getting past a site that has looked at the traffic and said no.
	req.Header.Set("User-Agent", g.userAgent)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	g.lastCall = time.Now()
	state.windowCount++

	resp, err := g.client.Do(req)
	if err != nil {
		state.failures++
		if state.failures >= g.tripAfter {
			state.openedAt = now.Unix()
			state.trips++
		}
		g.save(state, err.Error())
		return fmt.Errorf("%s: %s", g.name, redactURL(err.Error()))
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusTooManyRequests, resp.StatusCode >= 500:
		// Told to back off, so back off for real rather than retrying.
		state.failures = g.tripAfter
		state.trips++
		state.openedAt = now.Unix()

		// A Retry-After longer than the backoff wins, and it comes in two
		// formats: a number of seconds or an HTTP date. Only reading the first
		// means ignoring the second entirely and asking again immediately.
		wait := g.backoff(state.trips)
		if asked := retryAfter(resp.Header.Get("Retry-After"), now); asked > wait {
			// Stored as a LATER open time, so the existing backoff arithmetic
			// carries the longer wait without a second column for it. The other
			// way round stamps the breaker as having opened in the past and the
			// next call sails straight through.
			state.openedAt = now.Add(asked - wait).Unix()
			wait = asked
		}

		g.save(state, fmt.Sprintf("HTTP %d", resp.StatusCode))
		return fmt.Errorf("%s: HTTP %d, backing off %s", g.name, resp.StatusCode, wait.Round(time.Second))
	case resp.StatusCode != http.StatusOK:
		state.failures++
		g.save(state, fmt.Sprintf("HTTP %d", resp.StatusCode))
		return fmt.Errorf("%s: HTTP %d", g.name, resp.StatusCode)
	}

	if err := consume(resp.Body); err != nil {
		state.failures++
		g.save(state, err.Error())
		return fmt.Errorf("%s: %s", g.name, redactURL(err.Error()))
	}

	// A clean answer closes the breaker and forgives the history, so a service
	// that had a bad hour is not punished for it a week later.
	state.failures = 0
	state.trips = 0
	g.save(state, "")
	return nil
}

// retryAfter reads both formats the header is allowed to take, a number of
// seconds or an HTTP date. Reading only the first means asking again straight
// away.
func retryAfter(header string, now time.Time) time.Duration {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0
	}
	if secs, err := strconv.Atoi(header); err == nil {
		if secs < 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if when, err := http.ParseTime(header); err == nil {
		if d := when.Sub(now); d > 0 {
			return d
		}
	}
	return 0
}

// GuardStatus is what the dashboard shows, so the state of every upstream is
// visible rather than something to read logs for.
type GuardStatus struct {
	Endpoint  string
	Failures  int
	Open      bool
	OpenUntil time.Time
	Used      int
	LastError string
}

// Who each endpoint actually is. The footer is on a dashboard two people open on
// a phone, so a row reading "nc-parcels, 3 failures" is a developer's panel
// wearing a family's clothes.
var guardNames = map[string]string{
	"census-geocoder": "US Census, for turning an address into a place on the map",
	"nc-parcels":      "NC OneMap, for the lot and what the county values it at",
	"gis-alexander":   "Alexander County schools",
	"gis-caldwell":    "Caldwell County schools",
	"nc-schools":      "NC schools, statewide",
	"ncdot-aadt":      "NC transport, for how busy the road is",
	"fema-nfhl":       "FEMA, for the flood map",
	"usgs-nhd":        "US Geological Survey, for creeks and rivers",
	"usgs-3dep":       "US Geological Survey, for how flat the yard is",
	"acs-census":      "US Census, for who lives in the county",
	"nc-health":       "NC health, for hospitals and nursing homes",
	"fbi-cde":         "FBI, for reported crime",
	"fhfa-hpi":        "Federal Housing Finance Agency, for what prices have done",
	"osrm":            "OpenStreetMap routing, for drive times",
}

// Name is the endpoint in plain words, falling back to the slug for anything not
// named yet, which is better than an empty row.
func (g GuardStatus) Name() string {
	if n, ok := guardNames[g.Endpoint]; ok {
		return n
	}
	if strings.HasPrefix(g.Endpoint, "overpass") {
		return "OpenStreetMap, for the roads and places to go"
	}
	return g.Endpoint
}

// Resting reports that this one has been asked to slow down, which is the only
// state on this panel a reader can do anything about, and what they can do is wait.
func (g GuardStatus) Resting() bool { return g.Open }

// backoffFor is the status page's view of the same growth the guard applies, for
// a row whose own Guard is not to hand.
func backoffFor(trips int) time.Duration {
	const base, ceiling = 10 * time.Minute, 8 * time.Hour
	d := base
	for i := 1; i < trips && d < ceiling; i++ {
		d *= 2
	}
	if d > ceiling {
		d = ceiling
	}
	return d
}

func guardStatuses(db *sql.DB) ([]GuardStatus, error) {
	rows, err := db.Query(`SELECT endpoint, failures, opened_at, window_count, trips, last_error
		FROM guards ORDER BY endpoint`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []GuardStatus
	for rows.Next() {
		var s GuardStatus
		var opened int64
		var trips int
		if err := rows.Scan(&s.Endpoint, &s.Failures, &opened, &s.Used, &trips, &s.LastError); err != nil {
			return nil, err
		}
		if opened > 0 {
			s.Open = true
			s.OpenUntil = time.Unix(opened, 0).Add(backoffFor(trips))
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
