package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

const chromeUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

func postPerf(t *testing.T, s *site, id uuid.UUID, ua, data string) int {
	t.Helper()
	body := `{"collectorId":"` + id.String() + `","event":"perf","data":` + data + `}`
	r := httptest.NewRequest("POST", "/collect", strings.NewReader(body))
	r.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	r.Header.Set("User-Agent", ua)
	w := httptest.NewRecorder()
	s.collect(w, r)
	return w.Code
}

func perfSite(t *testing.T) (*site, uuid.UUID) {
	t.Helper()
	db, id := retentionTestDB(t)
	s, _, _ := testSiteOn(t, db, id)
	s.ua = NewUAParser()
	s.collects = newClientLimiter()
	s.propCollects = newPropertyLimiter()
	return s, id
}

func TestPerfEventFillsVitalsAndErrors(t *testing.T) {
	s, id := perfSite(t)
	code := postPerf(t, s, id, chromeUA, `{"url":"/pricing","lcp":2300.4,"inp":96,"cls":0.04,"fcp":900,"ttfb":180,
		"load":99999999,"lcp_el":"img.hero","nav":"navigate","overflow":1,"errors":[
		{"k":"js","m":"TypeError: cart is undefined","s":"/checkout.js:212:9"},
		{"k":"made-up","m":"ignored"},
		{"k":"csp","m":"script-src cdn.example.com","s":"/pricing"}]}`)
	if code != http.StatusNoContent {
		t.Fatalf("perf answered %d", code)
	}

	var lcp, inp int64
	var cls float64
	var load any
	var device, el string
	if err := s.db.QueryRow(`SELECT lcp_ms, inp_ms, cls, load_ms, device, lcp_el FROM vitals`).
		Scan(&lcp, &inp, &cls, &load, &device, &el); err != nil {
		t.Fatal(err)
	}
	if lcp != 2300 || inp != 96 || cls != 0.04 || device != "Desktop" || el != "img.hero" {
		t.Errorf("stored lcp %d inp %d cls %v device %q el %q", lcp, inp, cls, device, el)
	}
	if load != nil {
		t.Errorf("an impossible load time was kept as %v", load)
	}

	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM page_errors`).Scan(&n)
	if n != 2 {
		t.Errorf("%d errors stored, want 2 with the unknown kind dropped", n)
	}
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&n)
	if n != 0 {
		t.Errorf("a perf event landed in events %d times", n)
	}
}

// The beacon sent on a later hide carries only new errors, and must not add a
// second, empty page view to the percentiles.
func TestErrorOnlyBeaconAddsNoVitalsRow(t *testing.T) {
	s, id := perfSite(t)
	postPerf(t, s, id, chromeUA, `{"url":"/","errors":[{"k":"promise","m":"boom"}]}`)
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM vitals`).Scan(&n)
	if n != 0 {
		t.Errorf("%d vitals rows from an errors-only beacon", n)
	}
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM page_errors`).Scan(&n)
	if n != 1 {
		t.Errorf("%d errors stored, want 1", n)
	}
}

func TestBotPerfIsDropped(t *testing.T) {
	s, id := perfSite(t)
	postPerf(t, s, id, "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", `{"url":"/","lcp":100}`)
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM vitals`).Scan(&n)
	if n != 0 {
		t.Errorf("a bot's timings were kept")
	}
}

func TestP75IsNearestRank(t *testing.T) {
	for _, c := range []struct {
		in   []float64
		want float64
	}{
		{[]float64{5}, 5},
		{[]float64{4, 1, 3, 2}, 3},
		{[]float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, 8},
	} {
		if got := p75(append([]float64(nil), c.in...)); got != c.want {
			t.Errorf("p75(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func seedVitals(t *testing.T, s *site, id uuid.UUID) {
	t.Helper()
	for _, lcp := range []string{"1200", "1400", "5200", "1300"} {
		postPerf(t, s, id, chromeUA, `{"url":"/","lcp":`+lcp+`,"cls":0.01,"ttfb":100}`)
	}
	postPerf(t, s, id, chromeUA, `{"url":"/","errors":[{"k":"js","m":"TypeError: secret detail","s":"/app.js:1:2"}]}`)
}

func TestDashboardShowsRealUsers(t *testing.T) {
	s, id := perfSite(t)
	seedVitals(t, s, id)
	today := time.Now().In(eastern).Format("2006-01-02")
	w := get(t, s, id, "date_start="+today+"&date_end="+today, true)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{"LCP &middot; p75", "1.4 s", "TypeError: secret detail", "Slowest pages"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page is missing %q", want)
		}
	}

	if _, err := s.db.Exec(`UPDATE properties SET is_public = 1 WHERE id = ?`, id[:]); err != nil {
		t.Fatal(err)
	}
	body = get(t, s, id, "date_start="+today+"&date_end="+today, false).Body.String()
	if strings.Contains(body, "secret detail") {
		t.Error("a signed out visitor sees error text")
	}
	if !strings.Contains(body, "1.4 s") {
		t.Error("a signed out visitor doesn't see the vitals")
	}
}

func TestBridgeVitals(t *testing.T) {
	s, id := perfSite(t)
	seedVitals(t, s, id)
	var name string
	_ = s.db.QueryRow(`SELECT name FROM properties WHERE id = ?`, id[:]).Scan(&name)

	w := httptest.NewRecorder()
	s.bridgeVitals(w, httptest.NewRequest("GET", "/bridge/vitals?host="+name, nil))
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var out struct {
		Samples   int   `json:"samples"`
		Errors24h int64 `json:"errors_24h"`
		Metrics   []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
			Class string `json:"class"`
		} `json:"metrics"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Samples != 4 || out.Errors24h != 1 || out.Metrics[0].Name != "LCP" || out.Metrics[0].Value != "1.4 s" || out.Metrics[0].Class != "ok" {
		t.Errorf("bridge answered %+v", out)
	}

	w = httptest.NewRecorder()
	s.bridgeVitals(w, httptest.NewRequest("GET", "/bridge/vitals?host=nope.example.com", nil))
	if w.Code != 404 {
		t.Errorf("an unknown host answered %d", w.Code)
	}
}
