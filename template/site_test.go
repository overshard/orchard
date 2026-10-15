package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// A stand-in for what Vite writes, so the tests need no frontend build.
func testDist() fstest.MapFS {
	return fstest.MapFS{
		".vite/manifest.json": {Data: []byte(`{"src/index.js": {
			"file": "assets/index-abc123.js",
			"src": "src/index.js",
			"isEntry": true,
			"css": ["assets/index-def456.css"]
		}}`)},
		"assets/index-abc123.js":  {Data: []byte("")},
		"assets/index-def456.css": {Data: []byte("")},
	}
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestRoutes(t *testing.T) {
	s, err := newSite(testDist())
	if err != nil {
		t.Fatal(err)
	}
	h := s.handler()

	cases := []struct {
		path   string
		status int
		body   string
		cache  string
	}{
		{"/", http.StatusOK, "/static/assets/index-abc123.js", "public, max-age=300"},
		{"/healthz", http.StatusOK, "ok", "no-store"},
		{"/robots.txt", http.StatusOK, "User-agent: *", ""},
		{"/static/assets/index-abc123.js", http.StatusOK, "", "immutable"},
		{"/nothing-here", http.StatusNotFound, "No such page", "no-store"},
	}
	for _, c := range cases {
		rec := get(t, h, c.path)
		if rec.Code != c.status {
			t.Errorf("%s: status %d, want %d", c.path, rec.Code, c.status)
		}
		if !strings.Contains(rec.Body.String(), c.body) {
			t.Errorf("%s: body does not contain %q", c.path, c.body)
		}
		if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, c.cache) {
			t.Errorf("%s: Cache-Control %q, want it to contain %q", c.path, got, c.cache)
		}
	}
}

// The stylesheet comes out of the manifest, so a page without it means the
// entry name in newSite and the one in vite.config.js have drifted apart.
func TestPagesLinkTheBundle(t *testing.T) {
	s, err := newSite(testDist())
	if err != nil {
		t.Fatal(err)
	}
	body := get(t, s.handler(), "/").Body.String()
	if !strings.Contains(body, "/static/assets/index-def456.css") {
		t.Error("home page does not link the stylesheet")
	}
}
