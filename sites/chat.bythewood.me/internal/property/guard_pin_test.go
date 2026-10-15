package property

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// pinRewind moves one guard's clock back, in memory and in its row, so a test
// can step past a pace, a window or a backoff without waiting it out.
func pinRewind(t *testing.T, g *Guard, d time.Duration) {
	t.Helper()
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.lastCall.IsZero() {
		g.lastCall = g.lastCall.Add(-d)
	}
	secs := int64(d / time.Second)
	if _, err := g.db.Exec(`UPDATE guards SET
		opened_at = CASE WHEN opened_at > 0 THEN opened_at - ? ELSE 0 END,
		window_start = CASE WHEN window_start > 0 THEN window_start - ? ELSE 0 END
		WHERE endpoint = ?`, secs, secs, g.name); err != nil {
		t.Fatal(err)
	}
}

type pinReply struct {
	status int
	header map[string]string
	body   string
}

func pinUpstream(t *testing.T, replies ...pinReply) (*httptest.Server, *[]time.Time) {
	t.Helper()
	var mu sync.Mutex
	var hits []time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if len(hits) >= len(replies) {
			t.Errorf("an unscripted request reached the upstream")
			w.WriteHeader(http.StatusTeapot)
			return
		}
		rep := replies[len(hits)]
		hits = append(hits, time.Now())
		for k, v := range rep.header {
			w.Header().Set(k, v)
		}
		w.WriteHeader(rep.status)
		fmt.Fprint(w, rep.body)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

var (
	pinDuration = regexp.MustCompile(`\b(\d+h)?(\d+m)?\d+(\.\d+)?(s|ms|µs)\b`)
	pinHostPort = regexp.MustCompile(`127\.0\.0\.1:\d{2,}`)
)

// pinNormal rounds every duration in s, to 10ms for a short one and ten
// seconds for the rest, since a backoff's time left has moved on by the time it
// is read.
func pinNormal(s string) string {
	s = pinHostPort.ReplaceAllString(s, "HOST")
	return pinDuration.ReplaceAllStringFunc(s, func(m string) string {
		d, err := time.ParseDuration(m)
		if err != nil {
			return m
		}
		if d < 10*time.Second {
			return d.Round(10 * time.Millisecond).String()
		}
		return d.Round(10 * time.Second).String()
	})
}

func pinCompare(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") == strings.Join(want, "\n") {
		return
	}
	t.Errorf("the sequence changed.\ngot:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
}

// pinRow is the guard's row as the page and a restart would read it.
func pinRow(t *testing.T, db *sql.DB, name string) string {
	t.Helper()
	var failures, count, trips int
	var opened, start int64
	var last string
	err := db.QueryRow(`SELECT failures, opened_at, window_start, window_count, trips, last_error
		FROM guards WHERE endpoint = ?`, name).Scan(&failures, &opened, &start, &count, &trips, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return "row none"
	}
	if err != nil {
		t.Fatal(err)
	}
	until := "-"
	for _, s := range mustStatuses(t, db) {
		if s.Endpoint == name && s.Open {
			until = "past"
			if left := time.Until(s.OpenUntil); left > 0 {
				until = left.String()
			}
		}
	}
	return pinNormal(fmt.Sprintf("row failures=%d open=%v until=%s count=%d trips=%d windowed=%v err=%s",
		failures, opened > 0, until, count, trips, start > 0, last))
}

func mustStatuses(t *testing.T, db *sql.DB) []GuardStatus {
	t.Helper()
	s, err := guardStatuses(db)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

var pinClock = regexp.MustCompile(`\d\d:\d\d UTC`)

// The breaker through every way a call can end, against an upstream that
// answers 404, 429 and 5xx with Retry-After in both forms, plus a dead address
// and a body that fails to read.
func TestGuardSequenceIsPinned(t *testing.T) {
	db := testDB(t)
	date := time.Now().Add(2 * time.Hour).UTC().Format(http.TimeFormat)
	srv, hits := pinUpstream(t,
		pinReply{status: 200, body: `{"a":1}`},
		pinReply{status: 404}, pinReply{status: 404}, pinReply{status: 404},
		pinReply{status: 404}, pinReply{status: 404}, pinReply{status: 404},
		pinReply{status: 429, header: map[string]string{"Retry-After": "120"}},
		pinReply{status: 503, header: map[string]string{"Retry-After": date}},
		pinReply{status: 500},
		pinReply{status: 502},
		pinReply{status: 500},
		pinReply{status: 500},
		pinReply{status: 500},
		pinReply{status: 200, body: `{"a":2}`},
		pinReply{status: 200, body: "streamed"},
		pinReply{status: 429, header: map[string]string{"Retry-After": "  600 "}},
		pinReply{status: 403, header: map[string]string{"Retry-After": "3600"}},
	)
	dead := "http://127.0.0.1:1/q?api_key=SECRET&f=json"
	g := NewGuard(db, "pin", GuardOpts{Timeout: 5 * time.Second})

	var got []string
	note := func(err error) {
		if err != nil {
			got = append(got, pinNormal("err="+err.Error()))
		} else {
			got = append(got, "ok")
		}
	}
	call := func(url string) {
		before := len(*hits)
		var out map[string]any
		err := g.Get(t.Context(), url, &out)
		line := fmt.Sprintf("sent=%v ", len(*hits) > before)
		if err != nil {
			var guarded *ErrGuarded
			line += fmt.Sprintf("guarded=%v err=%s", errors.As(err, &guarded), err)
		} else {
			line += fmt.Sprintf("ok %v", out)
		}
		got = append(got, pinNormal(line))
	}
	row := func() { got = append(got, pinRow(t, db, "pin")) }
	resting := func() {
		got = append(got, strings.TrimSpace("resting "+pinClock.ReplaceAllString(strings.Join(Resting(db), "|"), "HH:MM UTC")))
	}

	call(srv.URL)
	row()
	for i := 0; i < 6; i++ {
		pinRewind(t, g, 2*time.Second)
		call(srv.URL)
	}
	row()
	pinRewind(t, g, 2*time.Second)
	call(dead)
	row()
	call(srv.URL)
	resting()
	pinRewind(t, g, 10*time.Minute)
	call(srv.URL)
	row()
	call(srv.URL)
	pinRewind(t, g, 20*time.Minute)
	call(srv.URL)
	row()
	call(srv.URL)
	for _, d := range []time.Duration{2 * time.Hour, 80 * time.Minute, 160 * time.Minute, 320 * time.Minute, 8 * time.Hour} {
		pinRewind(t, g, d)
		call(srv.URL)
		call(srv.URL)
	}
	row()
	pinRewind(t, g, 8*time.Hour)
	call(srv.URL)
	row()
	resting()
	for i := 0; i < 5; i++ {
		pinRewind(t, g, 2*time.Second)
		call(dead)
	}
	row()
	call(srv.URL)
	pinRewind(t, g, 10*time.Minute)
	note(g.Stream(t.Context(), srv.URL, func(r io.Reader) error {
		b, _ := io.ReadAll(r)
		return fmt.Errorf("could not use %q", b)
	}))
	row()
	pinRewind(t, g, 2*time.Second)
	call(srv.URL)
	row()
	call(srv.URL)
	pinRewind(t, g, 20*time.Minute)
	call(srv.URL)
	row()

	pinCompare(t, got, []string{
		"sent=true ok map[a:1]",
		"row failures=0 open=false until=- count=1 trips=0 windowed=true err=",
		"sent=true guarded=false err=pin: HTTP 404",
		"sent=true guarded=false err=pin: HTTP 404",
		"sent=true guarded=false err=pin: HTTP 404",
		"sent=true guarded=false err=pin: HTTP 404",
		"sent=true guarded=false err=pin: HTTP 404",
		"sent=true guarded=false err=pin: HTTP 404",
		"row failures=6 open=false until=- count=7 trips=0 windowed=true err=HTTP 404",
		`sent=false guarded=false err=pin: Get "http://127.0.0.1:1/q?api_key=REDACTED&f=json": dial tcp 127.0.0.1:1: connect: connection refused`,
		`row failures=7 open=true until=10m0s count=8 trips=1 windowed=true err=Get "http://127.0.0.1:1/q?api_key=REDACTED&f=json": dial tcp 127.0.0.1:1: connect: connection refused`,
		"sent=false guarded=true err=pin guarded: backed off, 10m0s left",
		"resting pin, resting until about HH:MM UTC",
		"sent=true guarded=false err=pin: HTTP 429, backing off 20m0s",
		"row failures=5 open=true until=20m0s count=9 trips=2 windowed=true err=HTTP 429",
		"sent=false guarded=true err=pin guarded: backed off, 20m0s left",
		"sent=true guarded=false err=pin: HTTP 503, backing off 2h0m0s",
		"row failures=5 open=true until=2h0m0s count=10 trips=3 windowed=true err=HTTP 503",
		"sent=false guarded=true err=pin guarded: backed off, 2h0m0s left",
		"sent=true guarded=false err=pin: HTTP 500, backing off 1h20m0s",
		"sent=false guarded=true err=pin guarded: backed off, 1h20m0s left",
		"sent=true guarded=false err=pin: HTTP 502, backing off 2h40m0s",
		"sent=false guarded=true err=pin guarded: backed off, 2h40m0s left",
		"sent=true guarded=false err=pin: HTTP 500, backing off 5h20m0s",
		"sent=false guarded=true err=pin guarded: backed off, 5h20m0s left",
		"sent=true guarded=false err=pin: HTTP 500, backing off 8h0m0s",
		"sent=false guarded=true err=pin guarded: backed off, 8h0m0s left",
		"sent=true guarded=false err=pin: HTTP 500, backing off 8h0m0s",
		"sent=false guarded=true err=pin guarded: backed off, 8h0m0s left",
		"row failures=5 open=true until=8h0m0s count=1 trips=8 windowed=true err=HTTP 500",
		"sent=true ok map[a:2]",
		"row failures=0 open=false until=- count=1 trips=0 windowed=true err=",
		"resting",
		`sent=false guarded=false err=pin: Get "http://127.0.0.1:1/q?api_key=REDACTED&f=json": dial tcp 127.0.0.1:1: connect: connection refused`,
		`sent=false guarded=false err=pin: Get "http://127.0.0.1:1/q?api_key=REDACTED&f=json": dial tcp 127.0.0.1:1: connect: connection refused`,
		`sent=false guarded=false err=pin: Get "http://127.0.0.1:1/q?api_key=REDACTED&f=json": dial tcp 127.0.0.1:1: connect: connection refused`,
		`sent=false guarded=false err=pin: Get "http://127.0.0.1:1/q?api_key=REDACTED&f=json": dial tcp 127.0.0.1:1: connect: connection refused`,
		`sent=false guarded=false err=pin: Get "http://127.0.0.1:1/q?api_key=REDACTED&f=json": dial tcp 127.0.0.1:1: connect: connection refused`,
		`row failures=5 open=true until=10m0s count=6 trips=1 windowed=true err=Get "http://127.0.0.1:1/q?api_key=REDACTED&f=json": dial tcp 127.0.0.1:1: connect: connection refused`,
		"sent=false guarded=true err=pin guarded: backed off, 10m0s left",
		`err=pin: could not use "streamed"`,
		`row failures=1 open=false until=- count=7 trips=1 windowed=true err=could not use "streamed"`,
		"sent=true guarded=false err=pin: HTTP 429, backing off 20m0s",
		"row failures=5 open=true until=20m0s count=8 trips=2 windowed=true err=HTTP 429",
		"sent=false guarded=true err=pin guarded: backed off, 20m0s left",
		"sent=true guarded=false err=pin: HTTP 403",
		"row failures=1 open=false until=- count=9 trips=2 windowed=true err=HTTP 403",
	})
}

// The budget is a fixed window from its first call, and a refusal after a
// backoff has run out writes nothing, so the row still says resting.
func TestGuardBudgetIsPinned(t *testing.T) {
	db := testDB(t)
	srv, _ := pinUpstream(t,
		pinReply{status: 200, body: "{}"}, pinReply{status: 200, body: "{}"}, pinReply{status: 200, body: "{}"},
		pinReply{status: 200, body: "{}"},
		pinReply{status: 200, body: "{}"}, pinReply{status: 429},
	)
	g := NewGuard(db, "pin-budget", GuardOpts{Budget: 3, MinInterval: time.Second})
	var got []string
	call := func(g *Guard) {
		err := g.Get(t.Context(), srv.URL, nil)
		got = append(got, pinNormal(fmt.Sprintf("%s %v", g.name, err)))
	}
	for i := 0; i < 4; i++ {
		call(g)
		pinRewind(t, g, 2*time.Second)
	}
	got = append(got, pinRow(t, db, "pin-budget"))
	pinRewind(t, g, time.Hour-10*time.Second)
	call(g)
	pinRewind(t, g, 12*time.Second)
	call(g)
	got = append(got, pinRow(t, db, "pin-budget"))

	h := NewGuard(db, "pin-half", GuardOpts{Budget: 2, MinInterval: time.Second})
	call(h)
	pinRewind(t, h, 2*time.Second)
	call(h)
	pinRewind(t, h, 10*time.Minute)
	call(h)
	got = append(got, pinRow(t, db, "pin-half"))

	pinCompare(t, got, []string{
		"pin-budget <nil>",
		"pin-budget <nil>",
		"pin-budget <nil>",
		"pin-budget pin-budget guarded: budget spent, 3 in this 1h0m0s",
		"row failures=0 open=false until=- count=3 trips=0 windowed=true err=",
		"pin-budget pin-budget guarded: budget spent, 3 in this 1h0m0s",
		"pin-budget <nil>",
		"row failures=0 open=false until=- count=1 trips=0 windowed=true err=",
		"pin-half <nil>",
		"pin-half pin-half: HTTP 429, backing off 10m0s",
		"pin-half pin-half guarded: budget spent, 2 in this 1h0m0s",
		"row failures=5 open=true until=past count=2 trips=1 windowed=true err=HTTP 429",
	})
}

// The pace is the gap plus up to a fifth of it at random, and a cancelled wait
// sends nothing and counts nothing.
func TestGuardPaceIsPinned(t *testing.T) {
	db := testDB(t)
	var replies []pinReply
	for i := 0; i < 6; i++ {
		replies = append(replies, pinReply{status: 200, body: "{}"})
	}
	srv, hits := pinUpstream(t, replies...)
	g := NewGuard(db, "pin-pace", GuardOpts{MinInterval: 100 * time.Millisecond})
	for i := 0; i < 6; i++ {
		if err := g.Get(t.Context(), srv.URL, nil); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i < len(*hits); i++ {
		if gap := (*hits)[i].Sub((*hits)[i-1]); gap < 100*time.Millisecond || gap > 170*time.Millisecond {
			t.Errorf("call %d came %s after the last, want 100ms to 120ms", i, gap)
		}
	}

	before := pinRow(t, db, "pin-pace")
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	time.Sleep(0)
	err := g.Get(ctx, srv.URL, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a wait that ran out returned %v", err)
	}
	if after := pinRow(t, db, "pin-pace"); after != before {
		t.Errorf("a cancelled wait changed the row from %s to %s", before, after)
	}
	if len(*hits) != 6 {
		t.Errorf("a cancelled wait reached the upstream")
	}
}

// Every endpoint's options, so a pace or a budget can't drift without this
// test changing with it.
func TestGuardEndpointsArePinned(t *testing.T) {
	db := testDB(t)
	guards := []*Guard{
		NewGeocoder(db).guard, NewFlood(db, nil).fema, NewFlood(db, nil).usgs,
		NewTerrain(db).guard, NewParcels(db).guard, NewUSDA(db).guard,
		NewCensus(db).guard, NewFacilities(db, nil).guard, NewRouter(db).guard,
		NewRates(db).guard, NewHPI(db).guard, NewCrime(db).guard,
		NewIndustry(db, nil).epa, NewRoads(db, nil, 0).ncdot, NewSchools(db).statewide,
		NewGuard(db, "defaults", GuardOpts{}),
	}
	for _, m := range NewRoads(db, nil, 0).overpass {
		guards = append(guards, m.guard)
	}
	for _, g := range NewSchools(db).guards {
		guards = append(guards, g)
	}
	var got []string
	for _, g := range guards {
		ua := "browser"
		if g.userAgent == clientUA {
			ua = "client"
		} else if g.userAgent != browserUA {
			ua = g.userAgent
		}
		got = append(got, fmt.Sprintf("%s gap=%s budget=%d/%s trip=%d open=%s timeout=%s ua=%s",
			g.name, g.minInterval, g.budget, g.window, g.tripAfter, g.openFor, g.client.Timeout, ua))
	}
	sort.Strings(got)
	pinCompare(t, got, []string{
		"acs-census gap=2s budget=60/1h0m0s trip=5 open=10m0s timeout=40s ua=browser",
		"census-geocoder gap=1.5s budget=60/1h0m0s trip=5 open=10m0s timeout=20s ua=browser",
		"defaults gap=1.5s budget=60/1h0m0s trip=5 open=10m0s timeout=20s ua=browser",
		"epa-facilities gap=1.5s budget=60/1h0m0s trip=5 open=10m0s timeout=30s ua=browser",
		"fbi-cde gap=2s budget=150/24h0m0s trip=5 open=10m0s timeout=45s ua=client",
		"fema-nfhl gap=1.5s budget=60/1h0m0s trip=5 open=10m0s timeout=30s ua=browser",
		"fhfa-hpi gap=30s budget=8/24h0m0s trip=5 open=10m0s timeout=3m0s ua=client",
		"freddiemac gap=5s budget=10/24h0m0s trip=5 open=10m0s timeout=30s ua=client",
		"gis-alexander gap=3s budget=80/1h0m0s trip=3 open=10m0s timeout=30s ua=browser",
		"gis-caldwell gap=3s budget=80/1h0m0s trip=3 open=10m0s timeout=30s ua=browser",
		"nc-health gap=2s budget=60/1h0m0s trip=5 open=10m0s timeout=45s ua=browser",
		"nc-parcels gap=2s budget=60/1h0m0s trip=5 open=10m0s timeout=45s ua=browser",
		"nc-schools gap=2s budget=60/1h0m0s trip=5 open=10m0s timeout=40s ua=browser",
		"ncdot-aadt gap=1.5s budget=60/1h0m0s trip=5 open=10m0s timeout=30s ua=browser",
		"osrm gap=2s budget=150/1h0m0s trip=5 open=10m0s timeout=30s ua=client",
		"overpass:lz4.overpass-api.de gap=4s budget=40/1h0m0s trip=5 open=10m0s timeout=25s ua=client",
		"overpass:overpass-api.de gap=4s budget=40/1h0m0s trip=5 open=10m0s timeout=25s ua=client",
		"overpass:overpass.kumi.systems gap=4s budget=40/1h0m0s trip=5 open=10m0s timeout=25s ua=client",
		"overpass:overpass.private.coffee gap=4s budget=40/1h0m0s trip=5 open=10m0s timeout=25s ua=client",
		"overpass:z.overpass-api.de gap=4s budget=40/1h0m0s trip=5 open=10m0s timeout=25s ua=client",
		"usda-eligibility gap=2s budget=60/1h0m0s trip=5 open=10m0s timeout=30s ua=client",
		"usgs-3dep gap=1.5s budget=60/1h0m0s trip=5 open=10m0s timeout=40s ua=browser",
		"usgs-nhd gap=1.5s budget=60/1h0m0s trip=5 open=10m0s timeout=30s ua=browser",
	})
}
