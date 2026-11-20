package main

import (
	"testing"
	"time"
)

func TestByDay(t *testing.T) {
	d := func(s string) time.Time { v, _ := time.Parse(time.RFC3339, s); return v }
	g := byDay([]Commit{
		{Subject: "c", Commit: d("2026-11-19T15:00:00Z")},
		{Subject: "b", Commit: d("2026-11-19T01:00:00Z")},
		{Subject: "a", Commit: d("2026-11-17T09:00:00Z")},
	})
	if len(g) != 2 || len(g[0].Commits) != 2 || g[1].Day != "Tue, Nov 17 2026" {
		t.Errorf("grouped as %+v", g)
	}
}

func TestCrumbsFold(t *testing.T) {
	if got := parentPath("a/b/c.go"); got != "a/b" {
		t.Errorf("parentPath gave %q", got)
	}
	if got := parentPath("c.go"); got != "" {
		t.Errorf("parentPath at the top gave %q", got)
	}
}
