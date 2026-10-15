package property

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

// Guard sits in front of somebody else's free server, where a ban is hard to undo.
// The breaker's state is a row in SQLite so a restart doesn't hand a struggling
// upstream a fresh round, and the budget is the backstop for a bug that loops.
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

// GuardOpts is per endpoint, since a county ArcGIS box and the Census geocoder
// don't deserve the same pace.
type GuardOpts struct {
	MinInterval time.Duration
	Budget      int
	Window      time.Duration
	TripAfter   int
	OpenFor     time.Duration
	Timeout     time.Duration

	// Empty means the browser string. Set it to clientUA for an API that wants
	// to know who is calling, which some of them enforce.
	UserAgent string
}

func NewGuard(db *sql.DB, name string, o GuardOpts) *Guard {
	// The defaults are what an endpoint added without thinking gets, so they're
	// the cautious numbers. A cold address costs about forty requests across all
	// of these, so no single endpoint has any business sending sixty an hour.
	if o.MinInterval == 0 {
		o.MinInterval = 1500 * time.Millisecond
	}
	if o.Budget == 0 {
		o.Budget = 60
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
		// Seeded per guard so two created in the same millisecond don't jitter
		// in lockstep.
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

// A public records server behind a WAF, like the DHSR roster, answers 403 to
// anything that isn't a browser, while Overpass answers 406 to a browser string.
// The browser string is the default since more of these are records servers.
const browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// Services that publish usage terms ask for a contact and are entitled to it.
const clientUA = "house.bythewood.me/1.0 (self-hosted house search; isaac@bythewood.me)"

// ErrGuarded means no request was sent, so a caller can tell "the upstream said
// no" from "we didn't ask".
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
	// Opens without a clean run in between, which grows the backoff. A
	// successful request resets it.
	trips int
}

// backoff doubles per consecutive trip and stops at eight hours, so a run the
// next morning isn't still waiting.
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

// An http transport error stringifies the whole URL, so saving it verbatim
// writes an API key into the database and anything that renders guard state.
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

// Stream is for the one source here measured in tens of megabytes.
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
		// Half open, one request decides. The trip count stays, so a service
		// that fails again waits twice as long.
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

	// Clients pacing to the same round number arrive in step, which looks like an
	// attack from the far end.
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
	// This is for getting a public record out of a WAF'd server, never for
	// getting past a site that has looked at the traffic and said no.
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
		state.failures = g.tripAfter
		state.trips++
		state.openedAt = now.Unix()

		wait := g.backoff(state.trips)
		if asked := retryAfter(resp.Header.Get("Retry-After"), now); asked > wait {
			// A later open time, so the backoff arithmetic carries the longer wait.
			// Stamping it in the past instead lets the next call sail through.
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

	// Forgive the history so a service that had a bad hour isn't punished for it
	// a week later.
	state.failures = 0
	state.trips = 0
	g.save(state, "")
	return nil
}

// retryAfter reads both formats, seconds or an HTTP date, since reading only the
// first means asking again straight away.
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

// GuardStatus exists since "no schools found nearby" and "the schools server is
// refusing us this hour" read the same on a page and aren't the same fact.
type GuardStatus struct {
	Endpoint  string
	Failures  int
	Open      bool
	OpenUntil time.Time
	Used      int
	LastError string
}

// "nc-parcels, 3 failures" means nothing to somebody who asked about a house.
var guardNames = map[string]string{
	"census-geocoder":  "US Census, for turning an address into a place on the map",
	"nc-parcels":       "NC OneMap, for the lot and what the county values it at",
	"gis-alexander":    "Alexander County schools",
	"gis-caldwell":     "Caldwell County schools",
	"nc-schools":       "NC schools, statewide",
	"ncdot-aadt":       "NC transport, for how busy the road is",
	"fema-nfhl":        "FEMA, for the flood map",
	"usgs-nhd":         "US Geological Survey, for creeks and rivers",
	"usgs-3dep":        "US Geological Survey, for how flat the yard is",
	"acs-census":       "US Census, for who lives in the county",
	"nc-health":        "NC health, for hospitals and nursing homes",
	"fbi-cde":          "FBI, for reported crime",
	"fhfa-hpi":         "Federal Housing Finance Agency, for what prices have done",
	"osrm":             "OpenStreetMap routing, for drive times",
	"usda-eligibility": "USDA Rural Development, for whether they will lend here",
	"freddiemac":       "Freddie Mac, for this week's mortgage rates",
}

func (g GuardStatus) Name() string {
	if n, ok := guardNames[g.Endpoint]; ok {
		return n
	}
	if strings.HasPrefix(g.Endpoint, "overpass") {
		return "OpenStreetMap, for the roads and places to go"
	}
	return g.Endpoint
}

func (g GuardStatus) Resting() bool { return g.Open }

// backoffFor mirrors Guard.backoff with the default OpenFor, for a row whose own
// Guard isn't to hand.
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

// Resting says which upstreams asked us to slow down and for how long, since
// waiting is the only thing a reader can do about a gap.
func Resting(db *sql.DB) []string {
	all, err := guardStatuses(db)
	if err != nil {
		return nil
	}
	var out []string
	for _, s := range all {
		if !s.Resting() {
			continue
		}
		out = append(out, fmt.Sprintf("%s, resting until about %s",
			s.Name(), s.OpenUntil.UTC().Format("15:04 UTC")))
	}
	return out
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
