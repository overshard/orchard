package main

import (
	"crypto/tls"
	"errors"
	"net"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"
)

// neverBlock is the operator's own domains, so no list or rule can lock me
// out of the page I'd use to undo it.
var neverBlock = []string{"bythewood.me", "isaacbythewood.com"}

// canary is the name Firefox asks to decide whether to turn on its own DoH,
// and answering it with NXDOMAIN keeps Firefox on this resolver.
const canary = "use-application-dns.net"

// ddrName is where RFC 9462 has a client ask its resolver what encrypted
// endpoints it has. The answer names this device's own DoT name and DoH path.
const ddrName = "_dns.resolver.arpa"

// fallbackAddr answers when unbound can't, so a phone on strict Private DNS
// keeps working through an unbound restart.
const (
	fallbackAddr = "9.9.9.9:853"
	fallbackName = "dns.quad9.net"
)

// blockTTL is short so an allow takes hold within a minute on a device that
// cached the block.
const blockTTL = 60

// rules is the hand written allows, blocks and rewrites, swapped whole on
// every change. Each maps a domain to the rules on it, one per device at most,
// with device 0 standing for every device.
type rules struct {
	allow   map[string][]Rule
	block   map[string][]Rule
	rewrite map[string][]Rule
}

// match walks name and each parent of it and gives the first rule there for
// device d, where 0 is the rules for every device.
func (r *rules) match(name string, m map[string][]Rule, d int64) (Rule, bool) {
	for n := name; ; {
		for _, x := range m[n] {
			if x.Device == d {
				return x, true
			}
		}
		i := strings.IndexByte(n, '.')
		if i < 0 {
			return Rule{}, false
		}
		n = n[i+1:]
	}
}

// safeSearch points the big search engines and YouTube at the names they
// keep for filtered results, for a device with Safe on.
var safeSearch = map[string]string{
	"google.com":               "forcesafesearch.google.com",
	"www.google.com":           "forcesafesearch.google.com",
	"bing.com":                 "strict.bing.com",
	"www.bing.com":             "strict.bing.com",
	"duckduckgo.com":           "safe.duckduckgo.com",
	"www.duckduckgo.com":       "safe.duckduckgo.com",
	"www.youtube.com":          "restrictmoderate.youtube.com",
	"m.youtube.com":            "restrictmoderate.youtube.com",
	"youtubei.googleapis.com":  "restrictmoderate.youtube.com",
	"youtube.googleapis.com":   "restrictmoderate.youtube.com",
	"www.youtube-nocookie.com": "restrictmoderate.youtube.com",
}

// resolver answers for every device. Lists and rules are read through atomic
// pointers, so a lookup never takes a lock.
type resolver struct {
	lists    *lister
	rules    atomic.Pointer[rules]
	devices  atomic.Pointer[map[string]*Device]
	upstream string
	udp      *dns.Client
	tcp      *dns.Client
	fall     *dns.Client
	fallPool connPool
	fellBack atomic.Int64
	counts   counts
	// upOK is the last time unbound answered the health probe.
	upOK   atomic.Int64
	log    *logger
	probes *probes
}

func newResolver(l *lister, upstream string, log *logger) *resolver {
	r := &resolver{
		lists: l, upstream: upstream, log: log, probes: newProbes(),
		udp:  &dns.Client{Net: "udp", Timeout: 3 * time.Second, UDPSize: dns.DefaultMsgSize},
		tcp:  &dns.Client{Net: "tcp", Timeout: 4 * time.Second},
		fall: &dns.Client{Net: "tcp-tls", Timeout: 3 * time.Second, TLSConfig: &tls.Config{ServerName: fallbackName, MinVersion: tls.VersionTLS12}},
	}
	r.rules.Store(&rules{})
	empty := map[string]*Device{}
	r.devices.Store(&empty)
	return r
}

func (r *resolver) setRules(rs []Rule) {
	n := &rules{allow: map[string][]Rule{}, block: map[string][]Rule{}, rewrite: map[string][]Rule{}}
	for _, x := range rs {
		switch x.Action {
		case "allow":
			n.allow[x.Domain] = append(n.allow[x.Domain], x)
		case "block":
			n.block[x.Domain] = append(n.block[x.Domain], x)
		case "rewrite":
			n.rewrite[x.Domain] = append(n.rewrite[x.Domain], x)
		}
	}
	r.rules.Store(n)
}

func (r *resolver) setDevices(ds []*Device) {
	m := make(map[string]*Device, len(ds))
	for _, d := range ds {
		m[d.Token] = d
	}
	r.devices.Store(&m)
}

func (r *resolver) device(token string) *Device {
	return (*r.devices.Load())[token]
}

// Verdict is what the lists and rules say about one name. Rule is the id of
// my rule that decided it, if one did.
type Verdict struct {
	Status string
	Reason string
	Domain string
	Target string
	Rule   int64
}

// decide says what a device gets for a name before anything is asked
// upstream. A paused device gets what it would have been blocked from, logged
// as allowed so the log still says what it would have blocked.
func (r *resolver) decide(d *Device, name string) Verdict {
	v := r.verdict(d, name)
	if (v.Status == "block" || v.Status == "deny") && d.PausedAt(time.Now()) {
		why := v.Reason
		if v.Status == "deny" {
			why = "rule " + v.Domain
		}
		return Verdict{Status: "allow", Reason: why + " while paused", Domain: v.Domain, Rule: v.Rule}
	}
	return v
}

// verdict is decide without a pause. A rewrite comes first, then allow beats
// block, and my own rules beat any list.
func (r *resolver) verdict(d *Device, name string) Verdict {
	rs := r.rules.Load()
	// A device's own rules come before the rules for every device.
	scopes := []int64{0}
	if d.ID != 0 {
		scopes = []int64{d.ID, 0}
	}
	for _, sc := range scopes {
		if x, ok := rs.match(name, rs.rewrite, sc); ok {
			return Verdict{Status: "rewrite", Reason: "rule " + x.Domain, Domain: x.Domain, Target: x.Target, Rule: x.ID}
		}
	}
	if t, ok := safeSearch[name]; ok && d.Safe {
		return Verdict{Status: "rewrite", Reason: "safe search", Domain: name, Target: t}
	}
	for _, own := range neverBlock {
		if name == own || strings.HasSuffix(name, "."+own) {
			return Verdict{Status: "pass"}
		}
	}
	listed, mask := r.lists.tab.Load().match(name)
	key := ""
	for i, li := range lists {
		if mask&(1<<i) != 0 && !d.Off(li.Key) {
			key = li.Key
			break
		}
	}
	for _, sc := range scopes {
		if a, ok := rs.match(name, rs.allow, sc); ok {
			if key != "" {
				return Verdict{Status: "allow", Reason: key, Domain: a.Domain, Rule: a.ID}
			}
			return Verdict{Status: "pass", Rule: a.ID}
		}
		if b, ok := rs.match(name, rs.block, sc); ok {
			return Verdict{Status: "deny", Reason: "rule", Domain: b.Domain, Rule: b.ID}
		}
	}
	if key != "" {
		return Verdict{Status: "block", Reason: key, Domain: listed}
	}
	return Verdict{Status: "pass"}
}

// Explain is decide for the domain checker, every device's lists on.
func (r *resolver) Explain(name string) (Verdict, []string) {
	name = cleanName(name)
	v := r.decide(&Device{}, name)
	_, mask := r.lists.tab.Load().match(name)
	var in []string
	for i, li := range lists {
		if mask&(1<<i) != 0 {
			in = append(in, li.Key)
		}
	}
	return v, in
}

// Client is where a query came from. The network is gate's answer for the
// address, and empty when gate had none.
type Client struct {
	Proto, IP         string
	ASN, Org, Country string
}

// Answer resolves one query for a device and logs it.
func (r *resolver) Answer(d *Device, req *dns.Msg, proto, ip string) *dns.Msg {
	return r.AnswerFor(d, req, Client{Proto: proto, IP: ip})
}

// AnswerFor resolves one query for a device and logs it. It always returns a
// message to send back.
func (r *resolver) AnswerFor(d *Device, req *dns.Msg, c Client) *dns.Msg {
	proto, ip := c.Proto, c.IP
	start := time.Now()
	if len(req.Question) != 1 || req.Opcode != dns.OpcodeQuery {
		m := new(dns.Msg)
		m.SetRcode(req, dns.RcodeFormatError)
		return m
	}
	q := req.Question[0]
	name := strings.TrimSuffix(strings.ToLower(q.Name), ".")
	l := Lookup{At: start, Device: d.ID, Name: name, Qtype: q.Qtype, Proto: proto, IP: ip, ASN: c.ASN, Org: c.Org, Country: c.Country}
	resp := r.answer(d, req, name, &l)
	l.Rcode = resp.Rcode
	l.Ms = int(time.Since(start).Milliseconds())
	r.counts.add(l.Status, proto)
	if l.Answer == "" {
		l.Answer = summary(resp)
	}
	r.log.add(l)
	return resp
}

func (r *resolver) answer(d *Device, req *dns.Msg, name string, l *Lookup) *dns.Msg {
	if name == canary || strings.HasSuffix(name, "."+canary) {
		l.Status = "local"
		m := new(dns.Msg)
		m.SetRcode(req, dns.RcodeNameError)
		return m
	}
	if name == ddrName {
		l.Status, l.Reason = "local", "ddr"
		return designate(req, d)
	}
	if nonce, ok := strings.CutSuffix(name, "."+probeZone); ok {
		r.probes.saw(nonce, d, l.Proto)
		l.Status, l.Reason = "local", "setup check"
		m := new(dns.Msg)
		m.SetRcode(req, dns.RcodeNameError)
		return m
	}
	if v := r.decide(d, name); v.Status == "rewrite" {
		l.Status, l.Reason = "rewrite", v.Reason
		return r.rewritten(req, v.Target)
	} else if v.Status == "block" || v.Status == "deny" {
		l.Status, l.Reason = v.Status, v.Reason
		if v.Status == "deny" {
			l.Reason = "rule " + v.Domain
		}
		return blocked(req, l.Reason)
	} else if v.Status == "allow" {
		l.Status, l.Reason = "allow", v.Reason
	} else {
		l.Status = "pass"
	}
	resp, err := r.forwardOrFallback(req)
	if err != nil {
		resp, err = r.fallback(req)
		if err != nil {
			l.Status, l.Reason = "fail", shortErr(err)
			m := new(dns.Msg)
			m.SetRcode(req, dns.RcodeServerFailure)
			return m
		}
		// unbound drops private answers for public names and Quad9 doesn't,
		// so a slow rebinding server would otherwise get through here.
		dropPrivate(resp)
		r.fellBack.Add(1)
		if l.Reason == "" {
			l.Reason = "fallback"
		}
	}
	// A tracker can hide behind a first party name with a CNAME, or an HTTPS
	// record's target, so the chain gets the same check the name did.
	for _, rr := range resp.Answer {
		var target string
		switch v := rr.(type) {
		case *dns.CNAME:
			target = v.Target
		case *dns.HTTPS:
			target = v.Target
		case *dns.SVCB:
			target = v.Target
		}
		target = strings.TrimSuffix(strings.ToLower(target), ".")
		if target == "" || target == name {
			continue
		}
		if v := r.decide(d, target); v.Status == "block" || v.Status == "deny" {
			l.Status, l.Reason, l.Via = v.Status, v.Reason, target
			if v.Status == "deny" {
				l.Reason = "rule " + v.Domain
			}
			return blocked(req, l.Reason)
		}
	}
	return resp
}

// errUpstreamDown skips straight to the fallback, so a hung unbound doesn't
// cost every lookup the full UDP timeout first.
var errUpstreamDown = errors.New("unbound down")

func (r *resolver) forwardOrFallback(req *dns.Msg) (*dns.Msg, error) {
	if r.upstreamDown(time.Now()) {
		return nil, errUpstreamDown
	}
	return r.forward(req)
}

// forward asks unbound. The client's ECS is dropped, since unbound recurses on
// its own and never needs to say where the client is.
func (r *resolver) forward(req *dns.Msg) (*dns.Msg, error) {
	up := req.Copy()
	up.Id = dns.Id()
	if o := up.IsEdns0(); o != nil {
		kept := o.Option[:0]
		for _, opt := range o.Option {
			if _, ecs := opt.(*dns.EDNS0_SUBNET); !ecs {
				kept = append(kept, opt)
			}
		}
		o.Option = kept
		if o.UDPSize() < dns.DefaultMsgSize {
			o.SetUDPSize(dns.DefaultMsgSize)
		}
	} else {
		up.SetEdns0(dns.DefaultMsgSize, false)
	}
	resp, _, err := r.udp.Exchange(up, r.upstream)
	if err == nil && resp.Truncated {
		resp, _, err = r.tcp.Exchange(up, r.upstream)
	}
	if err != nil {
		return nil, err
	}
	resp.Id = req.Id
	if req.IsEdns0() == nil {
		stripOPT(resp)
	}
	return resp, nil
}

// watchUpstream asks unbound for the root's SOA every 30 seconds, so the pages
// can say when lookups are going to the fallback.
func (r *resolver) watchUpstream() {
	for {
		q := new(dns.Msg)
		q.SetQuestion(".", dns.TypeSOA)
		if m, _, err := r.udp.Exchange(q, r.upstream); err == nil && m.Rcode == dns.RcodeSuccess {
			r.upOK.Store(time.Now().Unix())
		}
		time.Sleep(30 * time.Second)
	}
}

// upstreamDown says whether unbound has missed its probes for over a minute.
func (r *resolver) upstreamDown(now time.Time) bool {
	last := r.upOK.Load()
	return last != 0 && now.Unix()-last > 60
}

// rewritten answers with my own address for a name, or a CNAME to another name
// and whatever that name answers.
func (r *resolver) rewritten(req *dns.Msg, target string) *dns.Msg {
	q := req.Question[0]
	m := new(dns.Msg)
	m.SetReply(req)
	m.RecursionAvailable = true
	hdr := dns.RR_Header{Name: q.Name, Class: dns.ClassINET, Ttl: 300}
	if ip := net.ParseIP(target); ip != nil {
		switch {
		case q.Qtype == dns.TypeA && ip.To4() != nil:
			hdr.Rrtype = dns.TypeA
			m.Answer = []dns.RR{&dns.A{Hdr: hdr, A: ip.To4()}}
		case q.Qtype == dns.TypeAAAA && ip.To4() == nil:
			hdr.Rrtype = dns.TypeAAAA
			m.Answer = []dns.RR{&dns.AAAA{Hdr: hdr, AAAA: ip}}
		}
		return m
	}
	hdr.Rrtype = dns.TypeCNAME
	m.Answer = []dns.RR{&dns.CNAME{Hdr: hdr, Target: dns.Fqdn(target)}}
	if q.Qtype == dns.TypeCNAME {
		return m
	}
	sub := new(dns.Msg)
	sub.SetQuestion(dns.Fqdn(target), q.Qtype)
	sub.RecursionDesired = true
	resp, err := r.forwardOrFallback(sub)
	if err != nil {
		if resp, err = r.fallback(sub); err != nil {
			m.Rcode = dns.RcodeServerFailure
			return m
		}
	}
	m.Answer = append(m.Answer, resp.Answer...)
	m.Rcode = resp.Rcode
	return m
}

// fallback asks Quad9 over TLS, with no ECS since forward's copy is reused.
func (r *resolver) fallback(req *dns.Msg) (*dns.Msg, error) {
	up := req.Copy()
	up.Id = dns.Id()
	if o := up.IsEdns0(); o != nil {
		kept := o.Option[:0]
		for _, opt := range o.Option {
			if _, ecs := opt.(*dns.EDNS0_SUBNET); !ecs {
				kept = append(kept, opt)
			}
		}
		o.Option = kept
	}
	resp, err := r.fallPool.exchange(r.fall, up)
	if err != nil {
		return nil, err
	}
	resp.Id = req.Id
	return resp, nil
}

// connPool keeps a few TLS connections to Quad9 open, so a run of fallback
// lookups doesn't pay a handshake each. A connection serves one query at a time.
type connPool struct {
	mu   sync.Mutex
	idle []pooledConn
}

type pooledConn struct {
	c    *dns.Conn
	used time.Time
}

// poolIdle is under the time Quad9 holds an idle connection open.
const (
	poolIdle = 10 * time.Second
	poolMax  = 4
)

func (p *connPool) get() *dns.Conn {
	p.mu.Lock()
	defer p.mu.Unlock()
	for len(p.idle) > 0 {
		pc := p.idle[len(p.idle)-1]
		p.idle = p.idle[:len(p.idle)-1]
		if time.Since(pc.used) < poolIdle {
			return pc.c
		}
		pc.c.Close()
	}
	return nil
}

func (p *connPool) put(c *dns.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.idle) >= poolMax {
		c.Close()
		return
	}
	p.idle = append(p.idle, pooledConn{c, time.Now()})
}

// exchange tries a pooled connection first and a fresh one if that fails,
// since the far end may have closed it while it sat idle.
func (p *connPool) exchange(cl *dns.Client, m *dns.Msg) (*dns.Msg, error) {
	if c := p.get(); c != nil {
		if resp, _, err := cl.ExchangeWithConn(m, c); err == nil {
			p.put(c)
			return resp, nil
		}
		c.Close()
	}
	c, err := cl.Dial(fallbackAddr)
	if err != nil {
		return nil, err
	}
	resp, _, err := cl.ExchangeWithConn(m, c)
	if err != nil {
		c.Close()
		return nil, err
	}
	p.put(c)
	return resp, nil
}

// rebindNets is the same ranges as private-address in unbound.conf.
var rebindNets = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("fd00::/8"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("::ffff:0:0/96"),
}

// dropPrivate removes A and AAAA records that point into a private range.
func dropPrivate(m *dns.Msg) {
	kept := m.Answer[:0]
	for _, rr := range m.Answer {
		var a netip.Addr
		switch v := rr.(type) {
		case *dns.A:
			a, _ = netip.AddrFromSlice(v.A.To4())
		case *dns.AAAA:
			a, _ = netip.AddrFromSlice(v.AAAA.To16())
		}
		if a.IsValid() && rebinding(a) {
			continue
		}
		kept = append(kept, rr)
	}
	m.Answer = kept
}

func rebinding(a netip.Addr) bool {
	for _, n := range rebindNets {
		if n.Contains(a) {
			return true
		}
	}
	return false
}

// designate answers the DDR question with this device's DoT name first and
// its DoH path second.
func designate(req *dns.Msg, d *Device) *dns.Msg {
	m := new(dns.Msg)
	m.SetReply(req)
	m.RecursionAvailable = true
	q := req.Question[0]
	if q.Qtype != dns.TypeSVCB || d.Token == "" {
		return m
	}
	hdr := dns.RR_Header{Name: q.Name, Rrtype: dns.TypeSVCB, Class: dns.ClassINET, Ttl: 300}
	m.Answer = []dns.RR{
		&dns.SVCB{Hdr: hdr, Priority: 1, Target: d.Token + "." + dotZone + ".", Value: []dns.SVCBKeyValue{
			&dns.SVCBAlpn{Alpn: []string{"dot"}}, &dns.SVCBPort{Port: 853}}},
		&dns.SVCB{Hdr: hdr, Priority: 2, Target: dotZone + ".", Value: []dns.SVCBKeyValue{
			&dns.SVCBAlpn{Alpn: []string{"h2"}}, &dns.SVCBDoHPath{Template: "/q/" + d.Token + "{?dns}"}}},
	}
	return m
}

// pad fills an answer out to a multiple of 468 bytes when the query asked for
// padding, RFC 8467, so the size of an answer doesn't give the name away.
func pad(req, resp *dns.Msg) {
	ro := req.IsEdns0()
	if ro == nil {
		return
	}
	asked := false
	for _, o := range ro.Option {
		if _, ok := o.(*dns.EDNS0_PADDING); ok {
			asked = true
		}
	}
	if !asked {
		return
	}
	o := resp.IsEdns0()
	if o == nil {
		resp.SetEdns0(ro.UDPSize(), false)
		o = resp.IsEdns0()
	}
	kept := o.Option[:0]
	for _, opt := range o.Option {
		if _, ok := opt.(*dns.EDNS0_PADDING); !ok {
			kept = append(kept, opt)
		}
	}
	p := &dns.EDNS0_PADDING{}
	o.Option = append(kept, p)
	if n := resp.Len() % 468; n != 0 {
		p.Padding = make([]byte, 468-n)
	}
}

func stripOPT(m *dns.Msg) {
	kept := m.Extra[:0]
	for _, rr := range m.Extra {
		if rr.Header().Rrtype != dns.TypeOPT {
			kept = append(kept, rr)
		}
	}
	m.Extra = kept
}

// blocked answers 0.0.0.0 or :: for an address and nothing with an SOA for
// anything else, so an app fails at once and caches it, and says why in an
// Extended DNS Error when the client speaks EDNS.
func blocked(req *dns.Msg, why string) *dns.Msg {
	m := new(dns.Msg)
	m.SetReply(req)
	m.RecursionAvailable = true
	q := req.Question[0]
	hdr := dns.RR_Header{Name: q.Name, Class: dns.ClassINET, Ttl: blockTTL}
	switch q.Qtype {
	case dns.TypeA:
		hdr.Rrtype = dns.TypeA
		m.Answer = []dns.RR{&dns.A{Hdr: hdr, A: net.IPv4zero}}
	case dns.TypeAAAA:
		hdr.Rrtype = dns.TypeAAAA
		m.Answer = []dns.RR{&dns.AAAA{Hdr: hdr, AAAA: net.IPv6zero}}
	default:
		m.Ns = []dns.RR{&dns.SOA{Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: blockTTL},
			Ns: dotZone + ".", Mbox: "hostmaster." + dotZone + ".", Serial: 1, Refresh: 3600, Retry: 600, Expire: 86400, Minttl: blockTTL}}
	}
	if o := req.IsEdns0(); o != nil {
		m.SetEdns0(o.UDPSize(), false)
		m.IsEdns0().Option = append(m.IsEdns0().Option, &dns.EDNS0_EDE{InfoCode: dns.ExtendedErrorCodeBlocked, ExtraText: why})
	}
	return m
}

func summary(m *dns.Msg) string {
	var parts []string
	for _, rr := range m.Answer {
		switch v := rr.(type) {
		case *dns.A:
			parts = append(parts, v.A.String())
		case *dns.AAAA:
			parts = append(parts, v.AAAA.String())
		case *dns.CNAME:
			parts = append(parts, "→ "+strings.TrimSuffix(v.Target, "."))
		case *dns.HTTPS:
			parts = append(parts, "https "+strings.TrimSuffix(v.Target, "."))
		default:
			parts = append(parts, dns.TypeToString[rr.Header().Rrtype])
		}
		if len(parts) == 4 {
			break
		}
	}
	s := strings.Join(parts, " ")
	if len(s) > 160 {
		s = s[:160]
	}
	return s
}

func shortErr(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 {
		s = s[i+2:]
	}
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

// probes remembers which device asked for a setup page's one time name, so
// the page can say whether the browser it's on is using this resolver.
type probes struct {
	mu   sync.Mutex
	seen map[string]probeHit
}

type probeHit struct {
	Device string
	Proto  string
	At     time.Time
}

func newProbes() *probes { return &probes{seen: map[string]probeHit{}} }

func (p *probes) saw(nonce string, d *Device, proto string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	for k, v := range p.seen {
		if now.Sub(v.At) > 10*time.Minute {
			delete(p.seen, k)
		}
	}
	if len(p.seen) < 1000 {
		p.seen[nonce] = probeHit{Device: d.Name, Proto: proto, At: now}
	}
}

func (p *probes) get(nonce string) (probeHit, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	h, ok := p.seen[nonce]
	return h, ok
}

// logger batches lookups to the database off the answer path. A full queue
// drops the lookup, since an answer must never wait on a write.
type logger struct {
	ch      chan Lookup
	store   *Store
	dropped atomic.Int64
}

func newLogger(store *Store) *logger {
	return &logger{ch: make(chan Lookup, 8192), store: store}
}

func (lg *logger) add(l Lookup) {
	select {
	case lg.ch <- l:
	default:
		lg.dropped.Add(1)
	}
}

func (lg *logger) run(seen func(Lookup)) {
	t := time.NewTicker(time.Second)
	var batch []Lookup
	for {
		select {
		case l := <-lg.ch:
			batch = append(batch, l)
			if len(batch) < 2000 {
				continue
			}
		case <-t.C:
		}
		if len(batch) == 0 {
			continue
		}
		if err := lg.store.Insert(batch); err != nil {
			lg.dropped.Add(int64(len(batch)))
		}
		for _, l := range batch {
			seen(l)
		}
		batch = batch[:0]
	}
}
