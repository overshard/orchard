package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"status.bythewood.me/web"
)

// Each of these pins something that cannot fail loudly on its own:
//
//   - Templates are parsed and executed at *runtime*, so a bad field name is a
//     500 on a page nobody may load for a week. Executing every one of them
//     against realistic data catches that at test time.
//   - The schema has to accept an existing database untouched.
//   - The security-header reading, the alert state machine and the crawler
//     checks all encode judgements that are easy to "simplify" into being
//     wrong.

// --- templates -----------------------------------------------------------

// fullPageData is a PageData with every field populated, so executing a
// template against it exercises every branch that reads a value rather than
// only the nil paths.
func fullPageData(t *testing.T) PageData {
	t.Helper()

	pct := 99.4
	avg := int64(87)
	dur := int64(1234)
	pages := int64(42)
	now := time.Now()
	score := 0.42

	return PageData{
		Title:         "Example",
		Description:   "A description.",
		Path:          "/",
		Canonical:     baseURL + "/",
		Staging:       true,
		Authenticated: true,
		Year:          now.Year(),
		BaseURL:       baseURL,
		SourceURL:     sourceURL,
		SiteName:      siteName,
		AuthorName:    authorName,
		Script:        "/static/base.js",
		Styles:        []string{"/static/base.css"},
		PageScript:    "/static/pages.js",
		PageStyles:    []string{"/static/pages.css"},

		Next:  "/properties",
		Error: "Invalid password.",

		TotalChecks:     130042,
		TotalProperties: 3,
		FirstCheckAt:    "Jun 5, 2022",
		Query:           "example",
		GeneratedAt:     "2026-08-26 21:00 UTC",

		Property: &PropertyView{
			ID:               uuid.New().String(),
			URL:              "https://example.com",
			Name:             "example.com",
			IsPublic:         true,
			IsProtected:      false,
			CurrentStatus:    200,
			AvgResponseTime:  142,
			RecentUptimePct:  &pct,
			RecentTickStream: []string{"up", "up", "down", "up"},
			TotalChecks:      1440,

			CrawlState:          "idle",
			LastCrawlSuccessAt:  &now,
			LastCrawlDurationMS: &dur,
			LastCrawlPagesCount: &pages,
			NextRunAtCrawler:    &now,

			LighthouseState: "idle",
			LighthouseScores: &Scores{
				Performance: 98, Accessibility: 100, BestPractices: 96, SEO: 91,
			},
			LighthouseDetails: &Details{
				Metrics: []Metric{{
					ID: "largest-contentful-paint", Acronym: "LCP",
					Title: "Largest Contentful Paint", DisplayValue: "1.2 s",
					Score: &score, Weight: 25,
				}},
				Opportunities: []Opportunity{{
					ID: "unused-css-rules", Title: "Reduce unused CSS",
					DisplayValue: "Potential savings of 12 KiB", SavingsMS: 1350,
				}},
			},
			LastLighthouseSuccessAt:  &now,
			LastLighthouseDurationMS: &dur,
			NextLighthouseRunAt:      &now,
			AvgLighthouseScore:       &avg,

			AlertState: "up",
			CreatedAt:  now,
			UpdatedAt:  now,

			IsHTTPS:                   true,
			HasMIMEType:               true,
			HasContentSniffProtection: true,
			HasClickjackProtection:    false,
			HidesServerVersion:        true,
			HasHSTS:                   true,
			HasHSTSPreload:            false,
			HasSecurityIssue:          true,

			CrawlerInsights: []Insight{
				{URL: "https://example.com/a", Issue: "Page has no title", Type: typeSEO, Severity: sevError},
			},
		},

		InsightGroups: []InsightGroup{{
			Type: typeSEO,
			Items: []Insight{
				{URL: "https://example.com/a", Issue: "Page has no title", Type: typeSEO, Severity: sevError},
				{URL: "https://example.com/b", Issue: "Thin content (12 words)", Type: typeSEO, Severity: sevWarn, Item: "x"},
			},
		}},
		ResponseTimes: []ResponseTimePoint{
			{Label: now.Format(time.RFC3339), Total: 142, DNS: &avg, TCP: &avg, TLS: &avg, TTFB: &avg},
			// An older row: total only, every phase null.
			{Label: now.Format(time.RFC3339), Total: 138},
		},
		StatusCodes:  []LabelCount{{Label: 200, Count: 1400}, {Label: 526, Count: 40}},
		UptimeSlices: []LabelPercent{{Label: "Uptime", Count: 97.2}, {Label: "Downtime", Count: 2.8}},
	}
}

// emptyPageData is the other half of the matrix: a property that has just been
// created and has no checks, no crawl and no audit. Every nullable field is
// nil, which is the state a template is most likely to blow up on.
func emptyPageData() PageData {
	return PageData{
		Title:    "Example",
		SiteName: siteName,
		BaseURL:  baseURL,
		Year:     2026,
		Property: &PropertyView{
			ID:            uuid.New().String(),
			URL:           "https://example.com",
			Name:          "example.com",
			CurrentStatus: 200,
			CrawlState:    "idle",
			// LighthouseScores, LighthouseDetails, RecentUptimePct,
			// AvgLighthouseScore and every timestamp left nil.
		},
	}
}

func TestTemplatesExecute(t *testing.T) {
	templates, err := fs.Sub(templateFS, "templates")
	if err != nil {
		t.Fatal(err)
	}

	pages := []string{
		"home.html", "changelog.html", "login.html",
		"properties.html", "property.html", "notfound.html",
	}

	renderer, err := web.NewRenderer(templates, templateFuncs,
		[]string{"base.html", "partials.html"}, pages)
	if err != nil {
		t.Fatalf("parsing templates: %v", err)
	}

	full := fullPageData(t)
	full.Properties = []*PropertyView{full.Property}

	empty := emptyPageData()
	empty.Properties = []*PropertyView{empty.Property}

	for _, page := range pages {
		for name, data := range map[string]PageData{"full": full, "empty": empty} {
			t.Run(page+"/"+name, func(t *testing.T) {
				// Render through a recorder rather than calling Execute
				// directly, because Renderer.Render panics on a template
				// error and the panic is the failure being tested for.
				rec := httptest.NewRecorder()
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("rendering %s: %v", page, r)
					}
				}()
				renderer.Render(rec, http.StatusOK, page, data)

				if rec.Code != http.StatusOK {
					t.Fatalf("rendering %s: status %d", page, rec.Code)
				}
				if rec.Body.Len() == 0 {
					t.Fatalf("rendering %s produced no output", page)
				}
			})
		}
	}
}

// TestPropertiesListWithNoProperties covers the {{else}} arm of the range,
// which neither of the fixtures above reaches because both supply one.
func TestPropertiesListWithNoProperties(t *testing.T) {
	templates, _ := fs.Sub(templateFS, "templates")
	renderer, err := web.NewRenderer(templates, templateFuncs,
		[]string{"base.html", "partials.html"}, []string{"properties.html"})
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	renderer.Render(rec, http.StatusOK, "properties.html", PageData{
		Title: "Properties", SiteName: siteName, Year: 2026,
	})
	if !strings.Contains(rec.Body.String(), "no properties yet") {
		t.Error("empty properties list did not render its empty state")
	}
}

func TestReportTemplatesExecute(t *testing.T) {
	full := fullPageData(t)
	empty := emptyPageData()
	empty.GeneratedAt = "2026-08-26 21:00 UTC"
	empty.BaseURL = baseURL

	for _, name := range []string{"report.typ", "report.md"} {
		for label, data := range map[string]PageData{"full": full, "empty": empty} {
			t.Run(name+"/"+label, func(t *testing.T) {
				var buf bytes.Buffer
				if err := reportTemplates.ExecuteTemplate(&buf, name, data); err != nil {
					t.Fatalf("executing %s: %v", name, err)
				}
				if buf.Len() == 0 {
					t.Fatalf("%s produced no output", name)
				}
			})
		}
	}
}

// "//" starts a Typst line comment and a URL is full of them, so an unescaped
// property URL swallows the rest of its line in the PDF.
func TestReportEscapesTypstComment(t *testing.T) {
	data := fullPageData(t)
	data.Property.URL = "https://example.com/a//b"

	var buf bytes.Buffer
	if err := reportTemplates.ExecuteTemplate(&buf, "report.typ", data); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "https://example.com") {
		t.Error("property URL reached the Typst source with its slashes unescaped")
	}
	if !strings.Contains(buf.String(), `https:\/\/example.com`) {
		t.Error("property URL was not escaped the way typstMD escapes it")
	}
}

// --- schema --------------------------------------------------------------

// TestSchemaAcceptsLegacyDatabase builds a database with the shape this app
// inherited, opens it with openDB and writes through every column, so a drifted
// schema fails here rather than against the real database on deploy day.
func TestSchemaAcceptsLegacyDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite3")

	legacySchema, err := os.ReadFile("testdata/legacy_schema.sql")
	if err != nil {
		t.Fatal(err)
	}

	seed, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seed.Exec(string(legacySchema)); err != nil {
		t.Fatalf("applying the legacy schema: %v", err)
	}
	// An older migration tool's bookkeeping table, which this app ignores and
	// must not trip over. It is left in place on the real database too,
	// because dropping it would break a rollback.
	if _, err := seed.Exec(`CREATE TABLE _sqlx_migrations (version BIGINT PRIMARY KEY);
		INSERT INTO _sqlx_migrations VALUES (1), (2);`); err != nil {
		t.Fatal(err)
	}

	id := uuid.New()
	now := time.Now().UnixMilli()
	if _, err := seed.Exec(
		`INSERT INTO properties (id, url, is_public, is_protected, alert_state, created_at, updated_at)
		 VALUES (?, 'https://example.com', 1, 0, 'up', ?, ?)`, id[:], now, now); err != nil {
		t.Fatal(err)
	}
	// An older check row, with no phase timings at all.
	if _, err := seed.Exec(
		`INSERT INTO checks (property_id, status_code, response_ms, headers, created_at)
		 VALUES (?, 200, 142, '{}', ?)`, id[:], now); err != nil {
		t.Fatal(err)
	}
	seed.Close()

	db, err := openDB(path)
	if err != nil {
		t.Fatalf("opening a legacy database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	p, err := getProperty(ctx, db, id)
	if err != nil {
		t.Fatalf("reading the existing property: %v", err)
	}
	if p == nil {
		t.Fatal("the existing property was not found; the cutover would lose it")
	}
	if p.URL != "https://example.com" || p.Name() != "example.com" {
		t.Errorf("property read back wrong: %+v", p)
	}

	// Writing a new check must work, including the four phase columns.
	dns, tcp, tls, ttfb := int64(3), int64(11), int64(29), int64(64)
	if _, err := db.ExecContext(ctx,
		`INSERT INTO checks (property_id, status_code, response_ms, headers,
		   dns_ms, tcp_ms, tls_ms, ttfb_ms, created_at)
		 VALUES (?, 200, 107, '{}', ?, ?, ?, ?, ?)`,
		id[:], dns, tcp, tls, ttfb, now); err != nil {
		t.Fatalf("writing a check with phase timings: %v", err)
	}

	checks, err := recentChecks(ctx, db, id, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) != 2 {
		t.Fatalf("expected 2 checks, got %d", len(checks))
	}
	// The older row must still read back with nil phases rather than zeros:
	// the chart draws a gap for nil and a floor for 0.
	var legacy *Check
	for i := range checks {
		if checks[i].ResponseMS == 142 {
			legacy = &checks[i]
		}
	}
	if legacy == nil {
		t.Fatal("the pre-migration check row did not read back")
	}
	if legacy.DNSMS != nil || legacy.TCPMS != nil || legacy.TLSMS != nil || legacy.TTFBMS != nil {
		t.Error("a pre-migration check row came back with non-nil phase timings")
	}
}

// TestSchemaIsIdempotent covers the ordinary restart: openDB runs on every
// boot and must be a no-op against a database it already created.
func TestSchemaIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite3")
	for i := range 3 {
		db, err := openDB(path)
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		db.Close()
	}
}

// --- security posture ----------------------------------------------------

func TestSecurityPosture(t *testing.T) {
	headers := func(m map[string]string) []Check {
		encoded, _ := json.Marshal(m)
		return []Check{{StatusCode: 200, Headers: string(encoded)}}
	}

	t.Run("a fully hardened response passes everything", func(t *testing.T) {
		v := &PropertyView{URL: "https://example.com", CurrentStatus: 200}
		v.applySecurityPosture(headers(map[string]string{
			"content-type":              "text/html; charset=utf-8",
			"x-content-type-options":    "nosniff",
			"x-frame-options":           "SAMEORIGIN",
			"strict-transport-security": "max-age=31536000; includeSubDomains; preload",
		}))
		if v.HasSecurityIssue {
			t.Errorf("a hardened response was reported as having an issue: %+v", v)
		}
	})

	t.Run("an hsts max-age under a year does not count", func(t *testing.T) {
		v := &PropertyView{URL: "https://example.com"}
		v.applySecurityPosture(headers(map[string]string{
			"strict-transport-security": "max-age=86400",
		}))
		if v.HasHSTS {
			t.Error("a one-day max-age was accepted as HSTS")
		}
	})

	t.Run("an absurd max-age does not overflow into a failure", func(t *testing.T) {
		v := &PropertyView{URL: "https://example.com"}
		v.applySecurityPosture(headers(map[string]string{
			"strict-transport-security": "max-age=99999999999999999999999",
		}))
		// It does not parse as an int64, so it reports as absent rather than
		// wrapping to a negative number. What must not happen is a panic or a
		// nonsense comparison.
		_ = v.HasHSTS
	})

	t.Run("any server header spelling defeats the version check", func(t *testing.T) {
		for _, header := range []string{"server", "x-server", "powered-by", "x-powered-by"} {
			v := &PropertyView{URL: "https://example.com"}
			v.applySecurityPosture(headers(map[string]string{header: "nginx/1.2.3"}))
			if v.HidesServerVersion {
				t.Errorf("%s leaked the server version but the check passed", header)
			}
		}
	})

	t.Run("a property with no checks yet is not reported as insecure HTTPS", func(t *testing.T) {
		v := &PropertyView{URL: "https://example.com"}
		v.applySecurityPosture(nil)
		if !v.IsHTTPS {
			t.Error("an https:// URL with no checks was reported as not HTTPS")
		}
	})

	t.Run("526 is what marks a certificate invalid", func(t *testing.T) {
		v := &PropertyView{URL: "https://example.com", CurrentStatus: 526}
		v.applySecurityPosture(nil)
		if !v.InvalidCert {
			t.Error("a 526 did not mark the certificate invalid")
		}
	})
}

// --- alert state machine -------------------------------------------------

// TestAlertStateMachine pins the asymmetry that makes the alerting usable: two
// strikes to go down, one success to come back.
func TestAlertStateMachine(t *testing.T) {
	db, id := freshDB(t)
	ctx := context.Background()
	notifier := &Notifier{client: http.DefaultClient, base: "http://127.0.0.1:1", topic: "test"}

	p, err := getProperty(ctx, db, id)
	if err != nil {
		t.Fatal(err)
	}

	state := func() string {
		var s string
		if err := db.QueryRow("SELECT alert_state FROM properties WHERE id = ?", id[:]).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	record := func(code int64) {
		if _, err := db.Exec(
			`INSERT INTO checks (property_id, status_code, response_ms, headers, created_at)
			 VALUES (?, ?, 100, '{}', ?)`, id[:], code, nowMS()); err != nil {
			t.Fatal(err)
		}
		if err := advanceAlertState(ctx, db, notifier, p, code); err != nil {
			t.Fatal(err)
		}
	}

	if state() != "up" {
		t.Fatalf("a new property should start up, got %q", state())
	}

	record(503)
	if state() != "up" {
		t.Error("one failure took the property down; it should need two")
	}

	record(503)
	if state() != "down" {
		t.Error("two consecutive failures did not take the property down")
	}

	record(200)
	if state() != "up" {
		t.Error("a single success did not bring the property back up")
	}
}

// TestAlertStateIgnoresNonConsecutiveFailures is the flapping case the
// debounce exists for: fail, recover, fail must not fire an outage.
func TestAlertStateIgnoresNonConsecutiveFailures(t *testing.T) {
	db, id := freshDB(t)
	ctx := context.Background()
	notifier := &Notifier{client: http.DefaultClient, base: "http://127.0.0.1:1", topic: "test"}
	p, _ := getProperty(ctx, db, id)

	// created_at is set explicitly and increasing, because the state machine
	// orders by it and three rows written in the same millisecond would order
	// arbitrarily.
	base := nowMS()
	for i, code := range []int64{503, 200, 503} {
		if _, err := db.Exec(
			`INSERT INTO checks (property_id, status_code, response_ms, headers, created_at)
			 VALUES (?, ?, 100, '{}', ?)`, id[:], code, base+int64(i)); err != nil {
			t.Fatal(err)
		}
		if err := advanceAlertState(ctx, db, notifier, p, code); err != nil {
			t.Fatal(err)
		}
	}

	var s string
	if err := db.QueryRow("SELECT alert_state FROM properties WHERE id = ?", id[:]).Scan(&s); err != nil {
		t.Fatal(err)
	}
	if s != "up" {
		t.Errorf("a flapping site was marked down; state machine says %q", s)
	}
}

func freshDB(t *testing.T) (*sql.DB, uuid.UUID) {
	t.Helper()
	db, err := openDB(filepath.Join(t.TempDir(), "db.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	id, err := createProperty(context.Background(), db, "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	return db, id
}

// --- alerts --------------------------------------------------------------

func TestRenderAlert(t *testing.T) {
	ctx := AlertContext{
		ID: "abc", Name: "example.com", URL: "https://example.com",
		CurrentStatus: 503, AvgResponseMS: 184,
	}

	down, ok := renderAlert("down", ctx)
	if !ok {
		t.Fatal("the down alert did not render")
	}
	if !strings.Contains(down.Title, "example.com") || !strings.Contains(down.Message, "503") {
		t.Errorf("the down alert lost its detail: %+v", down)
	}
	if down.Click != baseURL+"/abc" {
		t.Errorf("the down alert links to %q, which is not an absolute dashboard URL", down.Click)
	}
	// Priority matters: this is read on a phone, and an outage that does not
	// stand out from a recovery gets missed.
	if down.Priority == "default" {
		t.Error("the outage alert has the same priority as a recovery")
	}

	if _, ok := renderAlert("sideways", ctx); ok {
		t.Error("an unknown alert kind rendered instead of being refused")
	}
}

// --- crawler checks ------------------------------------------------------

func page(url string, html *ParsedHTML) *Page {
	return &Page{URL: url, Status: 200, IsHTML: true, HTML: html, ContentType: "text/html"}
}

func parsed(t *testing.T, body, url string) *ParsedHTML {
	t.Helper()
	p, err := parseHTML([]byte(body), url)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseHTML(t *testing.T) {
	p := parsed(t, `<!doctype html><html lang="en"><head>
		<title>  A   title  </title>
		<meta name="description" content="A description.">
		<meta property="og:title" content="OG">
		<meta name="twitter:card" content="summary">
		<link rel="canonical" href="/canonical">
		<link rel="shortcut icon" href="/favicon.ico">
		<script type="application/ld+json">{"@type":"Thing"}</script>
		<script type="application/ld+json">{not json}</script>
	</head><body>
		<h1>Heading one</h1>
		<h3>Skipped a level</h3>
		<a href="/a">Link A</a>
		<a href="#frag">Fragment</a>
		<a href="mailto:x@y.z">Mail</a>
		<a href="https://other.example/b" rel="nofollow noopener">Off site</a>
		<img src="/img.png" alt="described">
		<img src="/bare.png">
		<img src="/decorative.png" alt="">
		<form action="/submit">
			<label for="name">Name</label>
			<input type="text" id="name">
			<input type="text" id="unlabeled">
			<input type="hidden" name="csrf">
		</form>
		<script>var ignored = "script text";</script>
		<style>.ignored { color: red }</style>
	</body></html>`, "https://example.com/page")

	if p.Title != "A title" {
		t.Errorf("title whitespace was not collapsed: %q", p.Title)
	}
	if p.Lang != "en" {
		t.Errorf("lang = %q", p.Lang)
	}
	if p.Canonical != "https://example.com/canonical" {
		t.Errorf("canonical was not resolved against the page URL: %q", p.Canonical)
	}
	if p.Favicon != "https://example.com/favicon.ico" {
		t.Errorf("favicon = %q", p.Favicon)
	}
	if len(p.JSONLD) != 1 || p.JSONLDBad != 1 {
		t.Errorf("JSON-LD: %d valid, %d bad; want 1 and 1", len(p.JSONLD), p.JSONLDBad)
	}

	// Fragments, mailto and tel are not pages.
	if len(p.Links) != 2 {
		t.Errorf("expected 2 crawlable links, got %d: %+v", len(p.Links), p.Links)
	}

	// The pointer exists to keep the alt distinction.
	var missing, decorative int
	for _, img := range p.Images {
		switch {
		case img.Alt == nil:
			missing++
		case *img.Alt == "":
			decorative++
		}
	}
	if missing != 1 || decorative != 1 {
		t.Errorf("alt handling: %d missing, %d decorative; want 1 and 1", missing, decorative)
	}

	if len(p.Forms) != 1 {
		t.Fatalf("expected one form, got %d", len(p.Forms))
	}
	if len(p.Forms[0].Inputs) != 3 || len(p.Forms[0].LabelFors) != 1 {
		t.Errorf("form parsed as %+v", p.Forms[0])
	}

	// Script and style text must not reach the word count, or every page with
	// a big inline bundle would pass the thin-content check on its JavaScript.
	scripty := parsed(t,
		`<html><body><p>one two</p><script>var lorem = "ipsum dolor sit amet";</script></body></html>`,
		"https://example.com/")
	if scripty.WordCount != 2 {
		t.Errorf("word count = %d, want 2; script contents reached the visible text", scripty.WordCount)
	}
}

// Entities must be decoded, or "&nbsp;" counts as a word.
func TestParseHTMLDecodesEntities(t *testing.T) {
	p := parsed(t, `<html><body><p>one&nbsp;two&amp;three</p></body></html>`, "https://example.com/")
	if p.WordCount != 2 {
		t.Errorf("word count = %d, want 2; entities were not decoded", p.WordCount)
	}
}

func TestSameSite(t *testing.T) {
	cases := []struct {
		url, host string
		want      bool
	}{
		{"https://example.com/a", "example.com", true},
		{"https://www.example.com/a", "example.com", true},
		{"https://example.com/a", "www.example.com", true},
		{"https://other.example/a", "example.com", false},
		// A subdomain is a different site: it can have its own robots.txt and
		// its own sitemap, and crawling into it would silently widen the audit.
		{"https://blog.example.com/a", "example.com", false},
		{"not a url", "example.com", false},
	}
	for _, c := range cases {
		if got := sameSite(c.url, c.host); got != c.want {
			t.Errorf("sameSite(%q, %q) = %v, want %v", c.url, c.host, got, c.want)
		}
	}
}

func TestChecksFindTheObviousFailures(t *testing.T) {
	good := parsed(t, `<html lang="en"><head>
		<title>A title long enough to sit inside the recommended thirty to sixty</title>
		<meta name="description" content="A description written to be comfortably inside the recommended seventy to one hundred and sixty characters.">
		<meta name="viewport" content="width=device-width">
		<link rel="canonical" href="https://example.com/good">
		<link rel="icon" href="/f.ico">
		<meta property="og:title" content="t"><meta property="og:description" content="d">
		<meta property="og:image" content="i"><meta property="og:url" content="u">
		<meta name="twitter:card" content="summary">
	</head><body><h1>A heading of a perfectly reasonable length</h1></body></html>`,
		"https://example.com/good")

	bad := parsed(t, `<html><head><title>Short</title></head><body></body></html>`,
		"https://example.com/bad")

	result := &CrawlResult{
		StartURL: "https://example.com/good",
		Host:     "example.com",
		Pages: []*Page{
			page("https://example.com/good", good),
			page("https://example.com/bad", bad),
		},
		Robots:      RobotsCtx{Exists: true, ReferencesSitemap: true},
		SitemapURLs: []string{"https://example.com/good", "https://example.com/bad"},
		Compression: "gzip",
	}

	byURL := map[string][]string{}
	for _, i := range runChecks(result) {
		byURL[i.URL] = append(byURL[i.URL], i.Issue)
	}

	// The bad page must be caught for all of these.
	for _, want := range []string{
		"Page has no meta description",
		"Page has no h1",
		"Page has no canonical URL",
		"HTML lang attribute missing",
		"Viewport meta tag missing (mobile)",
		"Favicon link missing",
		"Twitter card meta tag missing",
	} {
		if !containsIssue(byURL["https://example.com/bad"], want) {
			t.Errorf("the deficient page was not flagged for %q", want)
		}
	}

	// And the good page must not be caught for any of them. This is the
	// direction that matters: a check that fires on everything is noise, and
	// noise gets a whole audit ignored.
	for _, unwanted := range []string{
		"Page has no title",
		"Page has no meta description",
		"Page has no h1",
		"Page has no canonical URL",
		"HTML lang attribute missing",
		"Viewport meta tag missing (mobile)",
		"Favicon link missing",
		"Twitter card meta tag missing",
		"Open Graph tags missing",
	} {
		if containsIssue(byURL["https://example.com/good"], unwanted) {
			t.Errorf("a well-formed page was flagged for %q", unwanted)
		}
	}
}

func containsIssue(issues []string, want string) bool {
	for _, i := range issues {
		if strings.HasPrefix(i, want) {
			return true
		}
	}
	return false
}

// TestRedirectChainCheckFires guards a condition that is easy to make
// unreachable by recording a one-element chain and testing for more than two.
func TestRedirectChainCheckFires(t *testing.T) {
	result := &CrawlResult{
		StartURL: "https://example.com/",
		Host:     "example.com",
		Pages: []*Page{
			{URL: "https://example.com/final", RequestedURL: "http://example.com", Status: 200, RedirectHops: 2},
			{URL: "https://example.com/one-hop", RequestedURL: "http://example.com/one-hop", Status: 200, RedirectHops: 1},
			{URL: "https://example.com/direct", RequestedURL: "https://example.com/direct", Status: 200},
		},
	}

	var flagged []string
	for _, i := range runChecks(result) {
		if strings.HasPrefix(i.Issue, "Redirect chain") {
			flagged = append(flagged, i.URL)
		}
	}

	if len(flagged) != 1 || flagged[0] != "https://example.com/final" {
		t.Errorf("redirect chain check flagged %v; want only the two-hop URL", flagged)
	}
}

// The same crawl must produce the same findings in the same order every time,
// or a weekly report diffs as changed when nothing did.
func TestDuplicateChecksAreOrdered(t *testing.T) {
	build := func() []*Page {
		var pages []*Page
		for _, slug := range []string{"a", "b", "c", "d", "e", "f"} {
			url := "https://example.com/" + slug
			pages = append(pages, page(url, parsed(t,
				`<html><head><title>Same title everywhere</title></head><body></body></html>`, url)))
		}
		return pages
	}

	var first []string
	for run := range 8 {
		result := &CrawlResult{StartURL: "https://example.com/", Host: "example.com", Pages: build()}
		var order []string
		for _, i := range runChecks(result) {
			if i.Issue == "Duplicate title" {
				order = append(order, i.URL)
			}
		}
		if len(order) != 6 {
			t.Fatalf("run %d: expected 6 duplicate-title findings, got %d", run, len(order))
		}
		if first == nil {
			first = order
			continue
		}
		for i := range order {
			if order[i] != first[i] {
				t.Fatalf("run %d produced a different ordering than run 0:\n %v\n %v", run, first, order)
			}
		}
	}
}

func TestLighthouseScorePairsAreOrdered(t *testing.T) {
	s := &Scores{Performance: 1, Accessibility: 2, BestPractices: 3, SEO: 4}
	want := []string{"Performance", "Accessibility", "Best practices", "SEO"}
	for run := range 8 {
		for i, pair := range s.Pairs() {
			if pair.Label != want[i] {
				t.Fatalf("run %d: score %d is %q, want %q", run, i, pair.Label, want[i])
			}
		}
	}
}

// --- lighthouse parsing --------------------------------------------------

func TestParseScoresRejectsNulls(t *testing.T) {
	report := map[string]any{"categories": map[string]any{
		"performance":    map[string]any{"score": 0.98},
		"accessibility":  map[string]any{"score": nil},
		"best-practices": map[string]any{"score": 0.96},
		"seo":            map[string]any{"score": 0.91},
	}}

	// A null score means the category could not be evaluated. Storing it as
	// zero would draw a red bar claiming the site scored nothing, which is a
	// much worse claim than "the audit did not complete".
	if _, err := parseScores(report); err == nil {
		t.Fatal("a null score was accepted instead of failing the audit")
	}
}

func TestParseScoresRounds(t *testing.T) {
	report := map[string]any{"categories": map[string]any{
		"performance":    map[string]any{"score": 0.985},
		"accessibility":  map[string]any{"score": 1.0},
		"best-practices": map[string]any{"score": 0.0},
		"seo":            map[string]any{"score": 0.914},
	}}
	s, err := parseScores(report)
	if err != nil {
		t.Fatal(err)
	}
	if s.Performance != 99 || s.Accessibility != 100 || s.BestPractices != 0 || s.SEO != 91 {
		t.Errorf("scores rounded to %+v", s)
	}
}

func TestParseDetailsFiltersNonActionableAudits(t *testing.T) {
	report := map[string]any{
		"categories": map[string]any{"performance": map[string]any{"auditRefs": []any{
			map[string]any{"id": "lcp", "group": "metrics", "weight": 25.0, "acronym": "LCP"},
			map[string]any{"id": "passing", "weight": 0.0},
			map[string]any{"id": "hidden-audit", "group": "hidden", "weight": 0.0},
			map[string]any{"id": "diagnostic", "weight": 0.0},
			map[string]any{"id": "real-opportunity", "weight": 0.0},
		}}},
		"audits": map[string]any{
			"lcp":     map[string]any{"score": 0.4, "title": "Largest Contentful Paint", "displayValue": "3.1 s"},
			"passing": map[string]any{"score": 0.95, "title": "Already fine"},
			// Kept for compatibility by Lighthouse but no longer scored, so
			// it must not appear as a win the site never earned.
			"hidden-audit": map[string]any{"score": 0.1, "title": "Time to Interactive"},
			// Scores badly but carries no actionable saving.
			"diagnostic": map[string]any{"score": 0.1, "title": "Avoid forced reflow"},
			"real-opportunity": map[string]any{
				"score": 0.2, "title": "Reduce unused CSS",
				"details": map[string]any{"overallSavingsMs": 1350.0},
			},
		},
	}

	d := parseDetails(report)
	if d == nil {
		t.Fatal("details did not parse")
	}
	if len(d.Metrics) != 1 || d.Metrics[0].Acronym != "LCP" {
		t.Errorf("metrics = %+v", d.Metrics)
	}
	if len(d.Opportunities) != 1 || d.Opportunities[0].ID != "real-opportunity" {
		t.Errorf("opportunities = %+v; only the one with a real saving should survive", d.Opportunities)
	}
}

// --- helpers -------------------------------------------------------------

func TestSafeNext(t *testing.T) {
	cases := map[string]string{
		"/properties": "/properties",
		"/a/b?c=d":    "/a/b?c=d",
		// Protocol-relative and backslash forms both leave the site.
		"//evil.example":       "/properties",
		"/\\evil.example":      "/properties",
		"https://evil.example": "/properties",
		"":                     "/properties",
	}
	for in, want := range cases {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSessionRoundTrip(t *testing.T) {
	key := sessionKey("hunter2")

	rec := httptest.NewRecorder()
	issueSession(rec, key)
	cookie := rec.Result().Cookies()[0]

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(cookie)
	if !isAuthenticated(req, key) {
		t.Error("a freshly issued session did not authenticate")
	}

	// Rotating the password must end every outstanding session, which is why
	// the key is derived from it.
	if isAuthenticated(req, sessionKey("hunter3")) {
		t.Error("a session survived a password change")
	}

	// A tampered payload must not authenticate.
	tampered := httptest.NewRequest(http.MethodGet, "/", nil)
	tampered.AddCookie(&http.Cookie{
		Name:  sessionCookie,
		Value: "1:9999999999." + strings.Repeat("A", 43),
	})
	if isAuthenticated(tampered, key) {
		t.Error("a forged cookie authenticated")
	}
}

func TestPasswordMatchesIsExact(t *testing.T) {
	if !passwordMatches("hunter2", "hunter2") {
		t.Error("the correct password was rejected")
	}
	for _, wrong := range []string{"hunter", "hunter22", "Hunter2", ""} {
		if passwordMatches(wrong, "hunter2") {
			t.Errorf("%q was accepted", wrong)
		}
	}
}

func TestAsciiFilename(t *testing.T) {
	cases := map[string]string{
		"example.com": "example.com",
		// A non-ASCII byte cannot go into a header value unencoded.
		"Café":     "Caf_",
		"...":      "report",
		"  spaced": "spaced",
		"":         "report",
	}
	for in, want := range cases {
		if got := asciiFilename(in); got != want {
			t.Errorf("asciiFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPropertyName(t *testing.T) {
	cases := map[string]string{
		"https://www.example.com/path": "example.com",
		"https://example.com":          "example.com",
		"https://example.com:8443/":    "example.com",
		// Not a URL at all: the whole string, rather than a panic or an empty
		// name that would render as a blank row.
		"nonsense": "nonsense",
	}
	for in, want := range cases {
		p := &Property{URL: in}
		if got := p.Name(); got != want {
			t.Errorf("Name() for %q = %q, want %q", in, got, want)
		}
	}
}

func TestNext3MinBoundaryIsAligned(t *testing.T) {
	next := time.UnixMilli(next3MinBoundary()).UTC()
	if next.Second() != 0 || next.Nanosecond() != 0 || next.Minute()%3 != 0 {
		t.Errorf("next run at %s is not on a three-minute boundary", next)
	}
	if !next.After(time.Now().UTC()) {
		t.Errorf("next run at %s is not in the future", next)
	}
}

func TestNaturalTime(t *testing.T) {
	now := time.Now()
	cases := []struct {
		in   *time.Time
		want string
	}{
		{nil, "never"},
		{ptrTime(now.Add(-30 * time.Second)), "just now"},
		{ptrTime(now.Add(-2 * time.Minute)), "2 minutes ago"},
		{ptrTime(now.Add(-1 * time.Hour)), "1 hour ago"},
		{ptrTime(now.Add(48 * time.Hour)), "2 days from now"},
	}
	for _, c := range cases {
		if got := naturalTime(c.in); got != c.want {
			t.Errorf("naturalTime = %q, want %q", got, c.want)
		}
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

func TestIntcomma(t *testing.T) {
	cases := map[int64]string{
		0: "0", 999: "999", 1000: "1,000", 130042: "130,042",
		1234567: "1,234,567", -4321: "-4,321",
	}
	for in, want := range cases {
		if got := intcomma(in); got != want {
			t.Errorf("intcomma(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestJSONBlockDoesNotDoubleEscape(t *testing.T) {
	// The values here are what a crawled site can put in an error string.
	out, err := jsonBlock(map[string]string{"issue": `a <b> & "c"`})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), `\u003c`) {
		t.Errorf("JSON was HTML-escaped before html/template saw it: %s", out)
	}
	if !strings.Contains(string(out), "<b>") {
		t.Errorf("the angle brackets did not survive verbatim: %s", out)
	}
}

// TestStatusPayloadShape pins the field names the dashboard JavaScript reads.
// Renaming any of them silently breaks the live panel with no server error.
func TestStatusPayloadShape(t *testing.T) {
	pages := int64(12)
	p := &Property{
		CrawlState: "running", LighthouseState: "idle",
		LastCrawlPagesCount: &pages,
	}
	encoded, err := json.Marshal(buildStatusPayload(p))
	if err != nil {
		t.Fatal(err)
	}

	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"crawler", "lighthouse", "server_time"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("status payload is missing %q", key)
		}
	}
	crawler := payload["crawler"].(map[string]any)
	for _, key := range []string{
		"state", "started_at", "last_attempt_at", "last_success_at", "last_error",
		"last_duration_ms", "pages_count", "next_run_at", "is_overdue",
		"insights_total", "insights_by_severity", "progress",
	} {
		if _, ok := crawler[key]; !ok {
			t.Errorf("crawler status is missing %q", key)
		}
	}
	// A running crawl must report progress, or the bar never appears.
	if crawler["progress"] == nil {
		t.Error("a running crawl reported no progress")
	}
}

func TestCrawlProgressStaysBelowComplete(t *testing.T) {
	huge := int64(PageCap * 10)
	p := &Property{CrawlState: "running", LastCrawlPagesCount: &huge}
	progress := crawlProgress(p)
	if progress == nil || *progress > 0.9 {
		t.Errorf("progress = %v; a running crawl must never claim to be finished", progress)
	}

	idle := &Property{CrawlState: "idle"}
	if crawlProgress(idle) != nil {
		t.Error("an idle crawl reported progress")
	}
}

// reportTemplates is a package level Must(), so a broken report template panics
// at init and takes the whole binary down rather than one route.
func TestReportsParse(t *testing.T) {
	var names []string
	for _, tmpl := range reportTemplates.Templates() {
		names = append(names, tmpl.Name())
	}
	for _, want := range []string{"report.typ", "report.md"} {
		found := false
		for _, name := range names {
			if name == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s was not parsed; got %v", want, names)
		}
	}
}
