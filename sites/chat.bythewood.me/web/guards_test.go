package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSameOriginRefusesASiblingHost(t *testing.T) {
	h := SameOrigin("/collect")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	for _, c := range []struct {
		method, path, site string
		want               int
	}{
		{"POST", "/x", "same-origin", 200},
		{"POST", "/x", "", 200},
		{"POST", "/x", "none", 200},
		{"POST", "/x", "same-site", 403},
		{"POST", "/x", "cross-site", 403},
		{"DELETE", "/x", "same-site", 403},
		{"GET", "/x", "cross-site", 200},
		{"POST", "/collect", "cross-site", 200},
	} {
		r := httptest.NewRequest(c.method, c.path, nil)
		if c.site != "" {
			r.Header.Set("Sec-Fetch-Site", c.site)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Errorf("%s %s from %q = %d, want %d", c.method, c.path, c.site, w.Code, c.want)
		}
	}
}

func TestNoDirsRefusesAListing(t *testing.T) {
	h := NoDirs(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	for path, want := range map[string]int{"/a.css": 200, "/": 404, "": 404, "/sub/": 404, "/\xff.css": 404} {
		r := httptest.NewRequest("GET", "/x", nil)
		r.URL.Path = path
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Errorf("%q = %d, want %d", path, w.Code, want)
		}
	}
}
