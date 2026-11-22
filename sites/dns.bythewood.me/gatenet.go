package main

import (
	"context"
	"net/http"
	"net/netip"
	"sync"
	"time"
)

// gateNet asks gate about an address the way Caddy's forward_auth does, under
// a fixed host so no device token reaches gate's records.
type gateNet struct {
	url  string
	host string
	http *http.Client

	mu   sync.Mutex
	seen map[netip.Addr]gateSeen
}

type gateSeen struct {
	at time.Time
	nw netInfo
}

// netInfo is the network gate placed an address on.
type netInfo struct {
	ASN, Org, Country string
}

// netWait is short since a device's lookup waits on it, and an empty network
// only costs the log a column.
const netWait = 500 * time.Millisecond

func newGateNet(url, host string) *gateNet {
	return &gateNet{url: url, host: host, seen: map[netip.Addr]gateSeen{},
		http: &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: 16, IdleConnTimeout: 90 * time.Second}}}
}

// network gives the network gate puts an address on, cached for a minute.
// Gate's verdict is ignored, since only a device that already showed its token
// gets here.
func (g *gateNet) network(ip netip.Addr, path string) netInfo {
	g.mu.Lock()
	s, ok := g.seen[ip]
	g.mu.Unlock()
	if ok && time.Since(s.at) < time.Minute {
		return s.nw
	}
	ctx, cancel := context.WithTimeout(context.Background(), netWait)
	defer cancel()
	_, nw, err := g.ask(ctx, ip, path)
	if err != nil {
		return netInfo{}
	}
	g.mu.Lock()
	g.seen[ip] = gateSeen{at: time.Now(), nw: nw}
	g.mu.Unlock()
	return nw
}

// report tells gate about a request it would otherwise never see, like a bad
// DoT handshake, so its own counts and bans see it too.
func (g *gateNet) report(ip netip.Addr, path string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, _ = g.ask(ctx, ip, path)
}

func (g *gateNet) ask(ctx context.Context, ip netip.Addr, path string) (bool, netInfo, error) {
	if g.url == "" {
		return true, netInfo{}, nil
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, g.url, nil)
	req.Host = g.host
	req.Header.Set("X-Gate-Ip", ip.String())
	req.Header.Set("X-Forwarded-Uri", path)
	req.Header.Set("X-Forwarded-Method", "GET")
	req.Header.Set("User-Agent", "dns")
	resp, err := g.http.Do(req)
	if err != nil {
		return true, netInfo{}, err
	}
	resp.Body.Close()
	return resp.StatusCode < 300, netInfo{resp.Header.Get("Gate-Asn"), resp.Header.Get("Gate-Org"), resp.Header.Get("Gate-Country")}, nil
}

func (g *gateNet) tidy() {
	g.mu.Lock()
	defer g.mu.Unlock()
	for ip, s := range g.seen {
		if time.Since(s.at) > 10*time.Minute {
			delete(g.seen, ip)
		}
	}
}
