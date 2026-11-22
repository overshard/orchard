package main

import (
	"strings"
	"testing"
)

func TestHealthOfEmptyIsUnknown(t *testing.T) {
	if h := healthOf(nil); h.Known {
		t.Error("a site never probed should not claim to be up or down")
	}
}

func TestHealthTakesUpFromTheLatestSample(t *testing.T) {
	h := healthOf([]sample{{ms: 80, ok: true}, {ms: 90, ok: true}, {ok: false}})
	if !h.Known || h.Up {
		t.Errorf("got %+v, want known and down", h)
	}
	if h.Median != 90 {
		t.Errorf("median %d, want 90 from the two good samples", h.Median)
	}
}

func TestSparkPathStaysInsideItsBox(t *testing.T) {
	p := sparkPath([]int{70, 120, 95, 70})
	if !strings.HasPrefix(p, "M0.0 18.0") || !strings.Contains(p, "L33.3 2.0") {
		t.Errorf("unexpected path %q", p)
	}
	if flat := sparkPath([]int{5, 5}); flat != "M0.0 18.0 L100.0 18.0" {
		t.Errorf("flat line drew %q", flat)
	}
}

func TestLastPageReadsTheCommitCount(t *testing.T) {
	link := `<https://api.github.com/repositories/1/commits?per_page=1&page=2>; rel="next", <https://api.github.com/repositories/1/commits?per_page=1&page=1366>; rel="last"`
	if n, err := lastPage(link); err != nil || n != 1366 {
		t.Errorf("got %d, %v", n, err)
	}
	if _, err := lastPage(""); err == nil {
		t.Error("an empty header should be an error")
	}
}
