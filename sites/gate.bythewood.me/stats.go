package main

import (
	"net/netip"
	"strings"
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
	ring    *ring
	rules   func() []*Rule
	solves  map[solveKey]*solveCount
	people  map[peopleKey]struct{}
	pHour   int64
	banMin  time.Time
	banSeen map[netip.Addr]struct{}
}

type solveKey struct {
	Hour int64
	Host string
	Rule int64
}

// solveCount is one host's challenges in an hour. People is addresses, each
// counted once an hour however many challenge pages it loads.
type solveCount struct{ People, Solved, Failed int }

type peopleKey struct {
	rule int64
	ip   netip.Addr
}

// maxPeople bounds the set of addresses challenged this hour, so a flood from
// a botnet can't grow it without end. Past it new addresses go uncounted.
const maxPeople = 50000

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

// hitCount is a rule's hour. Hits is what it decided, and Matched is every
// request it matched while enforcing, including ones a rule before it took.
type hitCount struct{ Hits, Watched, Matched int }

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
	Why
	Also string // other rules the request matched, comma separated
}

// maxEventsPerMinute bounds the writes a flood can cause. The counts stay
// exact past it, only the sample of whole requests stops.
const maxEventsPerMinute = 600

func newStats() *stats {
	return &stats{traffic: map[trafficKey]int{}, hits: map[hitKey]*hitCount{}, orgs: map[int]string{}, ring: newRing(ringSize),
		rules: func() []*Rule { return nil }, solves: map[solveKey]*solveCount{}, people: map[peopleKey]struct{}{}, banSeen: map[netip.Addr]struct{}{}}
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
	if !v.orchard {
		s.ring.add(now, q, v)
	}
	switch v.kind {
	case vBlock, vLimit, vChallenge:
		s.event(now, q, v.rule, v.kind, false)
	case vBanned:
		s.bannedEvent(now, q, v.ban)
	}
	if v.kind == vChallenge && v.rule != nil {
		s.challenged(now, q, v.rule.ID)
	}
}

// bannedEvent samples a banned address once a minute, since a ban already
// handles it and a flood from one would crowd new blocks out of the sample.
func (s *stats) bannedEvent(now time.Time, q *req, rule string) {
	if m := now.Truncate(time.Minute); !m.Equal(s.banMin) {
		s.banMin = m
		clear(s.banSeen)
	}
	if _, ok := s.banSeen[q.IP]; ok || len(s.banSeen) >= maxPeople {
		return
	}
	s.banSeen[q.IP] = struct{}{}
	if s.event(now, q, nil, vBanned, false) {
		s.events[len(s.events)-1].Rule = rule
	}
}

func (s *stats) challenged(now time.Time, q *req, rule int64) {
	h := hourOf(now)
	if h != s.pHour {
		s.pHour = h
		clear(s.people)
	}
	k := peopleKey{rule, q.IP}
	if _, ok := s.people[k]; ok || len(s.people) >= maxPeople {
		return
	}
	s.people[k] = struct{}{}
	s.solveCount(h, q.Host, rule).People++
}

// solved counts an answer posted to /__gate/solve, good or not.
func (s *stats) solved(host string, rule int64, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.solveCount(hourOf(time.Now()), host, rule)
	if ok {
		c.Solved++
	} else {
		c.Failed++
	}
}

func (s *stats) solveCount(hour int64, host string, rule int64) *solveCount {
	k := solveKey{hour, host, rule}
	c := s.solves[k]
	if c == nil {
		c = &solveCount{}
		s.solves[k] = c
	}
	return c
}

func (s *stats) matched(ids []int64, now time.Time) {
	if len(ids) == 0 {
		return
	}
	h := hourOf(now)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		k := hitKey{id, h}
		c := s.hits[k]
		if c == nil {
			c = &hitCount{}
			s.hits[k] = c
		}
		c.Matched++
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
		s.event(now, q, r, r.Action, true)
	} else {
		c.Hits++
	}
}

// event keeps a sampled request and works out why it matched. The why and the
// other rules it matched are only worked out here, past the per minute cap.
func (s *stats) event(now time.Time, q *req, r *Rule, verdict string, watch bool) bool {
	if m := now.Truncate(time.Minute); !m.Equal(s.minute) {
		s.minute, s.inMin = m, 0
	}
	if s.inMin >= maxEventsPerMinute {
		return false
	}
	s.inMin++
	ip := ""
	if q.IP.IsValid() {
		ip = q.IP.Unmap().String()
	}
	e := Event{At: now, IP: ip, ASN: q.ASN, Org: q.Org, Country: q.Country, Host: q.Host,
		Method: q.Method, Path: cut(q.Path, 300), UA: cut(q.UA, 200), Verdict: verdict, Watch: watch}
	if r != nil {
		e.Rule, e.Why = r.Name, r.why(q)
		var also []string
		for _, o := range s.rules() {
			if o.ID != r.ID && o.match(q) {
				also = append(also, o.Name)
			}
		}
		e.Also = strings.Join(also, ",")
	}
	s.events = append(s.events, e)
	return true
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
	solves  map[solveKey]*solveCount
}

func (s *stats) take() pending {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := pending{s.traffic, s.hits, s.orgs, s.events, s.bans, s.solves}
	s.traffic, s.hits, s.orgs, s.events, s.bans = map[trafficKey]int{}, map[hitKey]*hitCount{}, map[int]string{}, nil, nil
	s.solves = map[solveKey]*solveCount{}
	return p
}

// maxHeldEvents bounds the sampled requests kept back while the database
// won't take them. The counts are kept whole.
const maxHeldEvents = 20000

// putBack returns a batch the database refused, so the next flush tries it
// again instead of losing its counts and bans.
func (s *stats) putBack(p pending) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, n := range p.traffic {
		s.traffic[k] += n
	}
	for k, c := range p.hits {
		if x := s.hits[k]; x != nil {
			x.Hits += c.Hits
			x.Watched += c.Watched
			x.Matched += c.Matched
		} else {
			s.hits[k] = c
		}
	}
	for k, o := range p.orgs {
		if _, ok := s.orgs[k]; !ok {
			s.orgs[k] = o
		}
	}
	for k, c := range p.solves {
		x := s.solveCount(k.Hour, k.Host, k.Rule)
		x.People += c.People
		x.Solved += c.Solved
		x.Failed += c.Failed
	}
	s.bans = append(p.bans, s.bans...)
	if room := maxHeldEvents - len(s.events); room > 0 {
		s.events = append(p.events[max(0, len(p.events)-room):], s.events...)
	}
}
