package main

import (
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
)

func TestCostFollowsTheRateCards(t *testing.T) {
	short := anthropic.Usage{InputTokens: 100_000, OutputTokens: 100_000}
	if got := costMicros(short); got != 60_000 {
		t.Errorf("a short prompt cost %d, want 60000", got)
	}
	cached := anthropic.Usage{InputTokens: 10_000, CacheReadInputTokens: 80_000, CacheCreationInputTokens: 8_000}
	if got := costMicros(cached); got != 1000+800+1000 {
		t.Errorf("cached cost %d, want 2800", got)
	}
	long := anthropic.Usage{InputTokens: 200_000, OutputTokens: 1000}
	if got := costMicros(long); got != 100_000+2500 {
		t.Errorf("a long prompt cost %d, want 102500", got)
	}
	search := anthropic.Usage{}
	search.ServerToolUse.WebSearchRequests = 2
	if got := costMicros(search); got != 20_000 {
		t.Errorf("two searches cost %d, want 20000", got)
	}
}

func TestSpendAlertsGoOutOnceEach(t *testing.T) {
	s := &site{store: testStore(t), budget: 100}
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, budgetZone)
	if got := s.spendAlerts(now); len(got) != 1 || got[0].Title != "llm spend is on track" {
		t.Fatalf("a quiet Monday sent %+v", got)
	}

	s.store.LogSpend(1, 85_000_000)
	got := s.spendAlerts(now)
	if len(got) != 2 || !strings.Contains(got[0].Title, "85%") || got[1].Title != "llm is running hot" {
		t.Fatalf("85 dollars by the 16th sent %+v", got)
	}
	if again := s.spendAlerts(now.Add(time.Hour)); len(again) != 0 {
		t.Errorf("the same alerts went out twice: %+v", again)
	}
	s.store.LogSpend(1, 20_000_000)
	if over := s.spendAlerts(now.Add(2 * time.Hour)); len(over) != 1 || over[0].Priority != "urgent" {
		t.Errorf("going over sent %+v", over)
	}
}

func TestAHotFirstMorningIsNotAPaceAlarm(t *testing.T) {
	s := &site{store: testStore(t), budget: 100}
	s.store.LogSpend(1, 5_000_000)
	if got := s.spendAlerts(time.Date(2026, 12, 1, 8, 0, 0, 0, budgetZone)); len(got) != 0 {
		t.Errorf("sent %+v", got)
	}
}

func TestAnIncognitoCallStillCounts(t *testing.T) {
	s, secret, _ := fakeClaude(t, "application/json", `{"id":"m","type":"message","role":"assistant","model":"x",
		"stop_reason":"end_turn","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":100000,"output_tokens":0}}`)
	rec := httptestCall(t, s, secret, `{"messages":[{"role":"user","content":"x"}]}`, true)
	if rec != 200 {
		t.Fatalf("status = %d", rec)
	}
	if n, _ := s.store.SpentSince(time.Now().Add(-time.Hour)); n != 10_000 {
		t.Errorf("spent %d, want 10000", n)
	}
	if calls, _ := s.store.Calls("", 5); len(calls) != 0 {
		t.Errorf("an incognito call was logged")
	}
}
