package main

import (
	"crypto/tls"
	"net"
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

// rules is the hand written allows and blocks, swapped whole on every change.
type rules struct {
	allow map[string]bool
	block map[string]bool
}

func (r *rules) match(name string, m map[string]bool) string {
	for n := name; ; {
		if m[n] {
			return n
		}
		i := strings.IndexByte(n, '.')
		if i < 0 {
			return ""
		}
		n = n[i+1:]
	}
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
	fellBack atomic.Int64
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
	n := &rules{allow: map[string]bool{}, block: map[string]bool{}}
	for _, x := range rs {
		if x.Action == "allow" {
			n.allow[x.Domain] = true
		} else {
			n.block[x.Domain] = true
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

// Verdict is what the lists and rules say about one name.
type Verdict struct {
	Status string
	Reason string
	Domain string
}

// decide says what a device gets for a name before anything is asked
// upstream. Allow beats block, and my own rules beat any list.
func (r *resolver) decide(d *Device, name string) Verdict {
	for _, own := range neverBlock {
		if name == own || strings.HasSuffix(name, "."+own) {
			return Verdict{Status: "pass"}
		}
	}
	rs := r.rules.Load()
	listed, mask := r.lists.tab.Load().match(name)
	key := ""
	for i, li := range lists {
		if mask&(1<<i) != 0 && !d.Off(li.Key) {
			key = li.Key
			break
		}
	}
	if a := rs.match(name, rs.allow); a != "" {
		if key != "" {
			return Verdict{Status: "allow", Reason: key, Domain: a}
		}
		return Verdict{Status: "pass"}
	}
	if b := rs.match(name, rs.block); b != "" {
		return Verdict{Status: "deny", Reason: "rule", Domain: b}
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

// Answer resolves one query for a device and logs it. It always returns a
// message to send back.
func (r *resolver) Answer(d *Device, req *dns.Msg, proto, ip string) *dns.Msg {
	start := time.Now()
	if len(req.Question) != 1 || req.Opcode != dns.OpcodeQuery {
		m := new(dns.Msg)
		m.SetRcode(req, dns.RcodeFormatError)
		return m
	}
	q := req.Question[0]
	name := strings.TrimSuffix(strings.ToLower(q.Name), ".")
	l := Lookup{At: start, Device: d.ID, Name: name, Qtype: q.Qtype, Proto: proto, IP: ip}
	resp := r.answer(d, req, name, &l)
	l.Rcode = resp.Rcode
	l.Ms = int(time.Since(start).Milliseconds())
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
	if v := r.decide(d, name); v.Status == "block" || v.Status == "deny" {
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
	resp, err := r.forward(req)
	if err != nil {
		resp, err = r.fallback(req)
		if err != nil {
			l.Status, l.Reason = "fail", shortErr(err)
			m := new(dns.Msg)
			m.SetRcode(req, dns.RcodeServerFailure)
			return m
		}
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
	resp, _, err := r.fall.Exchange(up, fallbackAddr)
	if err != nil {
		return nil, err
	}
	resp.Id = req.Id
	return resp, nil
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

// blocked answers 0.0.0.0 or :: for an address and nothing for anything else,
// so an app fails at once instead of retrying, and says why in an Extended
// DNS Error when the client speaks EDNS. The SOA lets a client cache an empty
// answer for blockTTL instead of asking again straight away.
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
