package main

import (
	"testing"
	"time"
)

func TestProviderRelevance(t *testing.T) {
	cases := []struct {
		provider, text string
		want           bool
	}{
		{"OVH VINT HILL", "[US][Backbone] - Backbone Incident Notification", false},
		{"OVH VINT HILL", "[US-EAST-VA] Network degradation", true},
		{"OVH VINT HILL", "[GRA] Object storage degraded", false},
		{"CLOUDFLARE", "R2 Downloads Stalling in Japan", false},
		{"CLOUDFLARE", "Cloudflare Tunnel connectivity issues", true},
		{"ANTHROPIC", "Claude unable to join meetings", false},
		{"ANTHROPIC", "Elevated errors on Claude Haiku", true},
		{"ANTHROPIC", "Claude Code sessions failing to start", true},
		{"ANTHROPIC", "Opus 5.5 degraded performance", true},
		{"ANTHROPIC", "Claude unable to join meetings claude.ai", false},
		{"ANTHROPIC", "Elevated errors Claude API", true},
	}
	for _, c := range cases {
		for _, p := range providers {
			if p.label == c.provider {
				if got := p.relevant.MatchString(c.text); got != c.want {
					t.Errorf("%s %q = %v, want %v", c.provider, c.text, got, c.want)
				}
			}
		}
	}
}

func TestHistoryDays(t *testing.T) {
	now := time.Date(2026, 11, 18, 15, 0, 0, 0, time.UTC)
	ovh := providers[0]
	resolved := time.Date(2026, 11, 16, 20, 0, 0, 0, time.UTC)
	h := historyDaysFor(ovh, []statuspageIncident{
		{Name: "[US-EAST-VA] Network degradation", CreatedAt: time.Date(2026, 11, 15, 14, 0, 0, 0, time.UTC), ResolvedAt: &resolved},
		{Name: "[GRA] Object storage", CreatedAt: time.Date(2026, 11, 18, 13, 0, 0, 0, time.UTC)},
		{Name: "[US-EAST-VA] Old", CreatedAt: time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC), ResolvedAt: &resolved},
	}, now)
	days := h.days
	if len(days) != historyDays {
		t.Fatalf("%d days", len(days))
	}
	want := []int{2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 0, 1}
	for i := range want {
		if days[i] != want[i] {
			t.Errorf("days = %v, want %v", days, want)
			break
		}
	}
	if got := h.notes[12]; got != "TUE NOV 17, NO INCIDENTS" {
		t.Errorf("quiet note = %q", got)
	}
	if got := h.notes[13]; got != "WED NOV 18\n[GRA] Object storage" {
		t.Errorf("today's note = %q", got)
	}
	if got := h.notes[11]; got != "MON NOV 16\n[US-EAST-VA] Network degradation\n[US-EAST-VA] Old" {
		t.Errorf("hit note = %q", got)
	}
	if hits, last := hitCount(days); hits != 12 || last != 2 {
		t.Errorf("hitCount = %d, %d", hits, last)
	}
	if hits, last := hitCount(make([]int, historyDays)); hits != 0 || last != -1 {
		t.Errorf("quiet hitCount = %d, %d", hits, last)
	}
}
