package main

import (
	"context"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// gateCheckURL is gate's forward_auth endpoint, which answers with the network
// and country it places an address on.
const gateCheckURL = "http://orchard-gate:8000/check"

// Where is the network and country gate put an address on.
type Where struct{ Org, Country string }

func (w Where) String() string {
	switch {
	case w.Org != "" && w.Country != "":
		return w.Org + ", " + w.Country
	case w.Org != "":
		return w.Org
	}
	return w.Country
}

// netLookup asks gate about each address once a day. Gate counts each ask as
// a request under a path of pass's own, so they're easy to tell apart.
type netLookup struct {
	url  string
	http *http.Client
	mu   sync.Mutex
	seen map[string]netSeen
}

type netSeen struct {
	at time.Time
	w  Where
}

func newNetLookup(url string) *netLookup {
	return &netLookup{url: url, http: &http.Client{Timeout: 2 * time.Second}, seen: map[string]netSeen{}}
}

// where never waits long, since a page renders behind it. An address gate
// doesn't answer for is shown bare.
func (n *netLookup) where(ctx context.Context, ip string) Where {
	if n == nil || n.url == "" {
		return Where{}
	}
	if _, err := netip.ParseAddr(ip); err != nil {
		return Where{}
	}
	n.mu.Lock()
	s, ok := n.seen[ip]
	n.mu.Unlock()
	if ok && time.Since(s.at) < 24*time.Hour {
		return s.w
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, n.url, nil)
	if err != nil {
		return Where{}
	}
	req.Host = "pass.bythewood.me"
	req.Header.Set("X-Gate-Ip", ip)
	req.Header.Set("X-Forwarded-Uri", "/__pass/where")
	req.Header.Set("X-Forwarded-Method", "GET")
	req.Header.Set("User-Agent", "pass")
	resp, err := n.http.Do(req)
	if err != nil {
		return Where{}
	}
	resp.Body.Close()
	w := Where{Org: shortOrg(resp.Header.Get("Gate-Org")), Country: resp.Header.Get("Gate-Country")}
	n.mu.Lock()
	if len(n.seen) > 5000 {
		n.seen = map[string]netSeen{}
	}
	n.seen[ip] = netSeen{at: time.Now(), w: w}
	n.mu.Unlock()
	return w
}

// shortOrg drops the company suffixes that make a network name wrap.
func shortOrg(s string) string {
	s = strings.TrimSpace(s)
	for _, suf := range []string{", LLC", " LLC", ", Inc.", " Inc.", ", Inc", " Inc", " Corporation", " Co., Ltd.", " Ltd."} {
		s = strings.TrimSuffix(s, suf)
	}
	return s
}

// places looks up every distinct address in a set of events at once.
func (n *netLookup) places(ctx context.Context, ips []string) map[string]Where {
	want := map[string]bool{}
	for _, ip := range ips {
		if ip != "" {
			want[ip] = true
		}
	}
	out := map[string]Where{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for ip := range want {
		wg.Go(func() {
			w := n.where(ctx, ip)
			mu.Lock()
			out[ip] = w
			mu.Unlock()
		})
	}
	wg.Wait()
	return out
}
