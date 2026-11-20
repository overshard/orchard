package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"logging.bythewood.me/suite"
	"logging.bythewood.me/web"
)

func TestParseQuery(t *testing.T) {
	got := parseQuery(`timeout -healthz source:Shop level:error,warn "two words" lvl:INFO order:A-1 -upstream:pay.example.com msg:"a, b" "x:y"`)
	want := []term{
		{Text: "timeout"},
		{Neg: true, Text: "healthz"},
		{Field: "source", Values: []string{"shop"}},
		{Field: "level", Values: []string{"ERROR", "WARN"}},
		{Text: "two words"},
		{Field: "level", Values: []string{"INFO"}},
		{Field: "order", Values: []string{"A-1"}},
		{Neg: true, Field: "upstream", Values: []string{"pay.example.com"}},
		{Field: "msg", Values: []string{"a, b"}},
		{Text: "x:y"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d terms, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Neg != want[i].Neg || got[i].Field != want[i].Field || got[i].Text != want[i].Text || !slices.Equal(got[i].Values, want[i].Values) {
			t.Errorf("term %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// Writing terms back and reading them again has to land on the same query,
// since every link on the page is built that way.
func TestQueryRoundTrips(t *testing.T) {
	for _, q := range []string{
		`timeout -healthz`,
		`source:shop,api status:5xx`,
		`msg:"a, b" "two words"`,
		`path:/cart/* -ip:127.0.0.1`,
	} {
		once := joinTerms(parseQuery(q))
		twice := joinTerms(parseQuery(once))
		if once != twice {
			t.Errorf("%q came back as %q then %q", q, once, twice)
		}
	}
}

func TestToggleValue(t *testing.T) {
	ts := parseQuery("timeout source:shop")
	ts = toggleValue(ts, "source", "api", false)
	if got := joinTerms(ts); got != "timeout source:shop,api" {
		t.Errorf("adding a second source = %q", got)
	}
	ts = toggleValue(ts, "source", "shop", false)
	ts = toggleValue(ts, "source", "api", false)
	if got := joinTerms(ts); got != "timeout" {
		t.Errorf("removing both sources = %q", got)
	}
	ts = toggleValue(ts, "level", "warn", true)
	if got := joinTerms(ts); got != "timeout -level:WARN" {
		t.Errorf("excluding a level = %q", got)
	}
}

func TestLegacyParamsFoldIntoTheQuery(t *testing.T) {
	q, _ := url.ParseQuery("source=blog&level=error&status=5&q=boom")
	v := newSearchView(q, time.Now())
	if v.Q != "source:blog level:ERROR status:5xx boom" {
		t.Errorf("query = %q", v.Q)
	}
}

func searchIDs(t *testing.T, db *sql.DB, q string) []int64 {
	t.Helper()
	w, args := searchWhere(0, 1<<62, parseQuery(q), "")
	rows, err := db.Query(`SELECT id FROM records WHERE `+w+` ORDER BY id`, args...)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		_ = rows.Scan(&id)
		out = append(out, id)
	}
	return out
}

func TestQueryClauses(t *testing.T) {
	db := testDB(t)
	base := time.Now().UTC().Add(-time.Hour).UnixMilli()
	seedRows(t, db, []row{
		{source: "shop", ts: base, level: "ERROR", msg: "payment gateway timeout after 10000ms", component: "checkout", attrs: `{"after_ms":10000,"upstream":"pay.example.com"}`},
		{source: "shop", ts: base + 1, level: "WARN", msg: "request", method: "POST", path: "/cart/checkout", status: 502, durationMS: 9000},
		{source: "api", ts: base + 2, level: "INFO", msg: "request", method: "GET", path: "/healthz-ish", status: 200, durationMS: 1},
		{source: "api", ts: base + 3, level: "INFO", msg: "retrying order write", attrs: `{"attempt":2}`},
		{source: "api", ts: base + 4, level: "INFO", msg: "100% done"},
	})

	cases := map[string][]int64{
		"timeout":                     {1},
		"pay.example":                 {1},
		"-healthz":                    {1, 2, 4, 5},
		"status:5xx":                  {2},
		"status:502":                  {2},
		"status:2xx,5xx":              {2, 3},
		"after_ms:10000":              {1},
		"attempt:2":                   {4},
		"-attempt:2":                  {1, 2, 3, 5},
		"path:/cart/*":                {2},
		`msg:"payment gateway *ms"`:   {1},
		"source:api -level:INFO":      nil,
		"component:checkout":          {1},
		"%":                           {5},
		"status:nope":                 nil,
		"source:shop level:WARN,INFO": {2},
	}
	for q, want := range cases {
		if got := searchIDs(t, db, q); !slices.Equal(got, want) {
			t.Errorf("%q matched %v, want %v", q, got, want)
		}
	}
}

func TestMasking(t *testing.T) {
	cases := map[string]string{
		"payment gateway timeout after 10000ms": "payment gateway timeout after <n>ms",
		"retrying order write attempt=2":        "retrying order write attempt=<n>",
		"took 1.5s and 2,000 rows":              "took <n>s and <n> rows",
		"commit 3f9a2c1b4d pushed":              "commit <id> pushed",
		"fetch /products/42 failed":             "fetch /products/<id> failed",
		"checking https://blog.bythewood.me":    "checking https://blog.bythewood.me",
	}
	for in, want := range cases {
		if got := maskMessage(in); got != want {
			t.Errorf("maskMessage(%q) = %q, want %q", in, got, want)
		}
	}
	if got := maskPath("/api/v2/orders/8812?x=1"); got != "/api/<id>/orders/<id>" {
		t.Errorf("maskPath = %q", got)
	}
}

func TestPercentiles(t *testing.T) {
	var d []float64
	for i := 1; i <= 100; i++ {
		d = append(d, float64(i))
	}
	l := percentiles(d)
	if l.P50 != 50 || l.P95 != 95 || l.P99 != 99 || l.Max != 100 {
		t.Errorf("got %+v", l)
	}
	if one := percentiles([]float64{7}); one.P95 != 7 {
		t.Errorf("one sample p95 = %v", one.P95)
	}
}

func TestZoomSnapsToBuckets(t *testing.T) {
	now := time.Date(2026, 11, 19, 14, 20, 0, 0, time.UTC)
	q, _ := url.ParseQuery("range=24h")
	v := newSearchView(q, now)
	from := v.StartMS + 3*v.BucketMS + 1000
	to := v.StartMS + 5*v.BucketMS - 1000
	q.Set("from", itoa(from))
	q.Set("to", itoa(to))
	z := newSearchView(q, now)
	if !z.Zoomed || z.FromMS != v.StartMS+3*v.BucketMS || z.ToMS != v.StartMS+5*v.BucketMS {
		t.Errorf("zoom = %v %d..%d, want buckets 3 to 5", z.Zoomed, z.FromMS-v.StartMS, z.ToMS-v.StartMS)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func seedExplorer(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	base := time.Now().UTC().Add(-2 * time.Hour).UnixMilli()
	var rows []row
	for i := 0; i < 40; i++ {
		rows = append(rows, row{source: "shop", ts: base + int64(i)*1000, level: "ERROR", msg: "payment gateway timeout after 10000ms", component: "checkout"})
	}
	for i := 0; i < 30; i++ {
		rows = append(rows, row{source: "api", ts: base + int64(i)*1000, level: "INFO", msg: "request", method: "GET", path: "/products/" + itoa(int64(i)), status: 200, durationMS: float64(i + 1)})
	}
	for i := 0; i < 10; i++ {
		rows = append(rows, row{source: "api", ts: base + int64(i)*1000, level: "WARN", msg: "request", method: "POST", path: "/pay", status: 502, durationMS: 100})
	}
	seedRows(t, db, rows)
	return base
}

func TestRunSearchCountsFromOnePass(t *testing.T) {
	db := testDB(t)
	seedExplorer(t, db)

	q, _ := url.ParseQuery("range=24h&q=source:api")
	v := newSearchView(q, time.Now())
	runSearch(context.Background(), db, v)
	v.finish()

	if v.Matches != 40 || v.Warns != 10 || v.S5xx != 10 || v.Requests != 40 {
		t.Errorf("matches %d warns %d 5xx %d requests %d", v.Matches, v.Warns, v.S5xx, v.Requests)
	}
	var hist int64
	for _, b := range v.Hist {
		hist += b.Total()
	}
	if hist != 40 {
		t.Errorf("histogram holds %d, want 40", hist)
	}
	// The source facet leaves its own include out, so shop still shows.
	for _, f := range v.Facets {
		if f.Field != "source" {
			continue
		}
		var names []string
		for _, fv := range f.Values {
			names = append(names, fv.Value)
			if fv.Value == "api" && !fv.On {
				t.Error("the chosen source isn't marked on")
			}
		}
		if !slices.Contains(names, "shop") {
			t.Errorf("source facet = %v, want shop listed beside api", names)
		}
	}
	// Thirty product pages are one shape and the 502s another.
	if v.Shapes != 2 {
		t.Errorf("shapes = %d, want 2: %+v", v.Shapes, v.Patterns)
	}
	if len(v.Results) != 40 || v.HasMore {
		t.Errorf("lines = %d, more %v", len(v.Results), v.HasMore)
	}
	for _, p := range v.Patterns {
		if p.Shape == "200 GET /products/<id>" && !strings.Contains(p.Href, "path%3A%2Fproducts%2F%2A") {
			t.Errorf("pattern link %q doesn't filter on the masked path", p.Href)
		}
	}
	if v.Latency.Count != 40 || v.Latency.Max != 100 {
		t.Errorf("latency = %+v", v.Latency)
	}
}

func TestSearchPagesPastTheFirst(t *testing.T) {
	db := testDB(t)
	base := time.Now().UTC().Add(-time.Hour).UnixMilli()
	var rows []row
	for i := 0; i < 250; i++ {
		rows = append(rows, row{source: "blog", ts: base + int64(i), level: "INFO", msg: "tick"})
	}
	seedRows(t, db, rows)
	q, _ := url.ParseQuery("range=24h&page=3")
	v := newSearchView(q, time.Now())
	runSearch(context.Background(), db, v)
	if len(v.Results) != 50 || v.HasMore || v.Pages != 3 {
		t.Fatalf("page 3 has %d lines, more %v, pages %d", len(v.Results), v.HasMore, v.Pages)
	}
	if v.Results[0].TS != base+49 {
		t.Errorf("page 3 starts at %d, want %d", v.Results[0].TS-base, 49)
	}
}

func testSiteWithTemplates(t *testing.T, db *sql.DB) *site {
	t.Helper()
	s := testSite(t, db)
	var err error
	s.renderer, err = web.NewRenderer(suite.With(mustTemplates(t)), suite.FuncsWith(templateFuncs), layoutTemplates, pageTemplates)
	if err != nil {
		t.Fatal(err)
	}
	if s.lineTmpl, err = parseLineTemplate(mustTemplates(t)); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSearchPageRenders(t *testing.T) {
	db := testDB(t)
	seedExplorer(t, db)
	s := testSiteWithTemplates(t, db)

	for _, path := range []string{
		"/search?q=timeout+-healthz&range=24h",
		"/search?q=source:api&range=24h&view=patterns",
		"/search?q=nothing-matches-this&range=1h",
		"/search?q=status:5xx&range=7d&from=1&to=2",
		"/search?source=api&status=5",
	} {
		rr := httptest.NewRecorder()
		s.search(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: %d\n%s", path, rr.Code, rr.Body.String())
		}
	}

	rr := httptest.NewRecorder()
	s.search(rr, httptest.NewRequest(http.MethodGet, "/search?q=timeout+-healthz&range=24h", nil))
	body := rr.Body.String()
	for _, want := range []string{`<mark>timeout</mark>`, `class="o-chip x neg"`, `context &plusmn;25`, `data-hist`, `40 lines</em> match`} {
		if !strings.Contains(body, want) {
			t.Errorf("search page lacks %q", want)
		}
	}
}

func TestContextShowsNeighbours(t *testing.T) {
	db := testDB(t)
	base := time.Now().UTC().Add(-time.Hour).UnixMilli()
	var rows []row
	for i := 0; i < 80; i++ {
		rows = append(rows, row{source: "blog", ts: base + int64(i), level: "INFO", msg: "tick"})
	}
	rows = append(rows, row{source: "other", ts: base + 40, level: "INFO", msg: "elsewhere"})
	seedRows(t, db, rows)

	target, got := contextRows(context.Background(), db, 41)
	if target == nil || len(got) != 51 {
		t.Fatalf("got %d lines around %v", len(got), target)
	}
	if got[25].ID != 41 {
		t.Errorf("the target sits at %d, want the middle", got[25].ID)
	}
	for _, r := range got {
		if r.Source != "blog" {
			t.Errorf("context pulled in a line from %s", r.Source)
		}
	}
}

func TestTailPollsOnlyNewMatches(t *testing.T) {
	db := testDB(t)
	base := time.Now().UTC().UnixMilli()
	seedRows(t, db, []row{
		{source: "shop", ts: base, level: "ERROR", msg: "boom one"},
		{source: "shop", ts: base, level: "INFO", msg: "quiet"},
	})
	s := testSiteWithTemplates(t, db)

	poll := func(after string) (int64, int, string) {
		rr := httptest.NewRecorder()
		s.tail(rr, httptest.NewRequest(http.MethodGet, "/search/tail?q=boom&after="+after, nil))
		var out struct {
			After int64  `json:"after"`
			N     int    `json:"n"`
			HTML  string `json:"html"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatalf("%v\n%s", err, rr.Body.String())
		}
		return out.After, out.N, out.HTML
	}

	after, n, html := poll("")
	if n != 1 || !strings.Contains(html, "<mark>boom</mark> one") {
		t.Fatalf("first poll = %d lines\n%s", n, html)
	}
	seedRows(t, db, []row{
		{source: "shop", ts: base + 1, level: "ERROR", msg: "boom two"},
		{source: "shop", ts: base + 1, level: "INFO", msg: "quiet again"},
	})
	next, n, html := poll(itoa(after))
	if n != 1 || !strings.Contains(html, "two") || next <= after {
		t.Errorf("second poll = %d lines, after %d to %d\n%s", n, after, next, html)
	}
	if _, n, _ := poll(itoa(next)); n != 0 {
		t.Errorf("a poll with nothing new returned %d lines", n)
	}
}

// A named window starts on the floored hour, so 24h spans up to 25 and must
// still draw half hour bars.
func TestBucketForFlooredWindows(t *testing.T) {
	const hour = int64(60 * 60 * 1000)
	cases := map[int64]int64{
		hour + hour/2:       2 * 60 * 1000,
		24*hour + hour/2:    30 * 60 * 1000,
		7*24*hour + hour/2:  3 * hour,
		30*24*hour + hour/2: 12 * hour,
	}
	for span, want := range cases {
		if got := bucketFor(span); got != want {
			t.Errorf("bucketFor(%dh) = %dm, want %dm", span/hour, got/60000, want/60000)
		}
	}
}
