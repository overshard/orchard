package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testDeps(base string) *Deps {
	return &Deps{HTTP: &http.Client{Timeout: 5 * time.Second}, Now: time.Now, Guard: NewGuard(time.Minute)}
}

// Without a session these tools have no way to prove who is asking, and the
// failure has to be local instead of an unauthenticated request going out.
func TestEstateToolsRefuseWithoutASession(t *testing.T) {
	reached := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
	}))
	defer srv.Close()

	var out any
	err := estateGet(context.Background(), testDeps(srv.URL).Turn(""), srv.URL, &out)
	if err == nil {
		t.Fatal("an unsigned request was allowed")
	}
	if !strings.Contains(err.Error(), "signed in") {
		t.Errorf("err = %q, want it to name the reason", err)
	}
	if reached {
		t.Error("a request went out with no session on it")
	}
}

func TestEstateToolsForwardTheSession(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(SessionCookie); err == nil {
			got = c.Value
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()

	d := testDeps(srv.URL).Turn("a-live-session")
	var out map[string]any
	if err := estateGet(context.Background(), d, srv.URL, &out); err != nil {
		t.Fatal(err)
	}
	if got != "a-live-session" {
		t.Errorf("cookie forwarded as %q", got)
	}
	if out["ok"] != true {
		t.Errorf("body = %v", out)
	}
}

// Each turn carries its own session and widgets, and the Deps every turn
// shares has nowhere to put either.
func TestTurnsDoNotShareASessionOrWidgets(t *testing.T) {
	base := testDeps("")
	a := base.Turn("one")
	b := base.Turn("two")
	if a.Session != "one" || b.Session != "two" {
		t.Errorf("turns crossed: %q and %q", a.Session, b.Session)
	}
	if a.Widgets == b.Widgets {
		t.Error("two turns share a widget sink")
	}
	if a.Deps != base || b.Deps != base {
		t.Error("a turn is not on the shared Deps")
	}
}

// A redirect to the login page is a sign in prompt, and decoding it as JSON
// would report a parse error instead of the real problem.
func TestEstateToolsReadARedirectAsSignedOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://auth.bythewood.me/login", http.StatusSeeOther)
	}))
	defer srv.Close()

	d := testDeps(srv.URL).Turn("stale")
	// The client must not follow it, or the test proves nothing.
	d.HTTP = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	var out any
	err := estateGet(context.Background(), d, srv.URL, &out)
	if err == nil || !strings.Contains(err.Error(), "sign in") {
		t.Errorf("err = %v, want a sign in", err)
	}
}

func TestEstateToolsReportARefusedSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	var out any
	err := estateGet(context.Background(), testDeps(srv.URL).Turn("revoked"), srv.URL, &out)
	if err == nil || !strings.Contains(err.Error(), "signed out") {
		t.Errorf("err = %v, want it to say the session was refused", err)
	}
}

// Every orchard tool has to be offered, or one exists in the dispatch table and
// not in the schemas the model sees.
func TestOrchardToolsAreRegistered(t *testing.T) {
	r := Default()
	for _, name := range []string{"orchard_logs", "orchard_status", "orchard_analytics", "orchard_repos", "orchard_dash"} {
		if _, ok := r.Get(name); !ok {
			t.Errorf("%s is not registered", name)
		}
	}
}

// A model totalling a statement writes every line into one expression and runs
// out of tokens partway, so the call arrives cut in half. The cap turns that
// into advice it can act on instead of a sum of whatever survived.
func TestCalcRefusesAnExpressionTooLongToBeWhole(t *testing.T) {
	long := strings.TrimSuffix(strings.Repeat("14.45+", 200), "+")
	_, err := Calc.Run(context.Background(), testDeps("").Turn(""), map[string]any{"expression": long})
	if err == nil {
		t.Fatal("a runaway expression was evaluated")
	}
	if !strings.Contains(err.Error(), "groups") {
		t.Errorf("err = %q, want it to say what to do instead", err)
	}
	// An ordinary sum still works.
	got, err := Calc.Run(context.Background(), testDeps("").Turn(""), map[string]any{"expression": "14.45+10.70+15.52"})
	if err != nil {
		t.Fatalf("an ordinary sum was refused: %v", err)
	}
	if !strings.Contains(fmt.Sprint(got), "40.67") {
		t.Errorf("got %v", got)
	}
}

// news reads every feed on the list, so a second call in one turn re-reads all
// of them for a rundown it already has, and the harness has to be able to take a
// tool off the table mid turn.
func TestWithoutTakesAToolOffTheTable(t *testing.T) {
	all := []map[string]any{
		{"type": "function", "function": map[string]any{"name": News.Name}},
		{"type": "function", "function": map[string]any{"name": WebSearch.Name}},
	}
	trimmed := Without(all, News.Name)
	if len(trimmed) != 1 {
		t.Fatalf("%d schemas left, want 1", len(trimmed))
	}
	for _, sc := range trimmed {
		fn, _ := sc["function"].(map[string]any)
		if fn["name"] == News.Name {
			t.Error("news survived being removed")
		}
	}
	if len(all) != 2 {
		t.Error("the original list was modified")
	}
}

func TestOrchardCodeReadsAFileAndListsADirectory(t *testing.T) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path)
		switch r.URL.Path {
		case "/api/repos/orchard/file/HEAD/sites/chat.bythewood.me/tools/web.go":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"repo": "orchard", "path": "tools/web.go", "text": "package tools", "lines": 1})
		case "/api/repos/orchard/tree/HEAD/sites":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"repo": "orchard", "path": "sites", "count": 1,
				"entries": []map[string]any{{"name": "chat.bythewood.me", "type": "tree"}}})
		default:
			http.Error(w, `{"error":"no such path"}`, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	old := reposBase
	reposBase = srv.URL
	defer func() { reposBase = old }()
	d := testDeps(srv.URL).Turn("a-live-session")

	got, err := OrchardCode.Run(context.Background(), d,
		map[string]any{"repo": "orchard", "path": "sites/chat.bythewood.me/tools/web.go"})
	if err != nil {
		t.Fatalf("reading a file failed: %v", err)
	}
	if m, ok := got.(map[string]any); !ok || m["text"] != "package tools" {
		t.Fatalf("the file came back as %#v", got)
	}

	// A directory is not a file, and the 404 on the file read has to fall
	// through to the listing instead of being reported as a missing path.
	got, err = OrchardCode.Run(context.Background(), d,
		map[string]any{"repo": "orchard", "path": "sites"})
	if err != nil {
		t.Fatalf("listing a directory failed: %v", err)
	}
	if m, ok := got.(map[string]any); !ok || m["count"] != float64(1) {
		t.Fatalf("the listing came back as %#v", got)
	}

	// A path that is neither says so and points at find with the file name,
	// since the commonest mistake is dropping a directory off the front and an
	// empty listing reads as an empty directory.
	_, err = OrchardCode.Run(context.Background(), d,
		map[string]any{"repo": "orchard", "path": "sites/nope/nothing.go"})
	if err == nil {
		t.Fatal("a path that does not exist came back as a result")
	}
	for _, want := range []string{"action find", "nothing.go"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %q, want it to name %q", err, want)
		}
	}
	_ = asked
}

// A path is one wildcard on the other side, so escaping it whole would turn
// every separator into %2F and nothing would ever resolve.
func TestOrchardCodeKeepsPathSeparators(t *testing.T) {
	if got, want := escapePath("sites/chat.bythewood.me/tools/web.go"),
		"sites/chat.bythewood.me/tools/web.go"; got != want {
		t.Errorf("escapePath = %q, want %q", got, want)
	}
	if got := escapePath("a dir/a file.go"); got != "a%20dir/a%20file.go" {
		t.Errorf("escapePath left a space unescaped: %q", got)
	}
}

// The whole dashboard is about six thousand tokens and a question about
// earnings wants one panel of it, which then sits in the conversation for the
// rest of it.
func TestDashSectionTakesOnePanel(t *testing.T) {
	state := map[string]any{
		"earnings": map[string]any{"reported": []any{"AVGO"}},
		"weather":  map[string]any{"temperature": 88},
		"air":      map[string]any{"aqi": 49},
	}

	got, err := dashSection(state, "earnings")
	if err != nil {
		t.Fatalf("earnings: %v", err)
	}
	m, ok := got.(map[string]any)
	if !ok || m["earnings"] == nil {
		t.Fatalf("earnings section = %#v", got)
	}
	if m["weather"] != nil || m["air"] != nil {
		t.Error("the other panels came with it")
	}

	// Empty is the whole thing.
	whole, err := dashSection(state, "")
	if err != nil || len(whole.(map[string]any)) != 3 {
		t.Errorf("no section = %#v, %v", whole, err)
	}
}

// A name that is not there has to say what is, or the next call is another
// guess at a panel name.
func TestDashSectionNamesThePanels(t *testing.T) {
	_, err := dashSection(map[string]any{"earnings": 1, "weather": 2}, "stonks")
	if err == nil {
		t.Fatal("a missing panel should be an error")
	}
	for _, want := range []string{"earnings", "weather"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

// find and search let the first call land on the file, where walking the tree
// guesses a path without its prefix and runs out of rounds.
func TestOrchardCodeFindsAndSearches(t *testing.T) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.RequestURI())
		switch r.URL.Path {
		case "/api/repos/orchard/find/HEAD":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"paths": []string{"sites/dash.bythewood.me/earnings.go"}, "count": 1})
		case "/api/repos/orchard/grep/HEAD":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"hits": []map[string]any{{
					"path": "sites/dash.bythewood.me/earnings.go", "line": 31,
					"text": "\tearningsShown = 3"}}, "count": 1})
		default:
			http.Error(w, `{"error":"no such path"}`, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	old := reposBase
	reposBase = srv.URL
	defer func() { reposBase = old }()
	d := testDeps(srv.URL).Turn("a-live-session")

	got, err := OrchardCode.Run(context.Background(), d,
		map[string]any{"repo": "orchard", "action": "find", "query": "earnings.go"})
	if err != nil {
		t.Fatalf("find failed: %v", err)
	}
	if m, _ := got.(map[string]any); m["count"] != float64(1) {
		t.Fatalf("find came back as %#v", got)
	}

	got, err = OrchardCode.Run(context.Background(), d, map[string]any{
		"repo": "orchard", "action": "search", "query": "earningsShown", "path": "sites/dash.bythewood.me"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if m, _ := got.(map[string]any); m["count"] != float64(1) {
		t.Fatalf("search came back as %#v", got)
	}
	if !strings.Contains(asked[1], "path=sites") {
		t.Errorf("the pathspec did not reach the server: %q", asked[1])
	}

	// A query with no action is a search, since a model that fills the argument
	// and forgets the verb has still said what it wants.
	if _, err := OrchardCode.Run(context.Background(), d,
		map[string]any{"repo": "orchard", "query": "earningsShown"}); err != nil {
		t.Errorf("a bare query should search: %v", err)
	}
	if len(asked) != 3 || !strings.HasPrefix(asked[2], "/api/repos/orchard/grep/") {
		t.Errorf("a bare query went to %q", asked[len(asked)-1])
	}
}

// A long file read whole is context the window pays for until the conversation
// ends, so the line range has to reach the server.
func TestOrchardCodeReadsALineRange(t *testing.T) {
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.RequestURI()
		_ = json.NewEncoder(w).Encode(map[string]any{"text": "func walkEarnings(", "from": 140, "to": 160})
	}))
	defer srv.Close()

	old := reposBase
	reposBase = srv.URL
	defer func() { reposBase = old }()
	d := testDeps(srv.URL).Turn("a-live-session")

	if _, err := OrchardCode.Run(context.Background(), d, map[string]any{
		"repo": "orchard", "action": "read",
		"path": "sites/dash.bythewood.me/earnings.go", "from": 140, "to": 160}); err != nil {
		t.Fatalf("range read failed: %v", err)
	}
	for _, want := range []string{"from=140", "to=160"} {
		if !strings.Contains(asked, want) {
			t.Errorf("asked %q, want %s", asked, want)
		}
	}
}

// The model passes GitHub's name for a repository, like "overshard/orchard".
func TestARepoIsFoundByItsGitHubName(t *testing.T) {
	for _, in := range []string{"orchard", "overshard/orchard", "https://github.com/overshard/orchard", "github.com/overshard/orchard.git", "/orchard/"} {
		if got := repoName(in); got != "orchard" {
			t.Errorf("repoName(%q) = %q", in, got)
		}
	}
}

// Asked where a constant is set, the model reaches for find, which only matches
// file names.
func TestFindFallsBackToSearchingInsideFiles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/repos/orchard/find/HEAD":
			_ = json.NewEncoder(w).Encode(map[string]any{"count": 0, "paths": []string{}})
		case "/api/repos/orchard/grep/HEAD":
			_ = json.NewEncoder(w).Encode(map[string]any{"count": 1, "hits": []map[string]any{
				{"path": "sites/chat.bythewood.me/chat.go", "line": 21, "text": "maxToolRounds = 6"}}})
		default:
			http.Error(w, "", http.StatusNotFound)
		}
	}))
	defer srv.Close()
	old := reposBase
	reposBase = srv.URL
	defer func() { reposBase = old }()
	d := testDeps(srv.URL).Turn("a-live-session")

	got, err := OrchardCode.Run(context.Background(), d,
		map[string]any{"repo": "overshard/orchard", "action": "find", "query": "maxToolRounds"})
	if err != nil {
		t.Fatal(err)
	}
	if m, _ := got.(map[string]any); m["hits"] == nil || m["note"] == nil {
		t.Errorf("an empty find did not search the files: %#v", got)
	}
}

func TestDashSectionTakesAPluralForASingularPanel(t *testing.T) {
	got, err := dashSection(map[string]any{"market": 1, "alerts": 2}, "markets")
	if err != nil || got.(map[string]any)["market"] != 1 {
		t.Errorf("markets did not find market: %v %v", got, err)
	}
	if got, err := dashSection(map[string]any{"market": 1, "alerts": 2}, "alert"); err != nil || got.(map[string]any)["alerts"] != 2 {
		t.Errorf("alert did not find alerts: %v %v", got, err)
	}
}

func TestAnalyticsNamesAPropertyThatIsNotThere(t *testing.T) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Query().Get("property"))
		props := []any{}
		if r.URL.Query().Get("property") == "" {
			props = append(props, map[string]any{"name": "blog.bythewood.me", "page_views": 48})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"days": 7, "properties": props})
	}))
	defer srv.Close()
	defer func(was string) { analyticsBase = was }(analyticsBase)
	analyticsBase = srv.URL

	got, err := OrchardAnalytics.Run(context.Background(), testDeps(srv.URL).Turn("s"), map[string]any{"property": "my blog"})
	if err != nil {
		t.Fatal(err)
	}
	m := got.(map[string]any)
	if props, _ := m["properties"].([]any); len(props) != 1 {
		t.Errorf("properties = %v, want every one after the name missed", m["properties"])
	}
	if note, _ := m["note"].(string); !strings.Contains(note, "my blog") {
		t.Errorf("note = %q, want it to say the name missed", note)
	}
	if strings.Join(asked, ",") != "my blog," {
		t.Errorf("asked for %q", asked)
	}
}

func TestAnalyticsRowsPutTheLabelFirst(t *testing.T) {
	m := map[string]any{"page_views": 49.0, "top_pages": []any{
		map[string]any{"count": 15.0, "label": "/"},
		map[string]any{"count": 8.0, "label": "/posts/optimizing-sqlite-for-django-in-production/"},
	}, "top_referrers": nil}
	labelFirst(m)
	got, _ := json.Marshal(m["top_pages"])
	if string(got) != `["/: 15","/posts/optimizing-sqlite-for-django-in-production/: 8"]` {
		t.Errorf("top_pages = %s", got)
	}
	if m["page_views"] != 49.0 {
		t.Errorf("page_views = %v", m["page_views"])
	}
}
