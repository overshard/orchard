package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"

	"repos.bythewood.me/web"
)

// A path that is not a file has to be a 404. cat-file failing inside the stream
// wrote nothing, so Go answered 200 with an empty body, cached for a year at a SHA.
func TestRawMissesAreNotFound(t *testing.T) {
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

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{name}/raw/{rev}/{path...}", s.raw)
	get := func(target string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
		return w
	}

	if w := get("/orchard/raw/main/README.md"); w.Code != http.StatusOK || w.Body.String() != "orchard\n" {
		t.Errorf("a real file answered %d with %q", w.Code, w.Body.String())
	}
	for _, target := range []string{
		"/orchard/raw/main/vue/dist/vue.esm.js",
		"/orchard/raw/main/sites/dash.bythewood.me",
		"/orchard/raw/nosuchbranch/README.md",
	} {
		w := get(target)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s answered %d, want 404", target, w.Code)
		}
		if cc := w.Header().Get("Cache-Control"); cc != "" {
			t.Errorf("%s carried Cache-Control %q", target, cc)
		}
	}
}
