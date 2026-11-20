package main

import (
	"testing"

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
