package main

import (
	"context"
	"crypto/subtle"
	"net"
	"net/http"
	"net/netip"
	"os"
	"slices"
	"sync"
	"time"

	"logging.bythewood.me/web"
)

// Every container on orchard-edge can reach /ingest and /aggregate, so a
// compromised public site could forge records or read the counts without this.
// An empty token refuses everything instead of letting everything in.
func requireToken(token string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get(web.LoggingTokenHeader)
		if token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func loggingToken() string {
	return os.Getenv("LOGGING_TOKEN")
}

const (
	// The edge compose file names it, and Docker's resolver answers for it on
	// the bridge.
	caddyHost = "orchard-caddy"

	// Bounds how often a stranger's connection can make this ask Docker's
	// resolver again.
	caddyResolveEvery = 2 * time.Second
	caddyResolveWait  = 2 * time.Second
)

// caddyPeer admits connections from the caddy container alone, since Caddy's
// net writer has no way to send a token. A miss resolves the name again, so a
// recreated caddy on a new address still gets in.
type caddyPeer struct {
	host    string
	resolve func(ctx context.Context, host string) ([]netip.Addr, error)

	mu      sync.Mutex
	addrs   []netip.Addr
	checked time.Time
}

func newCaddyPeer(host string) *caddyPeer {
	return &caddyPeer{host: host, resolve: func(ctx context.Context, host string) ([]netip.Addr, error) {
		return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	}}
}

func (p *caddyPeer) allowed(remote net.Addr) bool {
	if p == nil {
		return false
	}
	tcp, ok := remote.(*net.TCPAddr)
	if !ok {
		return false
	}
	ip := tcp.AddrPort().Addr().Unmap()

	p.mu.Lock()
	defer p.mu.Unlock()
	if slices.Contains(p.addrs, ip) {
		return true
	}
	if !p.checked.IsZero() && time.Since(p.checked) < caddyResolveEvery {
		return false
	}
	p.checked = time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), caddyResolveWait)
	defer cancel()
	found, err := p.resolve(ctx, p.host)
	if err != nil {
		return false
	}
	p.addrs = p.addrs[:0]
	for _, a := range found {
		p.addrs = append(p.addrs, a.Unmap())
	}
	return slices.Contains(p.addrs, ip)
}
