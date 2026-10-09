package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// TestBriefLive writes both briefs against the real feeds and the real model
// and prints them. It needs the gateway on the bridge, so run it in a container
// on orchard-edge with BRIEF_LIVE=1 and LLM_KEY set. BRIEF_SLOT picks morning,
// midday or close.
func TestBriefLive(t *testing.T) {
	if os.Getenv("BRIEF_LIVE") == "" {
		t.Skip("set BRIEF_LIVE=1 to call the feeds and the model")
	}
	m := NewModel(env("LLM_URL", "http://orchard-llm:8000"), os.Getenv("LLM_KEY"))
	if m == nil {
		t.Fatal("no LLM_KEY")
	}
	ctx := context.Background()
	g := NewGuard(t.TempDir())
	store := NewStore(NewHub())
	store.Prime(ctx, g)
	store.refreshBoard(ctx, g)
	store.refreshEarnings(ctx, g)
	b := NewBriefer(store, g, m, t.TempDir())

	slot, at := latestSlot(time.Now())
	for i, c := range clusterStories(gather(ctx, g, newsFeeds, at.Add(-24*time.Hour), time.Now()), 25) {
		t.Logf("%2d %v %s", i+1, c.outlets(), c.lead().title)
	}
	for _, s := range briefSlots {
		if s.kind == os.Getenv("BRIEF_SLOT") {
			slot = s
		}
	}

	for _, desk := range []string{"markets", "news"} {
		if d := os.Getenv("BRIEF_DESK"); d != "" && d != desk {
			continue
		}
		started := time.Now()
		brief, err := b.compileDesk(ctx, desk, slot, at)
		if err != nil {
			t.Fatalf("%s: %v", desk, err)
		}
		out, _ := json.MarshalIndent(brief, "", "  ")
		t.Logf("%s %s in %s\n%s", desk, slot.kind, time.Since(started).Round(time.Second), out)
	}
}

func env(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}

// TestBriefTickLive is the scheduled path end to end: ask about the card, write
// both briefs on one load, and unload. Afterwards the card should be empty.
func TestBriefTickLive(t *testing.T) {
	if os.Getenv("BRIEF_LIVE") == "" {
		t.Skip("set BRIEF_LIVE=1 to call the feeds and the model")
	}
	m := NewModel(env("LLM_URL", "http://orchard-llm:8000"), os.Getenv("LLM_KEY"))
	ctx := context.Background()
	g := NewGuard(t.TempDir())
	store := NewStore(NewHub())
	store.Prime(ctx, g)
	store.refreshBoard(ctx, g)
	store.refreshEarnings(ctx, g)
	b := NewBriefer(store, g, m, t.TempDir())

	before, err := m.GPU(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("before: %+v", before)

	started := time.Now()
	b.tick(ctx, time.Now())
	t.Logf("tick took %s", time.Since(started).Round(time.Second))

	after, err := m.GPU(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("after: %+v", after)
	if !before.Busy && after.Loaded {
		t.Errorf("the model is still on the card after the tick")
	}
	br := store.Snapshot().Briefs
	if before.Busy {
		t.Logf("card was busy, waiting: %q", br.News.Waiting)
		return
	}
	for _, desk := range []Brief{br.Markets, br.News} {
		if len(desk.Points) == 0 {
			t.Errorf("a desk wrote nothing")
		}
		for _, p := range desk.Points {
			t.Logf("%-14s %3d words  %s  [%s %s]", p.Label, len(strings.Fields(p.Text)), p.Text, p.Coverage, p.Note)
		}
	}
}
