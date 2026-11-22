package main

import (
	"encoding/json"
	"net/http"
	"net/netip"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// signIn is every request signing in to orchard makes, on auth and on the
// OAuth flow mcp runs for Claude. A rule that refuses any of these can lock
// Isaac out of every site at once.
var signIn = []struct{ host, method, path, query string }{
	{"auth.bythewood.me", "GET", "/login", ""},
	{"auth.bythewood.me", "GET", "/login", "next=https%3A%2F%2Fgate.bythewood.me%2F"},
	{"auth.bythewood.me", "POST", "/login", ""},
	{"auth.bythewood.me", "GET", "/code", ""},
	{"auth.bythewood.me", "POST", "/code", ""},
	{"auth.bythewood.me", "GET", "/recovery", ""},
	{"auth.bythewood.me", "POST", "/recovery", ""},
	{"auth.bythewood.me", "GET", "/connect", ""},
	{"auth.bythewood.me", "POST", "/connect", ""},
	{"mcp.bythewood.me", "GET", "/authorize", ""},
	{"mcp.bythewood.me", "GET", "/oauth/back", ""},
	{"mcp.bythewood.me", "POST", "/token", ""},
	{"mcp.bythewood.me", "GET", "/.well-known/oauth-authorization-server", ""},
}

// identity is who a sign in probe comes from.
type identity struct {
	IP      netip.Addr
	ASN     int
	Country string
	UA      string
}

// stand ins are a US browser on a network gate knows and one it doesn't, so
// a rule on networks or addresses other than Isaac's still trips.
var standIns = []identity{
	{netip.MustParseAddr("203.0.113.200"), 0, "US", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36"},
	{netip.MustParseAddr("198.51.100.200"), 64496, "US", "Mozilla/5.0 (iPhone; CPU iPhone OS 19_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/19.1 Mobile/15E148 Safari/604.1"},
}

// refuses is whether a rule's action can stop a person signing in. A limit
// only does when it's tighter than the few posts a sign in takes.
func refuses(r *Rule) bool {
	switch r.Action {
	case actBlock, actChallenge:
		return true
	case actLimit:
		return r.Rate < 5
	}
	return false
}

// lockout lists the sign in requests a rule would refuse, tried as the stand
// ins and as every browser seen carrying an orchard session.
func lockout(r *Rule, ids ...identity) []string {
	if !refuses(r) {
		return nil
	}
	if r.conds == nil && r.validate() != nil {
		return nil
	}
	var out []string
	for _, p := range signIn {
		for _, id := range append(slices.Clone(standIns), ids...) {
			q := &req{IP: id.IP, ASN: id.ASN, Country: id.Country, UA: id.UA, Crawler: "none", Host: p.host, Method: p.method,
				Path: p.path, Query: p.query, Header: func(string) string { return "" }}
			if r.match(q) {
				out = append(out, p.host+p.path)
				break
			}
		}
	}
	return slices.Compact(out)
}

// signedIn is the browsers in the hour that carried an orchard session.
func signedIn(hour []seen) []identity {
	var out []identity
	for i := len(hour) - 1; i >= 0 && len(out) < 8; i-- {
		x := hour[i]
		if !x.Session {
			continue
		}
		id := identity{x.IP, x.ASN, x.Country, x.UA}
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

func (s *site) lockoutFor(r *Rule) []string {
	return lockout(r, signedIn(s.engine.stats.hour(time.Now()))...)
}

// Tally is one row of a test's breakdown.
type Tally struct {
	Key string `json:"k"`
	N   int    `json:"n"`
}

func tally(m map[string]int, n int) []Tally {
	out := make([]Tally, 0, len(m))
	for k, v := range m {
		out = append(out, Tally{k, v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].N > out[j].N || out[i].N == out[j].N && out[i].Key < out[j].Key })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// TestResult is what a rule would have done over the hour ring.
type TestResult struct {
	Error     string   `json:"error,omitempty"`
	Total     int      `json:"total"`
	Minutes   int      `json:"minutes"`
	Matched   int      `json:"matched"`
	Addresses int      `json:"addresses"`
	First     string   `json:"first,omitempty"`
	ByMinute  []int    `json:"by_minute"`
	Hosts     []Tally  `json:"hosts"`
	Networks  []Tally  `json:"networks"`
	Paths     []Tally  `json:"paths"`
	UAs       []Tally  `json:"uas"`
	Already   []Tally  `json:"already"`
	AlreadyN  int      `json:"already_n"`
	Session   int      `json:"session"`
	Solved    int      `json:"solved"`
	OverRate  int      `json:"over_rate"`
	Lockout   []string `json:"lockout"`
	Headers   bool     `json:"headers"`
	RunsAt    int      `json:"runs_at"`
}

// testRule runs a rule over the hour exactly as check would match it.
func testRule(r *Rule, hour []seen, live []*Rule, now time.Time, ids ...identity) TestResult {
	res := TestResult{Total: len(hour), ByMinute: make([]int, 60), Lockout: []string{}}
	if len(hour) > 0 {
		res.Minutes = int(now.Sub(time.Unix(hour[0].At, 0)).Minutes()) + 1
	}
	if err := r.validate(); err != nil {
		res.Error = err.Error()
		return res
	}
	for _, c := range r.conds {
		res.Headers = res.Headers || c.field == "header"
	}
	res.Lockout = append(res.Lockout, lockout(r, ids...)...)
	rank := actionRank(r.Action)
	for _, o := range live {
		if o.Name != r.Name && actionRank(o.Action) <= rank {
			res.RunsAt++
		}
	}
	res.RunsAt++
	hosts, nets, paths, uas, already, ips := map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}, map[netip.Addr]bool{}
	perMin := map[windowKey]int{}
	for i := range hour {
		x := &hour[i]
		if !r.match(x.req()) {
			continue
		}
		res.Matched++
		if res.First == "" {
			res.First = time.Unix(x.At, 0).UTC().Format("15:04")
		}
		if m := 59 - int(now.Unix()-x.At)/60; m >= 0 && m < 60 {
			res.ByMinute[m]++
		}
		ips[x.IP] = true
		hosts[x.Host]++
		nets[netLabel(x.ASN)]++
		paths[cut(x.Path, 80)]++
		uas[cut(x.UA, 80)]++
		if x.Rule != "" && x.Rule != r.Name && x.Verdict != vPass && x.Verdict != vSolved {
			already[x.Rule]++
			res.AlreadyN++
		}
		if x.Session {
			res.Session++
		}
		if x.Verdict == vSolved {
			res.Solved++
		}
		if r.Action == actLimit {
			k := windowKey{x.At / 60, banKey(x.IP)}
			perMin[k]++
			if perMin[k] > r.Rate {
				res.OverRate++
			}
		}
	}
	res.Addresses = len(ips)
	res.Hosts, res.Networks, res.Paths, res.UAs, res.Already = tally(hosts, 5), tally(nets, 5), tally(paths, 5), tally(uas, 5), tally(already, 4)
	return res
}

func netLabel(asn int) string {
	if asn == 0 {
		return "unknown network"
	}
	return "AS" + strconv.Itoa(asn)
}

// testRulePage is the rule form's Test, run on every edit.
func (s *site) testRulePage(w http.ResponseWriter, r *http.Request) {
	rule := ruleFromForm(r, Rule{})
	if rule.Name == "" {
		rule.Name = "untitled"
	}
	now := time.Now()
	hour := s.engine.stats.hour(now)
	res := testRule(&rule, hour, *s.engine.rules.Load(), now, signedIn(hour)...)
	orgs := s.store.Orgs()
	for i, t := range res.Networks {
		if o := orgs[strings.TrimPrefix(t.Key, "AS")]; o != "" {
			res.Networks[i].Key = t.Key + " " + o
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(res)
}

// Shadow is a live rule whose every match in the hour was taken first by a
// rule above it, so it never decides anything.
type Shadow struct {
	By      string
	Matched int
}

type shadowCache struct {
	mu  sync.Mutex
	at  time.Time
	out map[int64]Shadow
}

// shadows works over the hour ring, at most once a minute since it runs every
// rule over every request in it.
func (s *site) shadows() map[int64]Shadow {
	s.shadow.mu.Lock()
	defer s.shadow.mu.Unlock()
	now := time.Now()
	if s.shadow.out != nil && now.Sub(s.shadow.at) < time.Minute {
		return s.shadow.out
	}
	s.shadow.out, s.shadow.at = shadowed(*s.engine.rules.Load(), s.engine.stats.hour(now), now), now
	return s.shadow.out
}

func shadowed(rules []*Rule, hour []seen, now time.Time) map[int64]Shadow {
	free := map[int64]int{}
	by := map[int64]map[string]int{}
	for i := range hour {
		q := hour[i].req()
		var taker *Rule
		for _, r := range rules {
			if r.Watching || r.Expired(now) || !r.match(q) {
				continue
			}
			if taker == nil {
				free[r.ID]++
			} else {
				if by[r.ID] == nil {
					by[r.ID] = map[string]int{}
				}
				by[r.ID][taker.Name]++
			}
			if taker == nil && (r.Action == actAllow || r.Action == actBlock) {
				taker = r
			}
		}
	}
	out := map[int64]Shadow{}
	for id, m := range by {
		if free[id] > 0 {
			continue
		}
		t := tally(m, 1)
		n := 0
		for _, v := range m {
			n += v
		}
		out[id] = Shadow{By: t[0].Key, Matched: n}
	}
	return out
}
