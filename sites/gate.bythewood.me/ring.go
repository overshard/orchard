package main

import (
	"net/netip"
	"time"
)

// ringSize is roughly an hour of checks at a busy rate. The ring lives only in
// memory, so a deploy starts it empty and nothing in it reaches the disk.
const ringSize = 20000

// seen is one checked request as the hour ring keeps it. Headers aren't kept,
// so a header line can't be tested against it.
type seen struct {
	At      int64
	IP      netip.Addr
	ASN     int
	Country string
	Host    string
	Method  string
	Path    string
	Query   string
	UA      string
	Crawler string
	Verdict string
	Rule    string
	Session bool
}

type ring struct {
	buf  []seen
	next int
	full bool
}

func newRing(n int) *ring { return &ring{buf: make([]seen, n)} }

// add overwrites the oldest slot in place. Every string a stranger sends is
// cut, since the ring is as big as its longest entries times its length.
func (g *ring) add(now time.Time, q *req, v verdict) {
	x := &g.buf[g.next]
	x.At, x.IP, x.ASN, x.Country, x.Host, x.Method = now.Unix(), q.IP, q.ASN, q.Country, cut(q.Host, 100), cut(q.Method, 16)
	x.Path, x.Query, x.UA, x.Crawler, x.Verdict, x.Session = cut(q.Path, 300), cut(q.Query, 300), cut(q.UA, 200), q.Crawler, v.kind, q.Session
	x.Rule = ""
	if v.rule != nil {
		x.Rule = v.rule.Name
	}
	g.next++
	if g.next == len(g.buf) {
		g.next, g.full = 0, true
	}
}

// since copies out what's newer than t, oldest first.
func (g *ring) since(t int64) []seen {
	var out []seen
	if g.full {
		out = make([]seen, 0, len(g.buf))
		out = append(out, g.buf[g.next:]...)
	} else {
		out = make([]seen, 0, g.next)
	}
	out = append(out, g.buf[:g.next]...)
	i := 0
	for i < len(out) && out[i].At < t {
		i++
	}
	return out[i:]
}

// hour is the ring's last hour, copied under the stats lock so a test that
// runs a slow regex over it never holds up a check.
func (s *stats) hour(now time.Time) []seen {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ring.since(now.Add(-time.Hour).Unix())
}

func (x *seen) req() *req {
	return &req{IP: x.IP, ASN: x.ASN, Country: x.Country, Host: x.Host, Method: x.Method, Path: x.Path, Query: x.Query,
		UA: x.UA, Crawler: x.Crawler, Session: x.Session}
}
