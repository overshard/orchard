package main

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"repos.bythewood.me/web"
)

func accessFixture(t *testing.T, signedIn bool) (*site, http.Handler, string) {
	t.Helper()
	s := searchFixture(t)
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
	if s.renderer, err = web.NewRenderer(templates, templateFuncs, layoutTemplates, pageTemplates); err != nil {
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
	} {
		w := fetch(h, target, false)
		if w.Code != http.StatusOK {
			t.Errorf("%s answered %d, want 200", target, w.Code)
		}
		if body := w.Body.String(); strings.Contains(body, "/"+sha) || strings.Contains(body, "/commit/") {
			t.Errorf("%s leaked a commit link", target)
		}
	}

	for _, target := range []string{
		"/orchard/tree/" + sha,
		"/orchard/blob/" + sha + "/README.md",
		"/orchard/raw/" + sha + "/README.md",
		"/orchard/archive/" + sha + ".tar.gz",
		"/orchard/log/main",
		"/orchard/commit/" + sha,
	} {
		w := fetch(h, target, false)
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

	if w := fetch(h, "/orchard", true); !strings.Contains(w.Body.String(), "/orchard/commit/"+sha) {
		t.Error("the repository page lost its recent commits signed in")
	}

	// Cloudflare caches by extension, so a public raw file at a SHA would be
	// handed to a signed out visitor from the edge.
	w := fetch(h, "/orchard/raw/"+sha+"/README.md", true)
	if cc := w.Header().Get("Cache-Control"); !strings.HasPrefix(cc, "private") {
		t.Errorf("a raw file at a SHA carried Cache-Control %q", cc)
	}
}
