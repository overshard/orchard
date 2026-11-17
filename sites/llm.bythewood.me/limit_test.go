package main

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const okReply = `{"id":"m","type":"message","role":"assistant","model":"x","stop_reason":"end_turn",
	"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`

func TestAHugeMaxTokensIsClamped(t *testing.T) {
	s, secret, sent := fakeClaude(t, "application/json", okReply, okReply)
	call(t, s, secret, `{"max_tokens":10000000,"messages":[{"role":"user","content":"x"}]}`)
	call(t, s, secret, `{"max_completion_tokens":10000000,"messages":[{"role":"user","content":"x"}]}`)
	for i, got := range *sent {
		if got["max_tokens"].(float64) != maxTokensCeiling {
			t.Errorf("call %d sent max_tokens %v", i, got["max_tokens"])
		}
	}
}

func TestACallOverTheBudgetIsRefused(t *testing.T) {
	s, secret, sent := fakeClaude(t, "application/json")
	s.budget = 100
	s.store.LogSpend(1, 100_000_000)
	rec := call(t, s, secret, `{"messages":[{"role":"user","content":"x"}]}`)
	if rec.Code != 429 || !strings.Contains(rec.Body.String(), "insufficient_quota") {
		t.Errorf("status %d body %s", rec.Code, rec.Body)
	}

	srec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/search", strings.NewReader(`{"query":"q"}`))
	req.Header.Set("Authorization", "Bearer "+secret)
	s.requireKey(s.search)(srec, req)
	if srec.Code != 429 {
		t.Errorf("a search over budget got %d", srec.Code)
	}
	if len(*sent) != 0 {
		t.Errorf("%d calls reached the api", len(*sent))
	}
}

func TestACallUnderTheBudgetGoesThrough(t *testing.T) {
	s, secret, _ := fakeClaude(t, "application/json", okReply)
	s.budget = 100
	s.store.LogSpend(1, 99_000_000)
	if rec := call(t, s, secret, `{"messages":[{"role":"user","content":"x"}]}`); rec.Code != 200 {
		t.Errorf("status %d body %s", rec.Code, rec.Body)
	}
}

func TestAKeyInALoopIsSlowedDown(t *testing.T) {
	replies := make([]string, keyBurst)
	for i := range replies {
		replies[i] = okReply
	}
	s, secret, sent := fakeClaude(t, "application/json", replies...)
	for i := range keyBurst {
		if rec := call(t, s, secret, `{"messages":[{"role":"user","content":"x"}]}`); rec.Code != 200 {
			t.Fatalf("call %d inside the burst got %d", i+1, rec.Code)
		}
	}
	rec := call(t, s, secret, `{"messages":[{"role":"user","content":"x"}]}`)
	if rec.Code != 429 || rec.Header().Get("Retry-After") == "" {
		t.Errorf("a call past the burst got %d", rec.Code)
	}
	if len(*sent) != keyBurst {
		t.Errorf("%d calls reached the api", len(*sent))
	}
}

func TestTheBucketRefillsPerKey(t *testing.T) {
	var l keyLimiter
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	for range keyBurst {
		l.allow(1, now)
	}
	if l.allow(1, now) {
		t.Error("an empty bucket allowed a call")
	}
	if !l.allow(2, now) {
		t.Error("one key's loop held up another key")
	}
	if !l.allow(1, now.Add(time.Second)) || l.allow(1, now.Add(time.Second)) {
		t.Error("a second refilled other than one call")
	}
}
