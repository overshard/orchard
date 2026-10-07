package main

import (
	"context"
	"encoding/json"
	"os"
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

	slot, at := latestSlot(time.Now(), false)
	for i, c := range clusterStories(gather(ctx, g, newsFeeds, at.Add(-24*time.Hour), time.Now()), 25) {
		t.Logf("%2d %v %s", i+1, c.outlets(), c.lead().title)
	}
	for _, s := range briefSlots {
		if s.kind == os.Getenv("BRIEF_SLOT") {
			slot = s
		}
	}

	for _, desk := range []string{"markets", "news"} {
		started := time.Now()
		var brief Brief
		var err error
		if desk == "markets" {
			brief, err = b.compileMarkets(ctx, slot, at)
		} else {
			brief, err = b.compileNews(ctx, slot, at)
		}
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
