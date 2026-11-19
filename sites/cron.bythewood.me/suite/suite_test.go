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

func TestSparkT(t *testing.T) {
	got := string(SparkT("ok", []float64{1, 2.5, 3}, []string{"a", `b"<`, "c"}))
	if !strings.Contains(got, `data-v="1,2.5,3"`) {
		t.Errorf("values missing from %s", got)
	}
	if !strings.Contains(got, `data-l="[&#34;a&#34;,&#34;b\&#34;\u003c&#34;,&#34;c&#34;]"`) {
		t.Errorf("labels not escaped in %s", got)
	}
	if strings.Contains(string(SparkT("ok", []float64{1, 2}, []string{"a"})), "data-l") {
		t.Error("labels that don't line up with the values were kept")
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
