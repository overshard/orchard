package main

import (
	"crypto/sha256"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gate.bythewood.me/web"
)

// Verdicts as counted. exempt is a request gate let through without asking the
// rules, because it came from orchard itself or someone signed in.
const (
	vPass      = "pass"
	vExempt    = "exempt"
	vBlock     = "block"
	vLimit     = "limit"
	vChallenge = "challenge"
	vBanned    = "banned"
	vSolved    = "solved"
)

// Modes. watch runs every rule as if it were watching, and off skips the
// rules entirely, both for the day something goes wrong.
const (
	modeOn    = "on"
	modeWatch = "watch"
	modeOff   = "off"
)

type engine struct {
	rules  atomic.Pointer[[]*Rule]
	mode   atomic.Value
	until  atomic.Int64 // unix seconds a watch or off mode runs out, 0 for never
	self   atomic.Pointer[[]netip.Prefix]
	geo    *Geo
	bots   *Crawlers
	auth   *web.Authenticator
	secret []byte
	stats  *stats
	spent  spent

	refusedSessions noCache

	mu      sync.Mutex
	windows map[windowKey]*window
	bans    map[netip.Addr]Ban // keyed by banKey
}

type windowKey struct {
	rule int64
	ip   netip.Addr
}

type window struct {
	start time.Time
	n     int
}

// banKey is what bans and limits count against, the address on IPv4 and its
// /64 on IPv6, since one IPv6 client can rotate through a whole /64.
func banKey(ip netip.Addr) netip.Addr {
	ip = ip.Unmap()
	if ip.Is6() {
		p, _ := ip.Prefix(64)
		return p.Addr()
	}
	return ip
}

// Covers is the range an IPv6 ban refuses, empty for IPv4.
func (b Ban) Covers() string {
	if k := banKey(b.IP); k.Is6() {
		return netip.PrefixFrom(k, 64).String()
	}
	return ""
}

// Ban is an address refused on every host until it runs out, set by a rule
// with ban after or by hand.
type Ban struct {
	IP      netip.Addr
	Until   time.Time
	Rule    string
	Created time.Time
}

func newEngine(geo *Geo, auth *web.Authenticator, secret []byte, st *stats) *engine {
	e := &engine{geo: geo, bots: NewCrawlers(), auth: auth, secret: secret, stats: st,
		windows: map[windowKey]*window{}, bans: map[netip.Addr]Ban{}}
	e.mode.Store(modeOn)
	empty := []*Rule{}
	e.rules.Store(&empty)
	st.rules = func() []*Rule { return *e.rules.Load() }
	none := []netip.Prefix{}
	e.self.Store(&none)
	return e
}

// load swaps in a new rule set, which every request after it sees. Allow
// comes first so nothing below it can refuse what it lets through.
func (e *engine) load(rules []Rule) {
	out := make([]*Rule, 0, len(rules))
	for i := range rules {
		r := rules[i]
		if r.validate() != nil {
			continue
		}
		out = append(out, &r)
	}
	slices.SortStableFunc(out, func(a, b *Rule) int { return actionRank(a.Action) - actionRank(b.Action) })
	e.rules.Store(&out)
}

func (e *engine) setSelf(addrs []netip.Prefix) { e.self.Store(&addrs) }

// Mode reads the clock itself, so a watch or off that ran out is enforcing
// from that second whatever the database says until the next reload.
func (e *engine) Mode() string {
	m := e.mode.Load().(string)
	if m == modeOn {
		return m
	}
	if u := e.until.Load(); u != 0 && time.Now().Unix() >= u {
		return modeOn
	}
	return m
}

// orchard is the bridge, loopback and the box's own public address, which is
// where status' probes and the Claude on this box come from.
func (e *engine) orchard(ip netip.Addr) bool {
	ip = ip.Unmap()
	if ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() {
		return true
	}
	for _, p := range *e.self.Load() {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

type verdict struct {
	kind    string
	rule    *Rule
	quiet   bool
	orchard bool   // never checked, and left out of the hour ring
	ban     string // the rule that made the ban, for a banned request
}

// check is what Caddy asks before every request. A 2xx lets the request on to
// the site and anything else is sent back to the visitor as it is, so it never
// touches the database and keeps its counting in memory.
func (e *engine) check(w http.ResponseWriter, r *http.Request) {
	q := e.request(r)
	v := e.decide(q, r)
	e.stats.count(q, v)

	switch v.kind {
	case vPass, vExempt, vSolved:
		// dns copies these onto a lookup, to notice a device's token turning
		// up on a network it hasn't been on.
		if q.ASN != 0 {
			w.Header().Set("Gate-Asn", strconv.Itoa(q.ASN))
			w.Header().Set("Gate-Org", asciiOnly(q.Org))
			w.Header().Set("Gate-Country", q.Country)
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if v.quiet {
		w.Header().Set("Gate-Quiet", "1")
	}
	switch v.kind {
	case vChallenge:
		e.challenge(w, r, q, v.rule)
	case vLimit:
		w.Header().Set("Retry-After", "60")
		plain(w, http.StatusTooManyRequests, "Slow down, and try again in a minute.")
	case vBanned:
		plain(w, http.StatusForbidden, "This address is refused for a while.")
	default:
		msg := v.rule.Message
		if msg == "" && v.rule.Status == http.StatusNotFound {
			msg = "404 page not found"
		} else if msg == "" {
			msg = "Refused."
		}
		plain(w, v.rule.Status, msg)
	}
}

func asciiOnly(s string) string {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s) && len(b) < 60; i++ {
		if c := s[i]; c >= 0x20 && c < 0x7f {
			b = append(b, c)
		}
	}
	return string(b)
}

func plain(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(msg + "\n"))
}

func (e *engine) request(r *http.Request) *req {
	q := &req{Header: r.Header.Get, UA: r.Header.Get("User-Agent")}
	q.Session = strings.Contains(r.Header.Get("Cookie"), web.SessionCookie+"=")
	q.IP, _ = netip.ParseAddr(strings.TrimSpace(r.Header.Get("X-Gate-Ip")))
	// Caddy keeps the visitor's Host on the way to /check. X-Forwarded-Host is
	// passed through from the client behind a trusted proxy, so it isn't read.
	q.Host = strings.ToLower(r.Host)
	if h, _, ok := strings.Cut(q.Host, ":"); ok {
		q.Host = h
	}
	q.Method = r.Header.Get("X-Forwarded-Method")
	if q.Method == "" {
		q.Method = http.MethodGet
	}
	uri := r.Header.Get("X-Forwarded-Uri")
	if u, err := url.ParseRequestURI(uri); err == nil {
		q.Path, q.Query = u.Path, u.RawQuery
	} else {
		q.Path, q.Query, _ = strings.Cut(uri, "?")
	}
	if q.Path == "" {
		q.Path = "/"
	}
	if q.IP.IsValid() {
		g := e.geo.Lookup(q.IP)
		q.Country, q.ASN, q.Org = g.Country, g.ASN, g.Org
	}
	if q.IP.IsValid() {
		q.Crawler = e.bots.Classify(r.Context(), q.IP, q.UA, q.ASN)
	}
	// Cloudflare's guess fills in when DB-IP has none, never the other way.
	if q.Country == "" {
		q.Country = strings.ToUpper(r.Header.Get("Cf-Ipcountry"))
		if q.Country == "XX" || q.Country == "T1" {
			q.Country = ""
		}
	}
	return q
}

func (e *engine) decide(q *req, r *http.Request) verdict {
	if !q.IP.IsValid() || e.orchard(q.IP) || strings.HasPrefix(q.Path, "/__gate/") {
		return verdict{kind: vExempt, orchard: true}
	}
	mode := e.Mode()
	if mode == modeOff {
		return verdict{kind: vPass}
	}
	now := time.Now()

	e.mu.Lock()
	ban, banned := e.bans[banKey(q.IP)]
	e.mu.Unlock()
	if banned && now.Before(ban.Until) && mode == modeOn {
		return e.unlessSignedIn(r, verdict{kind: vBanned, ban: ban.Rule})
	}

	var out verdict
	var matched []int64
	defer func() { e.stats.matched(matched, now) }()
	solved := false
	for _, rule := range *e.rules.Load() {
		if rule.Expired(now) || !rule.match(q) {
			continue
		}
		watching := rule.Watching || mode == modeWatch
		if watching {
			e.stats.hit(q, rule, true)
			continue
		}
		// A limit only counts as matched when it would refuse, and a challenge
		// already solved doesn't count at all.
		if rule.Action == actChallenge && e.passed(r, q) {
			solved = true
			continue
		}
		if rule.Action != actLimit {
			matched = append(matched, rule.ID)
		}
		if out.kind != "" {
			continue
		}
		switch rule.Action {
		case actAllow:
			e.stats.hit(q, rule, false)
			return verdict{kind: vPass, rule: rule}
		case actBlock:
			out = verdict{kind: vBlock, rule: rule, quiet: rule.Quiet}
		case actLimit:
			if e.over(rule, q.IP, now) {
				matched = append(matched, rule.ID)
				out = verdict{kind: vLimit, rule: rule, quiet: rule.Quiet}
			}
		case actChallenge:
			out = verdict{kind: vChallenge, rule: rule}
		}
	}
	if out.kind == "" {
		if solved {
			return verdict{kind: vSolved}
		}
		return verdict{kind: vPass}
	}
	out = e.unlessSignedIn(r, out)
	if out.kind == vExempt {
		matched = nil
	} else {
		e.stats.hit(q, out.rule, false)
		if out.rule != nil && out.rule.BanAfter > 0 {
			e.maybeBan(out.rule, q.IP, now)
		}
	}
	return out
}

// unlessSignedIn lets anyone with a live orchard session through, asked only
// once something would have refused them, so a normal request costs nothing.
// A cookie auth couldn't have made, or one it said no to a minute ago, isn't
// asked about again, so a flood carrying a junk cookie can't lean on auth.
func (e *engine) unlessSignedIn(r *http.Request, v verdict) verdict {
	c, err := r.Cookie(web.SessionCookie)
	if err != nil || !sessionShaped(c.Value) {
		return v
	}
	k := sha256.Sum256([]byte(r.Method + " " + c.Value))
	now := time.Now()
	if e.refusedSessions.has(k, now) {
		return v
	}
	if e.auth.Authenticated(r) {
		return verdict{kind: vExempt}
	}
	e.refusedSessions.add(k, now)
	return v
}

// sessionShaped is the 32 random bytes in unpadded base64url auth hands out.
func sessionShaped(v string) bool {
	if len(v) != 43 {
		return false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// noCache remembers the sessions auth said no to. Only a no is kept, so a
// session signed out elsewhere never keeps working here.
type noCache struct {
	mu sync.Mutex
	m  map[[32]byte]time.Time
}

const (
	noFor = time.Minute
	maxNo = 10000
)

func (c *noCache) has(k [32]byte, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	at, ok := c.m[k]
	return ok && now.Sub(at) < noFor
}

func (c *noCache) add(k [32]byte, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[[32]byte]time.Time{}
	}
	if len(c.m) >= maxNo {
		for x, at := range c.m {
			if now.Sub(at) >= noFor {
				delete(c.m, x)
			}
		}
		if len(c.m) >= maxNo {
			clear(c.m)
		}
	}
	c.m[k] = now
}

// over counts a request against a limit rule's minute for one address.
func (e *engine) over(rule *Rule, ip netip.Addr, now time.Time) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	k := windowKey{rule.ID, banKey(ip)}
	win := e.windows[k]
	if win == nil || now.Sub(win.start) >= time.Minute {
		win = &window{start: now}
		e.windows[k] = win
	}
	win.n++
	return win.n > rule.Rate
}

// maybeBan counts a rule's hits from one address and bans it everywhere once
// it passes the rule's ban after inside the window.
func (e *engine) maybeBan(rule *Rule, ip netip.Addr, now time.Time) {
	e.mu.Lock()
	k := windowKey{-rule.ID, banKey(ip)}
	win := e.windows[k]
	if win == nil || now.Sub(win.start) >= banWindow {
		win = &window{start: now}
		e.windows[k] = win
	}
	win.n++
	var b *Ban
	if win.n >= rule.BanAfter {
		if old, ok := e.bans[banKey(ip)]; !ok || old.Until.Before(now) {
			nb := Ban{IP: ip, Until: now.Add(rule.BanFor), Rule: rule.Name, Created: now}
			e.bans[banKey(ip)] = nb
			b = &nb
		}
		delete(e.windows, k)
	}
	e.mu.Unlock()
	if b != nil {
		e.stats.ban(*b)
	}
}

func (e *engine) setBans(bans []Ban) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.bans = map[netip.Addr]Ban{}
	for _, b := range bans {
		e.bans[banKey(b.IP)] = b
	}
}

func (e *engine) addBan(b Ban) {
	e.mu.Lock()
	e.bans[banKey(b.IP)] = b
	e.mu.Unlock()
}

func (e *engine) lift(ip netip.Addr) {
	e.mu.Lock()
	delete(e.bans, banKey(ip))
	e.mu.Unlock()
}

// sweep drops windows and bans that have run out, so the maps can't grow
// without bound under a crawl from a thousand addresses.
func (e *engine) sweep(now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for k, w := range e.windows {
		if now.Sub(w.start) > banWindow {
			delete(e.windows, k)
		}
	}
	for ip, b := range e.bans {
		if now.After(b.Until) {
			delete(e.bans, ip)
		}
	}
}
