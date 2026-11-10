package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHandLinesCarryNoNumbers(t *testing.T) {
	all := append(append([]string{}, dreamPool...), gossipPool...)
	for _, ls := range gazettePool {
		all = append(all, ls...)
	}
	for _, l := range all {
		if strings.ContainsAny(l, "0123456789%") || len(l) > lineMax {
			t.Errorf("hand line breaks the rules: %q", l)
		}
	}
	for _, l := range gossipPool {
		if strings.Count(l, "{name}") != 1 {
			t.Errorf("gossip without exactly one {name}: %q", l)
		}
	}
}

func TestGossipIsNeverAboutItself(t *testing.T) {
	for d := 0; d < 60; d++ {
		day := time.Date(2026, 11, 1, 12, 0, 0, 0, pondZone).AddDate(0, 0, d).Format("2006-01-02")
		for i, r := range residents {
			g := gossipOf(i, day)
			if strings.Contains(g, "{") {
				t.Fatalf("unfilled slot: %q", g)
			}
			if strings.Contains(g, " "+r.name+" ") {
				t.Errorf("%s gossips about itself on %s: %q", r.name, day, g)
			}
		}
	}
}

func TestGazetteHasThreeDifferentHeadlines(t *testing.T) {
	for _, mood := range []string{moodPlain, moodRain, moodGloom2, moodSombre, moodFrost} {
		for d := 0; d < 30; d++ {
			day := time.Date(2026, 11, 1, 12, 0, 0, 0, pondZone).AddDate(0, 0, d).Format("2006-01-02")
			g := gazetteOf(mood, day)
			if len(g) != 3 || g[0] == g[1] || g[1] == g[2] || g[0] == g[2] {
				t.Fatalf("%s on %s: %q", mood, day, g)
			}
			for _, h := range g {
				if strings.Contains(h, "{") {
					t.Fatalf("unfilled slot: %q", h)
				}
			}
		}
	}
}

func TestMarketHeadline(t *testing.T) {
	quiet := marketHeadline([]firmView{{business: business{Name: "Ribbit Labs"}, Price: 100, Prev: 100}}, "2026-11-10")
	if !strings.Contains(quiet, "quiet") {
		t.Errorf("flat board: %q", quiet)
	}
	firms := []firmView{
		{business: business{Name: "Ribbit Labs"}, Price: 101, Prev: 100},
		{business: business{Name: "Bog Oil & Peat"}, Price: 90, Prev: 100},
	}
	h := marketHeadline(firms, "2026-11-10")
	if !strings.Contains(h, "Bog Oil & Peat") || strings.ContainsAny(h, "0123456789%") {
		t.Errorf("biggest mover: %q", h)
	}
}

func TestCheckLine(t *testing.T) {
	seen := map[string]bool{}
	cases := []struct {
		line, must string
		allowed    []string
		ok         bool
	}{
		{"It sat on the pad and watched the clouds go by.", "", nil, true},
		{"It sat on the pad and watched the clouds go by!", "", nil, false},
		{"It counted 12 flies before lunch.", "", nil, false},
		{"It thinks the stock board looks funny today.", "", nil, false},
		{"It's watching {show} under a blanket.", "{show}", []string{"{show}"}, true},
		{"It's watching a cartoon under a blanket.", "{show}", []string{"{show}"}, false},
		{"It's watching {show} with {name} tonight.", "{show}", []string{"{show}"}, false},
		{"It heard the dead leaves rustle all night.", "", nil, false},
		{"It ate a dieffenbachia leaf by mistake, oops.", "", nil, true},
		{"It sang — loudly.", "", nil, false},
		{"Short line.", "", nil, false},
		{strings.Repeat("It hopped about a lot ", 6) + ".", "", nil, false},
	}
	for _, c := range cases {
		if _, ok := checkLine(c.line, c.allowed, c.must, seen); ok != c.ok {
			t.Errorf("%q: got %v", c.line, ok)
		}
	}
}

func TestBookMergesOnlyRecentNights(t *testing.T) {
	defer book.cur.Store(nil)
	book.cur.Store(&tales{Day: "2026-11-10", Dream: []string{"It dreamt of soup."},
		Greeting: map[string]map[string][]string{moodRain: {"SHY": {"It hid from the rain, then joined in."}}}})
	if got := book.pool("dream", "", "2026-11-10", dreamPool); len(got) != len(dreamPool)+1 {
		t.Errorf("same day: %d lines", len(got))
	}
	if got := book.pool("dream", "", "2026-11-11", dreamPool); len(got) != len(dreamPool)+1 {
		t.Errorf("next day should keep the night before: %d lines", len(got))
	}
	if got := book.pool("dream", "", "2026-11-12", dreamPool); len(got) != len(dreamPool) {
		t.Errorf("two days on should be hand lines only: %d lines", len(got))
	}
	if got := book.natured("greeting", moodRain, "BOLD", "2026-11-10", moodLines[moodRain]); len(got) != len(moodLines[moodRain]) {
		t.Errorf("another nature took a SHY line")
	}
	if got := book.natured("greeting", moodRain, "SHY", "2026-11-10", moodLines[moodRain]); len(got) != len(moodLines[moodRain])+1 {
		t.Errorf("SHY line missing")
	}
}

// A fake gateway answers every kind with lines, one of them bad, and the
// night lands on disk with the bad ones dropped.
func TestNightlyWritesAndChecks(t *testing.T) {
	defer book.cur.Store(nil)
	var calls, unloads atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/gpu":
			json.NewEncoder(w).Encode(gpuState{})
			return
		case "/v1/unload":
			unloads.Add(1)
			return
		}
		calls.Add(1)
		var req struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		ask := req.Messages[1].Content
		line := "It had a lovely quiet time by the water today."
		switch {
		case strings.Contains(ask, "{show}"):
			line = "It watched {show} with its feet up."
		case strings.Contains(ask, "{game}"):
			line = "It played {game} until the fireflies came out."
		case strings.Contains(ask, "{name}"):
			line = "Heard {name} has been humming in the reeds."
		}
		lines := []string{line + "", "It made 100 friends today."}
		var content any = map[string]any{"lines": lines}
		if strings.Contains(ask, "nature") {
			by := map[string][]string{}
			for _, n := range taleNatures {
				by[n] = []string{strings.Replace(line, "It", "It ("+strings.ToLower(n)+")", 1), line}
			}
			content = by
		}
		raw, _ := json.Marshal(content)
		json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]string{"content": string(raw)}}}})
	}))
	defer srv.Close()

	dir := t.TempDir()
	n := &nightly{m: newModel(srv.URL, "k"), dir: dir, free: plenty}
	night := time.Date(2026, 11, 10, 23, 0, 0, 0, pondZone)
	n.tick(context.Background(), night)
	if unloads.Load() != 1 {
		t.Errorf("weights this run loaded should come off, got %d unloads", unloads.Load())
	}
	raw, err := os.ReadFile(filepath.Join(dir, "tales-2026-11-11.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got tales
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Gossip) != 1 || len(got.Watching) == 0 || len(got.Greeting) != len(moodWords) {
		t.Errorf("night: %d gossip, %d watching, %d greeting moods", len(got.Gossip), len(got.Watching), len(got.Greeting))
	}
	if book.current("2026-11-11") == nil {
		t.Error("the book wasn't updated")
	}

	before := calls.Load()
	n.tick(context.Background(), night.Add(time.Hour))
	if calls.Load() != before {
		t.Error("a second run the same night asked the model again")
	}
	n.tick(context.Background(), time.Date(2026, 11, 11, 14, 0, 0, 0, pondZone))
	if calls.Load() != before {
		t.Error("ran in the afternoon")
	}
}

func TestNightlyWaitsForABusyCard(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/gpu" {
			json.NewEncoder(w).Encode(gpuState{Busy: true, Reason: "a game is running"})
			return
		}
		calls.Add(1)
	}))
	defer srv.Close()
	n := &nightly{m: newModel(srv.URL, "k"), dir: t.TempDir(), free: plenty}
	n.tick(context.Background(), time.Date(2026, 11, 10, 23, 0, 0, 0, pondZone))
	if calls.Load() != 0 {
		t.Error("asked the model while the card was busy")
	}
}

func plenty() (int64, bool) { return 8 << 30, true }

func TestNightlyWaitsForMemory(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer srv.Close()
	n := &nightly{m: newModel(srv.URL, "k"), dir: t.TempDir(), free: func() (int64, bool) { return 1 << 30, true }}
	n.tick(context.Background(), time.Date(2026, 11, 10, 23, 0, 0, 0, pondZone))
	if calls.Load() != 0 {
		t.Error("asked the gateway with too little memory free")
	}
}
