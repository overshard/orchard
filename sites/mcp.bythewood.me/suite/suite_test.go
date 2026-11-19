package suite

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSpark(t *testing.T) {
	if Spark("ok", []float64{1}) != "" {
		t.Error("a single point drew a line")
	}
	got := string(Spark("ok", []float64{1, 1, 1}))
	if !strings.Contains(got, `data-k="ok"`) || strings.Contains(got, "NaN") {
		t.Errorf("flat series drew %s", got)
	}
	if strings.Contains(string(Spark(`"><x`, []float64{1, 2})), "<x") {
		t.Error("kind is not escaped")
	}
}

func TestHandler(t *testing.T) {
	for path, want := range map[string]int{URL("suite.css"): 200, "/suite/geist.woff2": 200, "/suite/nope.css": 404, "/suite/": 404} {
		w := httptest.NewRecorder()
		Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != want {
			t.Errorf("%s answered %d", path, w.Code)
		}
		if want == 404 && w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s 404 can be cached", path)
		}
	}
}
