package main

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"dash.bythewood.me/web"
)

func TestHostOf(t *testing.T) {
	cases := map[string]string{
		"https://www.example.com/a/b?c=d": "example.com",
		"https://example.com:8443/x":      "example.com",
		"https://news.ycombinator.com/":   "news.ycombinator.com",
		"not a url":                       "",
		"":                                "",
	}
	for in, want := range cases {
		if got := hostOf(in); got != want {
			t.Errorf("hostOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHumanAge(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{10 * time.Second, "just now"},
		{5 * time.Minute, "5m"},
		{3 * time.Hour, "3h"},
		{50 * time.Hour, "2d"},
	}
	for _, c := range cases {
		if got := humanAge(now.Add(-c.ago), now); got != c.want {
			t.Errorf("%s ago gave %q, want %q", c.ago, got, c.want)
		}
	}
}

// The WMO code ranges are contiguous, so an off-by-one at a boundary is the
// failure mode and every boundary is checked rather than a sample.
func TestDescribeWeatherBoundaries(t *testing.T) {
	cases := map[int]string{
		0: "Clear", 1: "Partly cloudy", 2: "Partly cloudy", 3: "Overcast",
		45: "Fog", 48: "Fog",
		51: "Drizzle", 57: "Drizzle",
		61: "Rain", 67: "Rain",
		71: "Snow", 77: "Snow",
		80: "Showers", 82: "Showers",
		85: "Snow showers", 86: "Snow showers",
		95: "Thunderstorms", 99: "Thunderstorms",
		4: "Unknown", 44: "Unknown",
	}
	for code, want := range cases {
		if got := describeWeather(code); got != want {
			t.Errorf("code %d = %q, want %q", code, got, want)
		}
	}
}

// A cached edge response is not evidence the origin is up, which is the whole
// reason the strip asks the bridge first.
func TestCacheHit(t *testing.T) {
	for _, s := range []string{"HIT", "hit", "STALE", "UPDATING", "REVALIDATED"} {
		if !cacheHit(s) {
			t.Errorf("%q should count as served from cache", s)
		}
	}
	for _, s := range []string{"MISS", "DYNAMIC", "EXPIRED", "BYPASS", ""} {
		if cacheHit(s) {
			t.Errorf("%q should not count as served from cache", s)
		}
	}
}

// A probe that the guard refused measured nothing, so the row has to stay
// unknown. Reporting it as down is what made the strip claim six live sites
// were dead.
func TestProbeReportsUnknownWhenTheGuardRefuses(t *testing.T) {
	g := NewGuard(t.TempDir())
	g.Fail("uptime", http.StatusTooManyRequests, 0)

	row := probe(t.Context(), g, Monitored{Label: "Blog", Source: "blog", Host: "blog.invalid"})
	if row.State != "unknown" {
		t.Errorf("state %q, want unknown", row.State)
	}
}

func newTestSite(t *testing.T) *site {
	t.Helper()

	dist := os.DirFS("build/dist")
	if _, err := fs.Stat(dist, ".vite/manifest.json"); err != nil {
		t.Skip("no vite bundle; run `make frontend` first")
	}
	assets, err := web.LoadAssets(dist)
	if err != nil {
		t.Fatal(err)
	}
	templates, err := fs.Sub(templateFS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := web.NewRenderer(templates, templateFuncs,
		[]string{"base.html", "partials.html"},
		[]string{"home.html", "notfound.html"})
	if err != nil {
		t.Fatal(err)
	}

	hub := NewHub()
	return &site{
		renderer: renderer,
		store:    NewStore(hub),
		hub:      hub,
		guard:    NewGuard(t.TempDir()),
		script:   assets.Script("index.js"),
		styles:   assets.Styles("index.js"),
	}
}

// html/template resolves a missing field at execute time rather than at parse
// time, so a page nothing renders is a page nothing checks.
func TestHomeRendersWithAnEmptyState(t *testing.T) {
	s := newTestSite(t)

	rec := httptest.NewRecorder()
	s.home(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"MARKETS", "CONDITIONS", "RATES", "SECTORS", "EARNINGS", "WIRE",
		"HACKER NEWS", "LOBSTERS", "ATMOS", "SYSTEMS", "UPLINK", "STEAM"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page is missing the %s panel", want)
		}
	}
}

func TestHomeRendersAFullState(t *testing.T) {
	s := newTestSite(t)

	now := time.Date(2026, 8, 27, 14, 0, 0, 0, time.UTC)
	s.store.update(func(st *State) {
		st.Market = buildMarket(map[string]Quote{
			"^GSPC": {Price: 100, Previous: 99, High52: 110, AsOf: now, Closes: []float64{99, 100, 101}},
		}, now)
		st.HN = []Story{{Title: "A story", URL: "https://example.com/a", Host: "example.com", Comments: "https://news.ycombinator.com/item?id=1", Points: 10, Count: 2, Age: "1h"}}
		st.Lobsters = []Story{{Title: "Another", URL: "https://example.com/b", Host: "example.com", Comments: "https://lobste.rs/s/x", Points: 5, Count: 1, Age: "2h"}}
		st.Weather = Weather{Place: "Yadkin Valley, NC", Temperature: "81", Feels: "86", High: "86", Low: "70", Rain: "3%", Condition: "Clear", Wind: "4 mph"}
		st.Systems = Systems{Rows: []SystemRow{{Label: "Blog", State: "up", Response: "12ms", Errors: 3, KnowError: true}}, Up: 1, Total: 1, Window: 24}
	})

	rec := httptest.NewRecorder()
	s.home(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"A story", "Another", "Yadkin Valley", "S&amp;P 500", "spark-line"} {
		if !strings.Contains(body, want) {
			t.Errorf("the rendered page is missing %q", want)
		}
	}
}

func TestNotFoundRenders(t *testing.T) {
	s := newTestSite(t)

	rec := httptest.NewRecorder()
	s.notFound(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404", rec.Code)
	}
}

// The page is public, so the JSON behind it is too, and it must not grow a
// field that carries anything but numbers and public feed content.
func TestStateJSONCarriesNoInternalDetail(t *testing.T) {
	s := newTestSite(t)
	s.store.update(func(st *State) {
		st.Systems = Systems{Rows: []SystemRow{{Label: "Blog", Host: "blog.bythewood.me", State: "up", Response: "12ms", Errors: 3, KnowError: true}}}
	})

	rec := httptest.NewRecorder()
	s.state(rec, httptest.NewRequest(http.MethodGet, "/api/state", nil))

	var row map[string]any
	var body struct {
		Systems struct {
			Rows []map[string]any `json:"rows"`
		} `json:"systems"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	row = body.Systems.Rows[0]

	// Each of these was looked at before being added. label, host and url are
	// public hostnames that are already links on the page. state, response,
	// errors and the traffic figures are counts about sites that are on the
	// internet, and Isaac asked for them on a page with no login. The baseline
	// logging computes the trend from is not among them: the derived direction
	// is published and the underlying week is not.
	allowed := map[string]bool{
		"label": true, "host": true, "url": true, "state": true,
		"response": true, "errors": true, "know_error": true,
		"requests": true, "know_traf": true, "level": true, "trend": true,
	}
	for k := range row {
		if !allowed[k] {
			t.Errorf("a system row publishes %q, which was not reviewed as safe for a public page", k)
		}
	}
}

func TestHubReplaysTheLatestFrameToANewSubscriber(t *testing.T) {
	h := NewHub()
	h.Broadcast([]byte("first"))

	frames, unsubscribe := h.Subscribe()
	defer unsubscribe()

	select {
	case got := <-frames:
		if string(got) != "first" {
			t.Errorf("replayed %q, want first", got)
		}
	default:
		t.Error("a new subscriber got nothing, so a reconnect would show an empty page until the next poll")
	}
}

// A browser that cannot keep up must not hold the broadcast for everyone else.
func TestHubDropsRatherThanBlocking(t *testing.T) {
	h := NewHub()
	_, unsubscribe := h.Subscribe()
	defer unsubscribe()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			h.Broadcast([]byte("frame"))
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Broadcast blocked on a subscriber that was not reading")
	}
}

func TestHubUnsubscribeIsIdempotent(t *testing.T) {
	h := NewHub()
	_, unsubscribe := h.Subscribe()

	unsubscribe()
	unsubscribe()

	if n := h.Watching(); n != 0 {
		t.Errorf("%d watchers after unsubscribing, want 0", n)
	}
}

func TestIsLoopback(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "::1"} {
		if !isLoopback(ip) {
			t.Errorf("%s should be loopback", ip)
		}
	}
	for _, ip := range []string{"8.8.8.8", "192.168.1.5", "", "garbage"} {
		if isLoopback(ip) {
			t.Errorf("%s should not be loopback", ip)
		}
	}
}

// A visitor arriving during a quiet spell must not wait out the rest of the
// idle interval on a page that says it is live.
func TestHubWakesOnTheFirstSubscriber(t *testing.T) {
	h := NewHub()

	frames, unsubscribe := h.Subscribe()
	_ = frames
	select {
	case <-h.wake:
	default:
		t.Fatal("the first subscriber did not wake the poller")
	}

	// A second viewer while one is already connected is not a quiet spell
	// ending, so it must not force another fetch.
	_, unsubscribe2 := h.Subscribe()
	select {
	case <-h.wake:
		t.Error("a second concurrent subscriber woke the poller as well")
	default:
	}

	unsubscribe()
	unsubscribe2()

	// Back to nobody, so the next arrival is a fresh wake.
	_, unsubscribe3 := h.Subscribe()
	defer unsubscribe3()
	select {
	case <-h.wake:
	default:
		t.Error("the first subscriber after everyone left did not wake the poller")
	}
}

// The wake send must never block the browser that is connecting.
func TestHubWakeDoesNotBlockAConnectingClient(t *testing.T) {
	h := NewHub()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			_, unsubscribe := h.Subscribe()
			unsubscribe()
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Subscribe blocked on the wake channel")
	}
}

// The panel says where every figure on the page came from, so the order has to
// be stable: a map range would reshuffle it on every poll.
func TestGuardFeedsAreStableAndComplete(t *testing.T) {
	g := NewGuard(t.TempDir())
	now := time.Now()

	first := g.Feeds(now)
	if len(first) != len(feedOrder) {
		t.Fatalf("%d feeds, want %d", len(first), len(feedOrder))
	}
	for i := 0; i < 20; i++ {
		next := g.Feeds(now)
		for j := range next {
			if next[j].Name != first[j].Name {
				t.Fatalf("feed order moved at %d: %q then %q", j, first[j].Name, next[j].Name)
			}
		}
	}

	// Nothing has been called yet, so every row reads idle rather than ok.
	for _, f := range first {
		if f.State != "idle" {
			t.Errorf("%s reads %q before any call, want idle", f.Name, f.State)
		}
	}
}

func TestGuardFeedsReportBreakerState(t *testing.T) {
	g := NewGuard(t.TempDir())
	now := time.Now()

	if err := g.Allow("yahoo"); err != nil {
		t.Fatal(err)
	}
	g.Succeed("yahoo")
	g.Fail("lobsters", http.StatusTooManyRequests, 0)

	byName := map[string]Feed{}
	for _, f := range g.Feeds(now) {
		byName[f.Name] = f
	}

	if got := byName["YAHOO"].State; got != "ok" {
		t.Errorf("yahoo reads %q after a success, want ok", got)
	}
	if got := byName["LOBSTERS"].State; got != "open" {
		t.Errorf("lobsters reads %q with the breaker open, want open", got)
	}
}

func TestShortAge(t *testing.T) {
	cases := map[time.Duration]string{
		5 * time.Second:  "5s",
		90 * time.Second: "1m",
		3 * time.Hour:    "3h",
	}
	for d, want := range cases {
		if got := shortAge(d); got != want {
			t.Errorf("shortAge(%s) = %q, want %q", d, got, want)
		}
	}
}

func TestVisitURLCarriesTheCampaign(t *testing.T) {
	got := visitURL("blog.bythewood.me")

	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("visitURL produced %q: %v", got, err)
	}
	if u.Scheme != "https" || u.Host != "blog.bythewood.me" {
		t.Errorf("points at %s://%s, want https://blog.bythewood.me", u.Scheme, u.Host)
	}
	for k, want := range map[string]string{
		"utm_source": "dash.bythewood.me",
		"utm_medium": "referral",
	} {
		if got := u.Query().Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

// Busy is relative to the busiest thing on this machine, not to an absolute.
func TestTrafficLevel(t *testing.T) {
	cases := []struct {
		requests, busiest int64
		want              int
	}{
		{0, 1000, 0},
		{10, 0, 0},
		{1000, 1000, 4},
		{600, 1000, 4},
		{400, 1000, 3},
		{150, 1000, 2},
		{10, 1000, 1},
	}
	for _, c := range cases {
		if got := trafficLevel(c.requests, c.busiest); got != c.want {
			t.Errorf("trafficLevel(%d, %d) = %d, want %d", c.requests, c.busiest, got, c.want)
		}
	}
}
func TestSystemRowDoesNotCarryTheBaseline(t *testing.T) {
	b, err := json.Marshal(SystemRow{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "baseline") {
		t.Errorf("a system row publishes a baseline field: %s", b)
	}
}

// Caddy is on the strip because it is the one edge component that ships its
// access log, so it has an error count to show. It has no public hostname and
// no health endpoint, and both of those change how it is probed.
func TestCaddyIsMonitoredWithoutAHostname(t *testing.T) {
	var caddy *Monitored
	for i := range monitored {
		if monitored[i].Source == "caddy" {
			caddy = &monitored[i]
		}
	}
	if caddy == nil {
		t.Fatal("caddy is not on the strip")
	}
	if caddy.Host != "" {
		t.Errorf("caddy has host %q, but it serves every hostname and owns none", caddy.Host)
	}
	if !caddy.AnyStatus {
		t.Error("caddy answers its catch-all with 404, so any status has to count as serving")
	}
	if caddy.Bridge == "" {
		t.Error("caddy has no /healthz, so it needs an explicit bridge URL")
	}
}

// A row with no hostname must not be given a link, and must not fall back to a
// public probe it has no address for.
func TestProbeWithoutAHostnameStaysUnknown(t *testing.T) {
	g := NewGuard(t.TempDir())

	row := probe(t.Context(), g, Monitored{
		Label:  "Edge",
		Source: "nowhere",
		Bridge: "http://orchard-nowhere.invalid:80/",
	})

	if row.URL != "" {
		t.Errorf("URL = %q, want none for a row with no hostname", row.URL)
	}
	if row.State != "unknown" {
		t.Errorf("state = %q, want unknown with no route and no fallback", row.State)
	}
}

// Every source on the strip has to match what logging files its records under,
// or the error and traffic columns are silently blank for that row.
func TestMonitoredSourcesAreDistinct(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range monitored {
		if m.Source == "" {
			t.Errorf("%s has no source, so it can never be matched to its logs", m.Label)
		}
		if seen[m.Source] {
			t.Errorf("%q appears twice, so one row would take the other's counts", m.Source)
		}
		seen[m.Source] = true
	}
}

// The verdict is worded as an observation about the market, never as an
// instruction. A dashboard that tells someone to buy will eventually do it at
// the worst possible moment.
func TestSignalVerdictNeverInstructs(t *testing.T) {
	for worst := 0; worst <= 3; worst++ {
		level, headline := verdict(worst)
		if level == "" || headline == "" {
			t.Fatalf("worst %d gave %q / %q", worst, level, headline)
		}
		for _, word := range []string{"BUY ", "SELL", "SHOULD", "MUST", "NOW IS"} {
			if strings.Contains(headline, word) {
				t.Errorf("worst %d says %q, which reads as advice", worst, headline)
			}
		}
	}
}

func TestSignalGradesTheConditions(t *testing.T) {
	// 200 closes falling from 100 to 90, so the trend and the drawdown both
	// have something to say.
	closes := make([]float64, 220)
	for i := range closes {
		closes[i] = 100 - float64(i)*0.05
	}
	h := &history{closes: closes, high52: 100}

	quotes := map[string]Quote{
		"^GSPC": {Price: 78, Previous: 80},
		"^VIX":  {Price: 31, Previous: 24},
	}

	sig := buildSignal(h, quotes)
	if sig.Level != "deep" && sig.Level != "stress" {
		t.Errorf("level = %q with a 22%% drawdown and a VIX of 31", sig.Level)
	}
	if len(sig.Conditions) < 3 {
		t.Errorf("%d conditions, want drawdown, five day, vix and trend", len(sig.Conditions))
	}

	byLabel := map[string]Condition{}
	for _, c := range sig.Conditions {
		byLabel[c.Label] = c
	}
	if byLabel["VIX"].Note != "STRESSED" {
		t.Errorf("a VIX of 31 reads %q", byLabel["VIX"].Note)
	}
	if byLabel["TREND"].Note != "BELOW 200DMA" {
		t.Errorf("a falling series reads %q", byLabel["TREND"].Note)
	}
}

// A calm market must not be dressed up as an opportunity.
func TestSignalStaysCalmInAQuietMarket(t *testing.T) {
	closes := make([]float64, 220)
	for i := range closes {
		closes[i] = 100 + float64(i)*0.02
	}
	h := &history{closes: closes, high52: 105}

	sig := buildSignal(h, map[string]Quote{
		"^GSPC": {Price: 104.4, Previous: 104.3},
		"^VIX":  {Price: 13, Previous: 13.2},
	})
	if sig.Level != "calm" {
		t.Errorf("level = %q just below the high with a VIX of 13", sig.Level)
	}
}

func TestSignalSurvivesNoHistory(t *testing.T) {
	sig := buildSignal(nil, map[string]Quote{"^VIX": {Price: 14}})
	if sig.Level == "" || sig.Headline == "" {
		t.Error("the panel has to say something with no daily series")
	}
}

// Yahoo quoted these at ten times the yield for years and now quotes the yield.
func TestNormaliseYield(t *testing.T) {
	for in, want := range map[float64]float64{4.25: 4.25, 42.5: 4.25, 0.5: 0.5, 21: 2.1} {
		if got := normaliseYield(in); got != want {
			t.Errorf("normaliseYield(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestBuildRatesAndCurve(t *testing.T) {
	r := buildRates(map[string]Quote{
		"^IRX": {Price: 4.50, Previous: 4.48},
		"^FVX": {Price: 4.10, Previous: 4.12},
		"^TNX": {Price: 4.30, Previous: 4.25},
		"^TYX": {Price: 4.80, Previous: 4.79},
	})
	if len(r.Rows) != 4 {
		t.Fatalf("%d rows, want 4", len(r.Rows))
	}
	// 4.30 minus 4.50 is a 20 basis point inversion.
	if r.CurveState != "inverted" {
		t.Errorf("curve state = %q with the 10Y below the 3M", r.CurveState)
	}
	if r.Curve != "-20bp" {
		t.Errorf("curve = %q, want -20bp", r.Curve)
	}
}

// The board is read best to worst, so the order is the information.
func TestBuildSectorsSortsByMove(t *testing.T) {
	cells := buildSectors(map[string]Quote{
		"XLK": {Price: 101, Previous: 100},
		"XLE": {Price: 97, Previous: 100},
		"XLF": {Price: 100.5, Previous: 100},
	})

	var seen []float64
	for _, c := range cells {
		if !c.Unavailable {
			seen = append(seen, c.Raw)
		}
	}
	for i := 1; i < len(seen); i++ {
		if seen[i] > seen[i-1] {
			t.Errorf("out of order at %d: %v", i, seen)
			break
		}
	}
	if cells[len(cells)-1].Unavailable != true {
		t.Error("the funds with no quote should sort to the end")
	}
}

func TestEarningsHelpers(t *testing.T) {
	if got := parseMoney("$1,767,631,360,000"); got != 1767631360000 {
		t.Errorf("parseMoney = %v", got)
	}
	if got := parseMoney("N/A"); got != 0 {
		t.Errorf("parseMoney(N/A) = %v, want 0", got)
	}
	if got := shortMoney(1767631360000); got != "1.8T" {
		t.Errorf("shortMoney = %q, want 1.8T", got)
	}
	if got := trimCompany("Broadcom Inc."); got != "Broadcom" {
		t.Errorf("trimCompany = %q", got)
	}
	if got := whenLabel("time-after-hours"); got != "POST" {
		t.Errorf("whenLabel = %q, want POST", got)
	}
}

func TestBands(t *testing.T) {
	if got := aqiBand(49); got != "GOOD" {
		t.Errorf("aqiBand(49) = %q", got)
	}
	if got := aqiBand(160); got != "UNHEALTHY" {
		t.Errorf("aqiBand(160) = %q", got)
	}
	if got := pollenBand(8.8); got != "MED-HIGH" {
		t.Errorf("pollenBand(8.8) = %q", got)
	}
	if got := uvBand(7.15); got != "HIGH" {
		t.Errorf("uvBand(7.15) = %q", got)
	}
}

func TestDayProgressClamps(t *testing.T) {
	rise := time.Date(2026, 8, 30, 6, 0, 0, 0, time.UTC)
	set := rise.Add(12 * time.Hour)

	if got := dayProgress(rise.Add(-time.Hour), rise, set); got != 0 {
		t.Errorf("before dawn = %d, want 0", got)
	}
	if got := dayProgress(set.Add(time.Hour), rise, set); got != 100 {
		t.Errorf("after dusk = %d, want 100", got)
	}
	if got := dayProgress(rise.Add(6*time.Hour), rise, set); got != 50 {
		t.Errorf("midday = %d, want 50", got)
	}
}

// Go's time layout is the reference time and it is case sensitive, so an
// uppercased layout is matched literally. Every earnings row read "MON 1 JAN".
func TestDayLabelFormatsRealDates(t *testing.T) {
	today := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)

	if got := dayLabel(today, today); got != "TODAY" {
		t.Errorf("today = %q", got)
	}
	if got := dayLabel(today.AddDate(0, 0, 1), today); got != "TOMORROW" {
		t.Errorf("tomorrow = %q", got)
	}

	got := dayLabel(today.AddDate(0, 0, 3), today)
	if got == "MON 1 JAN" || !strings.Contains(got, "SEP") {
		t.Errorf("three days out = %q, want a real date in September", got)
	}
}

// A player count has to fit a narrow column, and nobody needs the last three
// digits of 431,908.
func TestCompactCount(t *testing.T) {
	for in, want := range map[int]string{
		431908: "432K", 4601: "4.6K", 1460000: "1.5M", 812: "812",
	} {
		if got := compactCount(in); got != want {
			t.Errorf("compactCount(%d) = %q, want %q", in, got, want)
		}
	}
}

// JustWatch returns every way to watch a title, including a dozen resold
// channels, so a title on Netflix and on three resellers has to read as Netflix
// and "with Ads" must not become its own service.
func TestPickProvider(t *testing.T) {
	cases := []struct {
		names []string
		want  string
	}{
		{[]string{"Netflix", "Netflix basic with Ads"}, "NETFLIX"},
		{[]string{"Amazon Prime Video", "Amazon Prime Video with Ads"}, "PRIME"},
		{[]string{"HBO Max", "HBO Max Amazon Channel"}, "HBO MAX"},
		{[]string{"Paramount Plus Apple TV Channel"}, "PARAMOUNT+"},
		{[]string{"Apple TV Amazon Channel", "Apple TV"}, "APPLE TV+"},
		{[]string{"Hulu"}, "HULU"},
		// Nothing Isaac would be subscribed to, so the row is dropped.
		{[]string{"Some Obscure Channel"}, ""},
		{nil, ""},
	}
	for _, c := range cases {
		if got := pickProvider(c.names); got != c.want {
			t.Errorf("pickProvider(%v) = %q, want %q", c.names, got, c.want)
		}
	}
}

// The answer must not depend on the order the offers came back in.
func TestPickProviderIgnoresOfferOrder(t *testing.T) {
	a := pickProvider([]string{"Hulu", "Netflix"})
	b := pickProvider([]string{"Netflix", "Hulu"})
	if a != b {
		t.Errorf("order changed the answer: %q then %q", a, b)
	}
}

// Friday counts, because that is when a weekend trip leaves.
func TestOutlookCoversFridayThroughSunday(t *testing.T) {
	if outlookDays != 3 {
		t.Errorf("outlookDays = %d, want 3", outlookDays)
	}
}

// A row labelled IMDB has to open IMDb or nothing, since a link that says one
// thing and opens another is worse than no link.
func TestIMDbURL(t *testing.T) {
	cases := map[string]string{
		"tt10986410": "https://www.imdb.com/title/tt10986410/",
		" tt9288030": "https://www.imdb.com/title/tt9288030/",
		"":           "",
		"nm0000001":  "",
		"tt":         "",
		"ttnotanid":  "",
		"10986410":   "",
	}
	for in, want := range cases {
		if got := imdbURL(in); got != want {
			t.Errorf("imdbURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSteamAppIDComesOffTheCapsuleURL(t *testing.T) {
	for _, tc := range []struct{ logo, want string }{
		{"https://shared.fastly.steamstatic.com/store_item_assets/steam/apps/3751260/abc/capsule_sm_120.jpg?t=1", "3751260"},
		{"https://cdn.akamai.steamstatic.com/steam/apps/730/capsule_sm_120.jpg", "730"},
		{"https://example.invalid/nothing.jpg", ""},
	} {
		got := ""
		if m := steamAppID.FindStringSubmatch(tc.logo); m != nil {
			got = m[1]
		}
		if got != tc.want {
			t.Errorf("%s gave %q, want %q", tc.logo, got, tc.want)
		}
	}
}

func TestSteamPriceCoversFreeAndUnreleased(t *testing.T) {
	free := &appDetails{}
	free.Data.IsFree = true
	if got := steamPrice(free); got != "FREE" {
		t.Errorf("a free game priced %q", got)
	}

	// A preorder carries no price_overview at all, and the top sellers chart
	// is full of them.
	if got := steamPrice(&appDetails{}); got != "TBA" {
		t.Errorf("an unreleased game priced %q", got)
	}

	paid := &appDetails{}
	paid.Data.Price = &struct {
		Final           int `json:"final"`
		DiscountPercent int `json:"discount_percent"`
	}{Final: 1674, DiscountPercent: 33}
	if got := steamPrice(paid); got != "$16.74" {
		t.Errorf("priced %q, want $16.74", got)
	}
	if got := discount(paid); got != 33 {
		t.Errorf("discount %d, want 33", got)
	}
}

func TestKeepSteamWillNotShrinkAFullPanel(t *testing.T) {
	full := make([]Game, steamShown)
	for _, tc := range []struct {
		name         string
		fresh, shown []Game
		want         bool
	}{
		{"a full poll always lands", full, full, true},
		{"two rows do not replace six", make([]Game, 2), full, false},
		{"two rows do replace one", make([]Game, 2), make([]Game, 1), true},
		{"the first poll lands short", make([]Game, 2), nil, true},
	} {
		if got := keepSteam(tc.fresh, tc.shown); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
