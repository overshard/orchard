package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"logging.bythewood.me/web"
)

// loopbackCaddy stands in for Docker's resolver, which is where the tests
// connect from.
func loopbackCaddy() *caddyPeer {
	return &caddyPeer{host: "caddy", resolve: func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}}
}

func TestRequireToken(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }
	for _, tc := range []struct {
		name, token, sent string
		want              int
	}{
		{"right token", "s3cret", "s3cret", http.StatusNoContent},
		{"wrong token", "s3cret", "s3creu", http.StatusForbidden},
		{"no header", "s3cret", "", http.StatusForbidden},
		{"prefix of the token", "s3cret", "s3c", http.StatusForbidden},
		{"no token configured", "", "", http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/ingest", nil)
			if tc.sent != "" {
				req.Header.Set(web.LoggingTokenHeader, tc.sent)
			}
			rec := httptest.NewRecorder()
			requireToken(tc.token, ok)(rec, req)
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

func tcpAddr(s string) net.Addr {
	return net.TCPAddrFromAddrPort(netip.MustParseAddrPort(s))
}

// A recreated caddy comes back on a new address, which a miss has to pick up,
// and a stranger's miss must not send every connection to the resolver.
func TestCaddyPeerResolvesAgainOnAMiss(t *testing.T) {
	current := "172.18.0.2"
	lookups := 0
	p := &caddyPeer{host: "caddy", resolve: func(context.Context, string) ([]netip.Addr, error) {
		lookups++
		return []netip.Addr{netip.MustParseAddr(current)}, nil
	}}

	if !p.allowed(tcpAddr("172.18.0.2:40000")) {
		t.Fatal("caddy's own address was refused")
	}
	if !p.allowed(tcpAddr("[::ffff:172.18.0.2]:40000")) {
		t.Error("a v4 mapped address was refused")
	}
	if p.allowed(tcpAddr("172.18.0.9:40000")) {
		t.Error("a stranger on the bridge was let in")
	}
	n := lookups
	if p.allowed(tcpAddr("172.18.0.9:40001")) || lookups != n {
		t.Errorf("a second miss inside %s resolved again, %d lookups", caddyResolveEvery, lookups)
	}

	current = "172.18.0.7"
	p.checked = time.Now().Add(-caddyResolveEvery)
	if !p.allowed(tcpAddr("172.18.0.7:40000")) {
		t.Error("caddy on its new address was refused")
	}
	if p.allowed(tcpAddr("172.18.0.2:40000")) {
		t.Error("caddy's old address is still let in")
	}
}

func TestCaddyPeerRefusesOnLookupFailure(t *testing.T) {
	p := &caddyPeer{host: "caddy", resolve: func(context.Context, string) ([]netip.Addr, error) {
		return nil, fmt.Errorf("no such host")
	}}
	if p.allowed(tcpAddr("172.18.0.2:40000")) {
		t.Error("let a peer in with nothing resolved")
	}
	var nilPeer *caddyPeer
	if nilPeer.allowed(tcpAddr("127.0.0.1:40000")) {
		t.Error("a nil peer let a connection in")
	}
}

// Over a real socket, a peer that isn't caddy writes lines that never land.
func TestCaddyLogSocketRefusesOtherPeers(t *testing.T) {
	db := testDB(t)
	s := testSite(t, db)
	s.caddy = &caddyPeer{host: "caddy", resolve: func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("172.18.0.2")}, nil
	}}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go s.serveCaddyLogs(t.Context(), ln)

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		fmt.Fprintln(conn, caddyLine)
	}
	conn.Close()

	time.Sleep(500 * time.Millisecond)
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM records WHERE source = 'caddy'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("records = %d from a peer that isn't caddy, want 0", n)
	}
}
