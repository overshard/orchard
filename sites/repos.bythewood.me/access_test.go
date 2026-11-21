package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"repos.bythewood.me/suite"
	"repos.bythewood.me/web"
)

func accessFixture(t *testing.T, signedIn bool) (*site, http.Handler, string) {
	t.Helper()
	s := repoFixture(t)
	db, err := OpenDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s.db = db
	templates, err := fs.Sub(templateFS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	if s.renderer, err = web.NewRenderer(suite.With(templates), suite.FuncsWith(templateFuncs), layoutTemplates, pageTemplates); err != nil {
		t.Fatal(err)
	}
	if signedIn {
		verify := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"ok":true}`))
		}))
		t.Cleanup(verify.Close)
		s.auth = web.NewAuthenticatorAt(verify.URL)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{name}", s.repo)
	mux.HandleFunc("GET /{name}/tree/{rev}", s.tree)
	mux.HandleFunc("GET /{name}/tree/{rev}/{path...}", s.tree)
	mux.HandleFunc("GET /{name}/blob/{rev}/{path...}", s.blob)
	mux.HandleFunc("GET /{name}/raw/{rev}/{path...}", s.raw)
	mux.HandleFunc("GET /{name}/log/{rev}", s.log)
	mux.HandleFunc("GET /{name}/commit/{sha}", s.commit)
	mux.HandleFunc("GET /{name}/branches", s.refsPage("branches.html"))
	mux.HandleFunc("GET /{name}/archive/{rev}", s.archive)
	mux.HandleFunc("GET /{name}/atom.xml", s.atom)
	mux.HandleFunc("GET /{name}/files/{rev}", s.files)
	mux.HandleFunc("GET /{name}/last/{rev}", s.lastCommits)
	mux.HandleFunc("GET /{name}/last/{rev}/{path...}", s.lastCommits)
	mux.HandleFunc("GET /{$}", s.index)

	repo, _ := s.store.Open("orchard")
	sha, err := s.store.Resolve(context.Background(), repo, "main")
	if err != nil {
		t.Fatal(err)
	}
	return s, mux, sha
}

func fetch(h http.Handler, target string, signedIn bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, target, nil)
	if signedIn {
		r.AddCookie(&http.Cookie{Name: web.SessionCookie, Value: "a-live-session"})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// Signed out, nothing pinned to a commit and no history is served, and no page
// links to any, so a crawler has nothing new to add to its list.
func TestSignedOutSeesBranchesAndTagsOnly(t *testing.T) {
	_, h, sha := accessFixture(t, false)

	for _, target := range []string{
		"/orchard",
		"/orchard/tree/main",
		"/orchard/blob/main/README.md",
		"/orchard/raw/main/README.md",
		"/orchard/branches",
		"/orchard/archive/main.tar.gz",
		"/orchard/atom.xml",
		"/orchard/tree/main/sites/dash.bythewood.me",
		"/orchard/files/main",
		"/",
	} {
		w := fetch(h, target, false)
		if w.Code != http.StatusOK {
			t.Errorf("%s answered %d, want 200", target, w.Code)
		}
		body := w.Body.String()
		if strings.Contains(body, "/"+sha) || strings.Contains(body, "/commit/") {
			t.Errorf("%s leaked a commit link", target)
		}
		// The feed's entry ids carry a SHA inside a tag URI, which isn't a link.
		if strings.Contains(body, sha[:7]) && !strings.HasSuffix(target, ".xml") {
			t.Errorf("%s printed a commit SHA", target)
		}
		for _, history := range []string{"/log/", "/last/", "data-sha", "data-last"} {
			if strings.Contains(body, history) {
				t.Errorf("%s pointed at history with %q", target, history)
			}
		}
	}

	for _, target := range []string{
		"/orchard/tree/" + sha,
		"/orchard/blob/" + sha + "/README.md",
		"/orchard/raw/" + sha + "/README.md",
		"/orchard/archive/" + sha + ".tar.gz",
		"/orchard/log/main",
		"/orchard/commit/" + sha,
		"/orchard/last/main",
		"/orchard/files/" + sha,
	} {
		w := fetch(h, target, false)
		if strings.Contains(target, "/last/") || strings.Contains(target, "/files/") {
			if w.Code != http.StatusNotFound {
				t.Errorf("%s answered %d, want 404", target, w.Code)
			}
			continue
		}
		if w.Code != http.StatusNotFound {
			t.Errorf("%s answered %d, want 404", target, w.Code)
		}
		if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("%s carried Cache-Control %q, want no-store", target, cc)
		}
		if !strings.Contains(w.Body.String(), "signed in only") {
			t.Errorf("%s didn't say it was signed in only", target)
		}
	}
}

func TestSignedInSeesEverything(t *testing.T) {
	_, h, sha := accessFixture(t, true)

	for _, target := range []string{
		"/orchard/tree/" + sha,
		"/orchard/blob/" + sha + "/README.md",
		"/orchard/archive/" + sha + ".tar.gz",
		"/orchard/log/main",
		"/orchard/commit/" + sha,
	} {
		if w := fetch(h, target, true); w.Code != http.StatusOK {
			t.Errorf("%s answered %d signed in, want 200", target, w.Code)
		}
	}

	body := fetch(h, "/orchard", true).Body.String()
	if !strings.Contains(body, "/orchard/commit/"+sha) {
		t.Error("the repository page lost its recent commits signed in")
	}
	for _, want := range []string{`data-last="/orchard/last/main"`, "<small>commits</small>", "/orchard/log/main"} {
		if !strings.Contains(body, want) {
			t.Errorf("the repository page signed in is missing %q", want)
		}
	}
	if body := fetch(h, "/orchard/blob/main/README.md", true).Body.String(); !strings.Contains(body, `data-sha="`+sha+`"`) {
		t.Error("a file signed in has no SHA for y to pin to")
	}

	w := fetch(h, "/orchard/last/main", true)
	var last struct {
		Entries map[string]struct{ SHA, Subject string }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &last); err != nil {
		t.Fatalf("last commits: %v: %s", err, w.Body.String())
	}
	for _, name := range []string{"README.md", "sites"} {
		if c := last.Entries[name]; c.SHA != sha || c.Subject != "first commit" {
			t.Errorf("last commit for %s is %+v", name, c)
		}
	}

	// Cloudflare caches by extension, so a public raw file at a SHA would be
	// handed to a signed out visitor from the edge.
	w = fetch(h, "/orchard/raw/"+sha+"/README.md", true)
	if cc := w.Header().Get("Cache-Control"); !strings.HasPrefix(cc, "private") {
		t.Errorf("a raw file at a SHA carried Cache-Control %q", cc)
	}
}

// A hidden repository answers a stranger exactly as a missing one would.
func TestHiddenIsSignedInOnly(t *testing.T) {
	for _, signedIn := range []bool{false, true} {
		s, h, _ := accessFixture(t, signedIn)
		if err := s.db.EnsureRepo("orchard"); err != nil {
			t.Fatal(err)
		}
		if err := s.db.SetHidden("orchard", true); err != nil {
			t.Fatal(err)
		}
		want := http.StatusNotFound
		if signedIn {
			want = http.StatusOK
		}
		for _, target := range []string{"/orchard", "/orchard/tree/main", "/orchard/raw/main/README.md", "/orchard/atom.xml", "/orchard/branches", "/orchard/files/main"} {
			w := fetch(h, target, signedIn)
			if w.Code != want {
				t.Errorf("%s answered %d signed in %v, want %d", target, w.Code, signedIn, want)
			}
			if signedIn && strings.HasPrefix(w.Header().Get("Cache-Control"), "public") {
				t.Errorf("%s of a hidden repository is shareable: %s", target, w.Header().Get("Cache-Control"))
			}
		}
		if body := fetch(h, "/", signedIn).Body.String(); strings.Contains(body, `href="/orchard"`) {
			t.Error("the index listed a hidden repository")
		}
	}
}

func TestHiddenCloneNeedsACredential(t *testing.T) {
	s := repoFixture(t)
	db, err := OpenDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	token, err := db.CreateToken("laptop")
	if err != nil {
		t.Fatal(err)
	}
	wr := &wire{store: s.store, db: db, backend: gitHTTPBackend(), signedIn: func(r *http.Request) bool {
		return r.Header.Get("Cookie") == "session"
	}}
	h := wr.Router(http.NotFoundHandler())
	clone := func(hdr ...string) int {
		r := httptest.NewRequest(http.MethodGet, "/orchard.git/info/refs?service=git-upload-pack", nil)
		for i := 0; i+1 < len(hdr); i += 2 {
			r.Header.Set(hdr[i], hdr[i+1])
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if code := clone(); code == http.StatusNotFound {
		t.Fatalf("a public repository wouldn't clone, %d", code)
	}
	if err := db.EnsureRepo("orchard"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetHidden("orchard", true); err != nil {
		t.Fatal(err)
	}
	basic := "Basic " + base64.StdEncoding.EncodeToString([]byte("git:"+token))
	wrong := "Basic " + base64.StdEncoding.EncodeToString([]byte("git:nope"))
	for _, c := range []struct {
		hdr     []string
		refused bool
	}{
		{nil, true},
		{[]string{"Authorization", wrong}, true},
		{[]string{"Authorization", basic}, false},
		{[]string{"Cookie", "session"}, false},
	} {
		if code := clone(c.hdr...); (code == http.StatusNotFound) != c.refused {
			t.Errorf("clone with %v answered %d", c.hdr, code)
		}
	}
}
