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
// failure has to be local rather than an unauthenticated request going out.
func TestEstateToolsRefuseWithoutASession(t *testing.T) {
	reached := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
	}))
	defer srv.Close()

	var out any
	err := estateGet(context.Background(), testDeps(srv.URL), srv.URL, &out)
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

	d := testDeps(srv.URL).WithSession("a-live-session")
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

// WithSession must copy, or one person's session lands on the shared Deps and
// the next turn borrows it.
func TestWithSessionDoesNotMutateTheShared(t *testing.T) {
	base := testDeps("")
	a := base.WithSession("one")
	b := base.WithSession("two")
	if base.Session != "" {
		t.Errorf("the shared Deps was written to: %q", base.Session)
	}
	if a.Session != "one" || b.Session != "two" {
		t.Errorf("copies crossed: %q and %q", a.Session, b.Session)
	}
}

// A redirect to the login page is a sign in prompt, and decoding it as JSON
// would report a parse error instead of the real problem.
func TestEstateToolsReadARedirectAsSignedOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://auth.bythewood.me/login", http.StatusSeeOther)
	}))
	defer srv.Close()

	d := testDeps(srv.URL).WithSession("stale")
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
	err := estateGet(context.Background(), testDeps(srv.URL).WithSession("revoked"), srv.URL, &out)
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
	_, err := Calc.Run(context.Background(), testDeps(""), map[string]any{"expression": long})
	if err == nil {
		t.Fatal("a runaway expression was evaluated")
	}
	if !strings.Contains(err.Error(), "groups") {
		t.Errorf("err = %q, want it to say what to do instead", err)
	}
	// An ordinary sum still works.
	got, err := Calc.Run(context.Background(), testDeps(""), map[string]any{"expression": "14.45+10.70+15.52"})
	if err != nil {
		t.Fatalf("an ordinary sum was refused: %v", err)
	}
	if !strings.Contains(fmt.Sprint(got), "40.67") {
		t.Errorf("got %v", got)
	}
}

// deep_search costs a minute of the one GPU slot, so the harness has to be able
// to take it off the table rather than asking the model nicely.
func TestSchemasCanHaveOneToolRemoved(t *testing.T) {
	all := Default().Schemas()
	trimmed := Without(all, DeepSearch.Name)
	if len(trimmed) != len(all)-1 {
		t.Fatalf("removed %d schemas, want 1", len(all)-len(trimmed))
	}
	for _, s := range trimmed {
		fn := s["function"].(map[string]any)
		if fn["name"] == DeepSearch.Name {
			t.Error("deep_search survived being removed")
		}
	}
	// Everything else is still offered.
	if len(trimmed) == 0 {
		t.Error("removing one tool emptied the offer")
	}
}

func TestDeepSearchNeedsASessionAndAQuestion(t *testing.T) {
	if _, err := DeepSearch.Run(context.Background(), testDeps("").WithSession("live"), map[string]any{}); err == nil {
		t.Error("an empty question was accepted")
	}
	if _, err := DeepSearch.Run(context.Background(), testDeps(""), map[string]any{"question": "is it true"}); err == nil {
		t.Error("a turn with no session was accepted")
	}
}

func TestSearchAnswerCarriesSourcesAndSupport(t *testing.T) {
	got, err := searchAnswer(`{"text":"The bridge opened in 1937.","support":0.92,"elapsed":"71s",
		"sources":[{"title":"Golden Gate","url":"https://example.org/gg"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	m := got.(map[string]any)
	if m["answer"] != "The bridge opened in 1937." {
		t.Errorf("answer = %v", m["answer"])
	}
	if m["support"] != 0.92 {
		t.Errorf("support = %v", m["support"])
	}
	srcs := m["sources"].([]map[string]string)
	if len(srcs) != 1 || srcs[0]["url"] != "https://example.org/gg" {
		t.Errorf("sources = %v", srcs)
	}
}

// A thinly supported answer must not be handed over as if it were settled.
func TestALowSupportAnswerSaysSo(t *testing.T) {
	got, _ := searchAnswer(`{"text":"Maybe.","support":0.3,"sources":[]}`)
	note := got.(map[string]any)["note"].(string)
	if !strings.Contains(note, "uncertain") {
		t.Errorf("note = %q, want it to flag the weak support", note)
	}
}

func TestAFailedSearchIsReportedNotSwallowed(t *testing.T) {
	stream := "event: status\ndata: {\"step\":\"routing\"}\n\nevent: failed\ndata: {\"error\":\"every source refused\"}\n\n"
	_, err := readSearchStream(strings.NewReader(stream))
	if err == nil || !strings.Contains(err.Error(), "every source refused") {
		t.Errorf("err = %v, want search's own reason", err)
	}
}

func TestAStreamThatEndsEarlyIsAnError(t *testing.T) {
	_, err := readSearchStream(strings.NewReader("event: status\ndata: {\"step\":\"reading\"}\n\n"))
	if err == nil || !strings.Contains(err.Error(), "without answering") {
		t.Errorf("err = %v", err)
	}
}

// orchard_code exists because the model could list Isaac's repositories and
// read nothing inside them, so it guessed raw addresses on repos and github and
// collected 404s. It has to walk down to a file the way a person does.
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
	d := testDeps(srv.URL).WithSession("a-live-session")

	got, err := OrchardCode.Run(context.Background(), d,
		map[string]any{"repo": "orchard", "path": "sites/chat.bythewood.me/tools/web.go"})
	if err != nil {
		t.Fatalf("reading a file failed: %v", err)
	}
	if m, ok := got.(map[string]any); !ok || m["text"] != "package tools" {
		t.Fatalf("the file came back as %#v", got)
	}

	// A directory is not a file, and the 404 on the file read has to fall
	// through to the listing rather than being reported as a missing path.
	got, err = OrchardCode.Run(context.Background(), d,
		map[string]any{"repo": "orchard", "path": "sites"})
	if err != nil {
		t.Fatalf("listing a directory failed: %v", err)
	}
	if m, ok := got.(map[string]any); !ok || m["count"] != float64(1) {
		t.Fatalf("the listing came back as %#v", got)
	}

	// A path that is neither says so, and says what to do about it, rather
	// than handing back an empty listing that reads as an empty directory.
	_, err = OrchardCode.Run(context.Background(), d,
		map[string]any{"repo": "orchard", "path": "sites/nope/nothing.go"})
	if err == nil {
		t.Fatal("a path that does not exist came back as a result")
	}
	if !strings.Contains(err.Error(), "list the level above") {
		t.Errorf("err = %q, want it to say what to do next", err)
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
