package main

import (
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestBlockedHTTPSHasSOA(t *testing.T) {
	req := new(dns.Msg)
	req.SetQuestion("tracker.example.", dns.TypeHTTPS)
	m := blocked(req, "pro")
	if len(m.Answer) != 0 || len(m.Ns) != 1 {
		t.Fatalf("want NODATA with an SOA, got %v", m)
	}
	if soa := m.Ns[0].(*dns.SOA); soa.Minttl != blockTTL {
		t.Fatalf("minttl %d", soa.Minttl)
	}
}

func TestPadToBlock(t *testing.T) {
	req := new(dns.Msg)
	req.SetQuestion("example.com.", dns.TypeA)
	req.SetEdns0(1232, false)
	req.IsEdns0().Option = append(req.IsEdns0().Option, &dns.EDNS0_PADDING{})
	resp := blocked(req, "pro")
	pad(req, resp)
	out, err := resp.Pack()
	if err != nil {
		t.Fatal(err)
	}
	if len(out)%468 != 0 {
		t.Fatalf("padded to %d", len(out))
	}
	plain := new(dns.Msg)
	plain.SetQuestion("example.com.", dns.TypeA)
	r2 := blocked(plain, "pro")
	pad(plain, r2)
	if r2.IsEdns0() != nil {
		t.Fatal("padded a query that didn't ask")
	}
}

func TestDesignateNamesTheDevice(t *testing.T) {
	req := new(dns.Msg)
	req.SetQuestion(ddrName+".", dns.TypeSVCB)
	m := designate(req, &Device{Token: "abcdefghijklmnop"})
	if len(m.Answer) != 2 {
		t.Fatalf("got %v", m.Answer)
	}
	if got := m.Answer[0].(*dns.SVCB).Target; got != "abcdefghijklmnop."+dotZone+"." {
		t.Fatalf("dot target %s", got)
	}
}

func TestSafeBack(t *testing.T) {
	for in, want := range map[string]string{
		"/log?status=blocked": "/log?status=blocked",
		"//evil.com":          "/lists",
		"/\\evil.com":         "/lists",
		"https://evil.com":    "/lists",
		"log":                 "/lists",
	} {
		if got := safeBack(in, "/lists"); got != want {
			t.Errorf("safeBack(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDeviceRulesAndRewrites(t *testing.T) {
	s, d := testSite(t)
	other, _ := s.store.AddDevice("laptop", "computer")
	_ = s.store.PutRule("tracker.example", "allow", "")
	_ = s.store.PutRuleFor("tracker.example", "block", "", d.ID, "")
	_ = s.store.PutRuleFor("nas.home.example", "rewrite", "", 0, "192.168.1.20")
	_ = s.reload()
	if v := s.res.decide(s.res.device(d.Token), "tracker.example"); v.Status != "deny" {
		t.Errorf("the device's own block wins for it: %+v", v)
	}
	if v := s.res.decide(s.res.device(other.Token), "tracker.example"); v.Status != "allow" {
		t.Errorf("every device's allow holds for the rest: %+v", v)
	}
	req := new(dns.Msg)
	req.SetQuestion("nas.home.example.", dns.TypeA)
	m := s.res.Answer(s.res.device(d.Token), req, "dot", "")
	if len(m.Answer) != 1 || m.Answer[0].(*dns.A).A.String() != "192.168.1.20" {
		t.Errorf("rewrite answered %v", m.Answer)
	}
}

func TestSafeSearchOnlyWhenOn(t *testing.T) {
	s, d := testSite(t)
	if v := s.res.decide(d, "www.google.com"); v.Status == "rewrite" {
		t.Fatal("safe search is off by default")
	}
	_ = s.store.UpdateDevice(d.ID, d.Name, d.Kind, nil, true)
	_ = s.reload()
	if v := s.res.decide(s.res.device(d.Token), "www.google.com"); v.Status != "rewrite" || v.Target != "forcesafesearch.google.com" {
		t.Fatalf("got %+v", v)
	}
}

func TestNewNetsNeedASettledDevice(t *testing.T) {
	now := time.Now()
	ds := []*Device{{ID: 1}, {ID: 2}}
	all := []Net{
		{Device: 1, ASN: "20115", First: now.Add(-72 * time.Hour)},
		{Device: 1, ASN: "21928", First: now.Add(-time.Hour)},
		{Device: 2, ASN: "16276", First: now.Add(-time.Hour)},
	}
	got := newNets(all, ds, now)
	if len(got) != 1 || got[0].ASN != "21928" {
		t.Fatalf("got %+v", got)
	}
}
