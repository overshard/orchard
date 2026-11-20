package main

import (
	"sync"
	"time"
)

// stats holds what check counted since the last flush. check only ever adds
// to maps under one lock, and flush swaps them out for the database.
type stats struct {
	mu      sync.Mutex
	traffic map[trafficKey]int
	hits    map[hitKey]*hitCount
	orgs    map[int]string
	events  []Event
	bans    []Ban
	minute  time.Time
	inMin   int
}

type trafficKey struct {
	Hour    int64
	Host    string
	ASN     int
	Country string
	Verdict string
}

type hitKey struct {
	Rule int64
	Hour int64
}

type hitCount struct{ Hits, Watched int }

// Event is one request a rule acted on or would have. Passes aren't kept, the
// hourly traffic counts cover those.
type Event struct {
	At      time.Time
	IP      string
	ASN     int
	Org     string
	Country string
	Host    string
	Method  string
	Path    string
	UA      string
	Rule    string
	Verdict string
	Watch   bool
}

// maxEventsPerMinute bounds the writes a flood can cause. The counts stay
// exact past it, only the sample of whole requests stops.
const maxEventsPerMinute = 600

func newStats() *stats {
	return &stats{traffic: map[trafficKey]int{}, hits: map[hitKey]*hitCount{}, orgs: map[int]string{}}
}

func hourOf(t time.Time) int64 { return t.UTC().Truncate(time.Hour).Unix() }

func (s *stats) count(q *req, v verdict) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.traffic[trafficKey{hourOf(now), q.Host, q.ASN, q.Country, v.kind}]++
	if q.ASN != 0 && q.Org != "" {
		s.orgs[q.ASN] = q.Org
	}
	switch v.kind {
	case vBlock, vLimit, vChallenge, vBanned:
		name := ""
		if v.rule != nil {
			name = v.rule.Name
		}
		s.event(now, q, name, v.kind, false)
	}
}

func (s *stats) hit(q *req, r *Rule, watch bool) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	k := hitKey{r.ID, hourOf(now)}
	c := s.hits[k]
	if c == nil {
		c = &hitCount{}
		s.hits[k] = c
	}
	if watch {
		c.Watched++
		s.event(now, q, r.Name, r.Action, true)
	} else {
		c.Hits++
	}
}

func (s *stats) event(now time.Time, q *req, rule, verdict string, watch bool) {
	if m := now.Truncate(time.Minute); !m.Equal(s.minute) {
		s.minute, s.inMin = m, 0
	}
	if s.inMin >= maxEventsPerMinute {
		return
	}
	s.inMin++
	ip := ""
	if q.IP.IsValid() {
		ip = q.IP.Unmap().String()
	}
	s.events = append(s.events, Event{At: now, IP: ip, ASN: q.ASN, Org: q.Org, Country: q.Country, Host: q.Host,
		Method: q.Method, Path: cut(q.Path, 300), UA: cut(q.UA, 200), Rule: rule, Verdict: verdict, Watch: watch})
}

func (s *stats) ban(b Ban) {
	s.mu.Lock()
	s.bans = append(s.bans, b)
	s.mu.Unlock()
}

func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

type pending struct {
	traffic map[trafficKey]int
	hits    map[hitKey]*hitCount
	orgs    map[int]string
	events  []Event
	bans    []Ban
}

func (s *stats) take() pending {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := pending{s.traffic, s.hits, s.orgs, s.events, s.bans}
	s.traffic, s.hits, s.orgs, s.events, s.bans = map[trafficKey]int{}, map[hitKey]*hitCount{}, map[int]string{}, nil, nil
	return p
}
