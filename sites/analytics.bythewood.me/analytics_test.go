package main

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The tests here cover the functions where a mistake is silent: escaping that
// produces a valid-looking document with the wrong content, auth that lets the
// wrong request through, and arithmetic that renders a plausible wrong number.
// Everything a compiler or an obviously broken page would catch is left alone.

// The "/" case is the one that matters and the one that is not obvious. A page
// URL is user data, "//" opens a Typst line comment, and the result is a
// report that silently loses the rest of a line rather than failing.
func TestTypstMD(t *testing.T) {
	tests := []struct{ in, want string }{
		{"/blog/post", `\/blog\/post`},
		{"https://example.com//x", `https:\/\/example.com\/\/x`},
		{"a*b_c`d", `a\*b\_c\` + "`" + `d`},
		{"#set page", `\#set page`},
		{"[link]", `\[link\]`},
		{"a@b", `a\@b`},
		{"plain text", "plain text"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := typstMD(tt.in); got != tt.want {
			t.Errorf("typstMD(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestTypstStr(t *testing.T) {
	tests := []struct{ in, want string }{
		{`say "hi"`, `say \"hi\"`},
		{`back\slash`, `back\\slash`},
		{"line\nbreak", `line\nbreak`},
		// Markup metacharacters are harmless inside a string literal, so
		// escaping them here would put visible backslashes in the PDF.
		{"a/b#c", "a/b#c"},
	}
	for _, tt := range tests {
		if got := typstStr(tt.in); got != tt.want {
			t.Errorf("typstStr(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// A property named "Café" panicked the Rust version outright when its report
// was downloaded, because a non-ASCII byte cannot go into a header value.
func TestASCIIFilename(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Café", "Caf_"},
		{"my site.com", "my site.com"},
		// Leading dots are stripped, so this cannot become a hidden file.
		{"../../etc/passwd", "_.._etc_passwd"},
		{`quote"inject`, "quote_inject"},
		{"...", "report"},
		{"", "report"},
		{"   ", "report"},
	}
	for _, tt := range tests {
		got := asciiFilename(tt.in)
		if got != tt.want {
			t.Errorf("asciiFilename(%q) = %q, want %q", tt.in, got, tt.want)
		}
		for _, r := range got {
			if r > 127 {
				t.Errorf("asciiFilename(%q) = %q, which is not ASCII", tt.in, got)
			}
		}
	}
}

// Referrers are grouped by this value, so getting it wrong does not error, it
// just splits one source into several rows that should have been one.
func TestNormalizeReferrer(t *testing.T) {
	tests := []struct{ in, want string }{
		{"https://www.google.com/search?q=x", "google.com"},
		{"http://news.ycombinator.com/", "news.ycombinator.com"},
		{"https://GitHub.com/overshard", "github.com"},
		{"example.com/path", "example.com"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := normalizeReferrer(tt.in); got != tt.want {
			t.Errorf("normalizeReferrer(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// "//evil.example" starts with a slash and is still off-site: browsers read it
// as protocol-relative. A naive prefix check would pass it straight through.
func TestSafeNext(t *testing.T) {
	tests := []struct{ in, want string }{
		{"/properties", "/properties"},
		{"/86ee87e3-f1f2-46da-8a3e-d719fbece1b9", "/86ee87e3-f1f2-46da-8a3e-d719fbece1b9"},
		{"//evil.example", "/properties"},
		{`/\evil.example`, "/properties"},
		{"https://evil.example", "/properties"},
		{"", "/properties"},
	}
	for _, tt := range tests {
		if got := safeNext(tt.in); got != tt.want {
			t.Errorf("safeNext(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// The session cookie is the entire authentication system, so its failure modes
// are pinned rather than reasoned about.
func TestSession(t *testing.T) {
	key := sessionKey("correct horse")

	t.Run("round trips", func(t *testing.T) {
		w := httptest.NewRecorder()
		issueSession(w, key)

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		for _, c := range w.Result().Cookies() {
			r.AddCookie(c)
		}
		if !isAuthenticated(r, key) {
			t.Error("a freshly issued cookie did not authenticate")
		}
	})

	t.Run("a different password rejects it", func(t *testing.T) {
		w := httptest.NewRecorder()
		issueSession(w, key)

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		for _, c := range w.Result().Cookies() {
			r.AddCookie(c)
		}
		// Rotating the password must invalidate outstanding sessions, which is
		// the whole reason the key is derived from it rather than configured.
		if isAuthenticated(r, sessionKey("different")) {
			t.Error("a cookie survived the password changing")
		}
	})

	t.Run("a tampered expiry is rejected", func(t *testing.T) {
		// The forgery a signature exists to stop: take a valid payload and
		// push the expiry out, keeping the old signature.
		far := time.Now().Add(999 * time.Hour).Unix()
		payload := "1:" + strconv.FormatInt(far, 10)

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: payload + "." + sign(sessionKey("wrong"), payload)})
		if isAuthenticated(r, key) {
			t.Error("a cookie signed with the wrong key authenticated")
		}
	})

	t.Run("an expired cookie is rejected", func(t *testing.T) {
		payload := "1:" + strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: payload + "." + sign(key, payload)})
		if isAuthenticated(r, key) {
			t.Error("an expired cookie authenticated")
		}
	})

	t.Run("garbage is rejected", func(t *testing.T) {
		for _, value := range []string{"", "nonsense", "1:999999999999", "1:abc.sig", "."} {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.AddCookie(&http.Cookie{Name: sessionCookie, Value: value})
			if isAuthenticated(r, key) {
				t.Errorf("cookie value %q authenticated", value)
			}
		}
	})
}

func TestPasswordMatches(t *testing.T) {
	if !passwordMatches("hunter2", "hunter2") {
		t.Error("the correct password did not match")
	}
	for _, wrong := range []string{"hunter", "hunter22", "", "HUNTER2"} {
		if passwordMatches(wrong, "hunter2") {
			t.Errorf("%q matched", wrong)
		}
	}
}

// Zero previous must not become an infinite or 100% increase, which would put
// a delta on every card of a property's first week.
func TestPctChange(t *testing.T) {
	tests := []struct {
		cur, prev float64
		want      int64
	}{
		{100, 100, 0},
		{120, 100, 20},
		{80, 100, -20},
		{5, 0, 0},
		{0, 0, 0},
		{0, 100, -100},
	}
	for _, tt := range tests {
		if got := pctChange(tt.cur, tt.prev); got != tt.want {
			t.Errorf("pctChange(%v, %v) = %d, want %d", tt.cur, tt.prev, got, tt.want)
		}
	}
}

// The single-point case divides by len-1, which is zero.
func TestChartPolyline(t *testing.T) {
	if got := chartPolyline(nil); got != "" {
		t.Errorf("empty graph = %q, want empty", got)
	}

	one := chartPolyline([]GraphPoint{{Label: "Jan 1", Count: 5}})
	if !strings.HasPrefix(one, "300.0,") {
		t.Errorf("single point = %q, want it centred at x=300", one)
	}

	many := chartPolyline([]GraphPoint{{Count: 0}, {Count: 10}, {Count: 5}})
	parts := strings.Fields(many)
	if len(parts) != 3 {
		t.Fatalf("three points produced %d coordinates: %q", len(parts), many)
	}
	// The peak pins to the top of the usable band, the zero to the bottom.
	if !strings.HasPrefix(parts[0], "0.0,96.0") {
		t.Errorf("zero count = %q, want it on the baseline at y=96", parts[0])
	}
	if !strings.HasPrefix(parts[1], "300.0,4.0") {
		t.Errorf("peak = %q, want it at the top at y=4", parts[1])
	}
	if !strings.HasPrefix(parts[2], "600.0,") {
		t.Errorf("last point = %q, want it at x=600", parts[2])
	}
}

// A bot counted as a human inflates every metric on the dashboard, and nothing
// about the number looks wrong afterwards.
func TestBotClassification(t *testing.T) {
	bots := []string{
		"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
		"facebookexternalhit/1.1",
		"Mozilla/5.0 (X11; Linux x86_64) HeadlessChrome/120.0.0.0",
		"UptimeRobot/2.0",
	}
	for _, ua := range bots {
		if !looksLikeBot(ua) {
			t.Errorf("%q was not classified as a bot", ua)
		}
	}

	humans := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 Version/17.4 Mobile/15E148 Safari/604.1",
	}
	for _, ua := range humans {
		if looksLikeBot(ua) {
			t.Errorf("%q was misclassified as a bot", ua)
		}
	}
}

// Tablet is tested before mobile because an iPad's User-Agent contains neither
// "mobile" nor "iphone", while an Android tablet's contains "mobile".
func TestClassifyDevice(t *testing.T) {
	tests := []struct{ ua, family, want string }{
		{"Mozilla/5.0 (iPad; CPU OS 17_4 like Mac OS X)", "iPad", "Tablet"},
		{"Mozilla/5.0 (Linux; Android 14; SM-X200) Mobile Safari", "Tablet", "Tablet"},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_4)", "iPhone", "Mobile"},
		{"Mozilla/5.0 (Linux; Android 14; Pixel 8) Mobile Safari", "", "Mobile"},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/131", "", "Desktop"},
	}
	for _, tt := range tests {
		if got := classifyDevice(tt.ua, tt.family); got != tt.want {
			t.Errorf("classifyDevice(%q, %q) = %q, want %q", tt.ua, tt.family, got, tt.want)
		}
	}
}

// Truncating on a byte boundary would cut a multi-byte character in half and
// store invalid UTF-8.
func TestClampRunes(t *testing.T) {
	if got := clampRunes("hello", 10); got != "hello" {
		t.Errorf("short string was altered: %q", got)
	}
	got := clampRunes(strings.Repeat("é", 100), 10)
	if len([]rune(got)) != 10 {
		t.Errorf("clamped to %d runes, want 10", len([]rune(got)))
	}
	if !isValidUTF8(got) {
		t.Errorf("clamping produced invalid UTF-8: %q", got)
	}
}

func isValidUTF8(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}
	return true
}

// encodeExtra must not escape < and > into <: the value is stored, not
// rendered, and a URL that went in with a "<" has to come back with one.
func TestEncodeExtra(t *testing.T) {
	if got := encodeExtra(nil); got != "{}" {
		t.Errorf("empty extra = %q, want {}", got)
	}
	got := encodeExtra(map[string]any{"note": "a<b & c>d"})
	if !strings.Contains(got, "a<b & c>d") {
		t.Errorf("encodeExtra escaped HTML in stored data: %q", got)
	}
}

// The date bounds decide which events a dashboard counts.
func TestParseDateToMS(t *testing.T) {
	start, ok := parseDateToMS("2026-05-09", false)
	if !ok {
		t.Fatal("a valid date failed to parse")
	}
	end, ok := parseDateToMS("2026-05-09", true)
	if !ok {
		t.Fatal("a valid end date failed to parse")
	}
	// One second short of a full day: 23:59:59, matching the Rust bounds.
	if d := end - start; d != 86399*1000 {
		t.Errorf("end minus start = %dms, want %dms", d, 86399*1000)
	}
	for _, bad := range []string{"", "not-a-date", "2026-13-45", "05/09/2026"} {
		if _, ok := parseDateToMS(bad, false); ok {
			t.Errorf("%q parsed as a date", bad)
		}
	}
}

// The two non-integer card values are formatted through this, and %v would
// flip to scientific notation at the top of the range.
func TestTrimFloat(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{0, "0"},
		{60.94, "60.94"},
		{100, "100"},
		{1200000, "1200000"},
	}
	for _, tt := range tests {
		if got := trimFloat(tt.in); got != tt.want {
			t.Errorf("trimFloat(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
