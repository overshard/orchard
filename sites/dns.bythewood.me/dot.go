package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"
)

// The DoT front is the only thing published to the internet. It holds the
// certificate and nothing else, and hands each query to orchard-dns over the
// bridge, so the database and the Cloudflare token stay off the open port.
const (
	dotListen      = ":8853"
	perIPConns     = 16
	totalConns     = 512
	handshakeLimit = 5 * time.Second
	// spareConns is room past totalConns for addresses that recently finished
	// a handshake, so a flood of stalled strangers can't lock my phone out.
	spareConns     = 64
	goodFor        = 24 * time.Hour
	idleLimit      = 2 * time.Minute
	inflightPerCon = 32
	badBeforeBan   = 10
	banFor         = 24 * time.Hour
)

type front struct {
	certDir string
	appURL  string
	gateURL string
	cert    atomic.Pointer[tls.Certificate]
	certMod time.Time
	http    *http.Client

	mu     sync.Mutex
	conns  map[netip.Addr]int
	total  int
	bad    map[netip.Addr]*badCount
	tokens map[string]tokenSeen
	gate   map[netip.Addr]gateSeen
	good   map[netip.Addr]time.Time
	ready  atomic.Bool
}

type badCount struct {
	n      int
	first  time.Time
	banned time.Time
}

type tokenSeen struct {
	ok bool
	at time.Time
}

type gateSeen struct {
	ok bool
	at time.Time
}

func newFront(certDir, app, gate string) *front {
	return &front{
		certDir: certDir, appURL: app, gateURL: gate,
		http:  &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: 64, IdleConnTimeout: 90 * time.Second}},
		conns: map[netip.Addr]int{}, bad: map[netip.Addr]*badCount{},
		tokens: map[string]tokenSeen{}, gate: map[netip.Addr]gateSeen{}, good: map[netip.Addr]time.Time{},
	}
}

func runFront(certDir string) error {
	f := newFront(certDir, appURL, gateCheckURL)
	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
			if !f.ready.Load() {
				plainNoStore(w, http.StatusServiceUnavailable, "waiting for a certificate")
				return
			}
			healthz(w, r)
		})
		_ = http.ListenAndServe("127.0.0.1:8000", mux)
	}()
	for !f.loadCert() {
		slog.Info("dot front waiting for a certificate", slog.String("dir", certDir))
		time.Sleep(time.Minute)
	}
	go func() {
		for range time.Tick(10 * time.Minute) {
			f.loadCert()
			f.tidy()
		}
	}()
	ln, err := net.Listen("tcp4", dotListen)
	if err != nil {
		return err
	}
	f.ready.Store(true)
	slog.Info("dot front listening", slog.String("addr", dotListen))
	return f.accept(ln)
}

func (f *front) accept(ln net.Listener) error {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, NextProtos: []string{"dot"}}
	for {
		c, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return err
			}
			time.Sleep(50 * time.Millisecond)
			continue
		}
		go f.serve(c, cfg)
	}
}

func (f *front) loadCert() bool {
	certPath, keyPath := f.certDir+"/cert.pem", f.certDir+"/key.pem"
	st, err := os.Stat(certPath)
	if err != nil {
		return false
	}
	if !st.ModTime().After(f.certMod) && f.cert.Load() != nil {
		return true
	}
	c, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		slog.Error("dot front certificate", slog.Any("err", err))
		return f.cert.Load() != nil
	}
	f.cert.Store(&c)
	f.certMod = st.ModTime()
	slog.Info("dot front loaded a certificate")
	return true
}

func (f *front) tidy() {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now()
	for ip, b := range f.bad {
		if now.Sub(b.first) > time.Hour && now.After(b.banned) {
			delete(f.bad, ip)
		}
	}
	for t, s := range f.tokens {
		if now.Sub(s.at) > 10*time.Minute && !(s.ok && now.Sub(s.at) < goodFor) {
			delete(f.tokens, t)
		}
	}
	for ip, at := range f.good {
		if now.Sub(at) > goodFor {
			delete(f.good, ip)
		}
	}
	for ip, g := range f.gate {
		if now.Sub(g.at) > 10*time.Minute {
			delete(f.gate, ip)
		}
	}
}

func (f *front) admit(ip netip.Addr) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if b := f.bad[ip]; b != nil && time.Now().Before(b.banned) {
		return false
	}
	limit := totalConns
	if _, ok := f.good[ip]; ok {
		limit += spareConns
	}
	if f.total >= limit || f.conns[ip] >= perIPConns {
		return false
	}
	f.total++
	f.conns[ip]++
	return true
}

func (f *front) release(ip netip.Addr) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.total--
	if f.conns[ip]--; f.conns[ip] <= 0 {
		delete(f.conns, ip)
	}
}

func (f *front) wasGood(ip netip.Addr) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.good[ip]
	return ok
}

func (f *front) markGood(ip netip.Addr) {
	f.mu.Lock()
	f.good[ip] = time.Now()
	f.mu.Unlock()
}

// strike counts a failed handshake, bans the address here after too many, and
// tells gate so its own counts and bans see it too. A stalled handshake only
// counts here, since a phone on a weak signal can stall too.
func (f *front) strike(ip netip.Addr, why string, tellGate bool) {
	f.mu.Lock()
	b := f.bad[ip]
	now := time.Now()
	if b == nil || now.Sub(b.first) > time.Hour {
		b = &badCount{first: now}
		f.bad[ip] = b
	}
	b.n++
	if b.n >= badBeforeBan {
		b.banned = now.Add(banFor)
	}
	f.mu.Unlock()
	if tellGate {
		go f.askGate(ip, "/bad-token")
	}
	slog.Info("dot handshake refused", slog.String("ip", ip.String()), slog.String("why", why))
}

func (f *front) gateOK(ip netip.Addr) bool {
	f.mu.Lock()
	g, ok := f.gate[ip]
	f.mu.Unlock()
	if ok && time.Since(g.at) < time.Minute {
		return g.ok
	}
	allowed := f.askGate(ip, "/")
	f.mu.Lock()
	f.gate[ip] = gateSeen{ok: allowed, at: time.Now()}
	f.mu.Unlock()
	return allowed
}

// askGate puts a connection to gate the way Caddy puts a request, under a
// fixed host so no token reaches gate's records. With gate down it lets the
// connection through, since a token still has to match after it.
func (f *front) askGate(ip netip.Addr, path string) bool {
	req, _ := http.NewRequest(http.MethodGet, f.gateURL, nil)
	req.Host = "dot." + dotZone
	req.Header.Set("X-Gate-Ip", ip.String())
	req.Header.Set("X-Forwarded-Uri", path)
	req.Header.Set("X-Forwarded-Method", "GET")
	req.Header.Set("User-Agent", "dns-over-tls")
	resp, err := f.http.Do(req)
	if err != nil {
		return true
	}
	resp.Body.Close()
	return resp.StatusCode < 300
}

// tokenOK says whether the app knows a token. sure is false when the app
// couldn't say, during a deploy for instance, and then a token it knew lately
// still works and an unknown one is turned away without a strike.
func (f *front) tokenOK(token string) (ok, sure bool) {
	f.mu.Lock()
	s, had := f.tokens[token]
	f.mu.Unlock()
	if had && (s.ok && time.Since(s.at) < 5*time.Minute || !s.ok && time.Since(s.at) < time.Minute) {
		return s.ok, true
	}
	req, _ := http.NewRequest(http.MethodGet, f.appURL+"/internal/token/"+token, nil)
	req.Header.Set("X-Dns-Key", bridgeKey)
	resp, err := f.http.Do(req)
	if err != nil {
		return had && s.ok, had && s.ok
	}
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusNotFound:
	default:
		return had && s.ok, had && s.ok
	}
	good := resp.StatusCode == http.StatusNoContent
	f.mu.Lock()
	f.tokens[token] = tokenSeen{ok: good, at: time.Now()}
	f.mu.Unlock()
	return good, true
}

// tokenFrom takes the device token out of a server name like
// <token>.dns.bythewood.me.
func tokenFrom(sni string) (string, bool) {
	t, ok := strings.CutSuffix(strings.ToLower(sni), "."+dotZone)
	if !ok || !validToken(t) {
		return "", false
	}
	return t, true
}

func (f *front) serve(c net.Conn, base *tls.Config) {
	ap, err := netip.ParseAddrPort(c.RemoteAddr().String())
	if err != nil {
		c.Close()
		return
	}
	ip := ap.Addr().Unmap()
	if !f.admit(ip) {
		c.Close()
		return
	}
	defer f.release(ip)
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(handshakeLimit))
	if !f.gateOK(ip) {
		return
	}

	var token string
	unsure := false
	cfg := base.Clone()
	cfg.GetConfigForClient = func(h *tls.ClientHelloInfo) (*tls.Config, error) {
		t, ok := tokenFrom(h.ServerName)
		if !ok {
			return nil, errors.New("no device name")
		}
		ok, sure := f.tokenOK(t)
		if !sure {
			unsure = true
			return nil, errors.New("the app didn't answer")
		}
		if !ok {
			return nil, errors.New("unknown device")
		}
		token = t
		inner := base.Clone()
		inner.Certificates = []tls.Certificate{*f.cert.Load()}
		return inner, nil
	}
	tc := tls.Server(c, cfg)
	if err := tc.Handshake(); err != nil {
		switch {
		case token != "" || unsure:
		case isTimeout(err):
			if !f.wasGood(ip) {
				f.strike(ip, "stalled handshake", false)
			}
		case !isNetNoise(err):
			f.strike(ip, err.Error(), true)
		}
		return
	}
	f.markGood(ip)

	var wmu sync.Mutex
	sem := make(chan struct{}, inflightPerCon)
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		_ = c.SetReadDeadline(time.Now().Add(idleLimit))
		var n uint16
		if err := binary.Read(tc, binary.BigEndian, &n); err != nil || n == 0 {
			return
		}
		msg := make([]byte, n)
		if _, err := io.ReadFull(tc, msg); err != nil {
			return
		}
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			out := f.ask(token, ip, msg)
			if out == nil {
				return
			}
			wmu.Lock()
			defer wmu.Unlock()
			_ = c.SetWriteDeadline(time.Now().Add(10 * time.Second))
			buf := make([]byte, 2+len(out))
			binary.BigEndian.PutUint16(buf, uint16(len(out)))
			copy(buf[2:], out)
			_, _ = tc.Write(buf)
		}()
	}
}

// ask hands a query to the app. When the app can't answer, the client gets a
// SERVFAIL at once instead of waiting out its own timeout.
func (f *front) ask(token string, ip netip.Addr, msg []byte) []byte {
	out := f.askApp(token, ip, msg)
	if out == nil {
		return servfail(msg)
	}
	return out
}

func servfail(msg []byte) []byte {
	req := new(dns.Msg)
	if req.Unpack(msg) != nil {
		return nil
	}
	m := new(dns.Msg)
	m.SetRcode(req, dns.RcodeServerFailure)
	out, err := m.Pack()
	if err != nil {
		return nil
	}
	return out
}

func (f *front) askApp(token string, ip netip.Addr, msg []byte) []byte {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, f.appURL+"/internal/query", bytes.NewReader(msg))
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("X-Dns-Token", token)
	req.Header.Set("X-Dns-Ip", ip.String())
	req.Header.Set("X-Dns-Key", bridgeKey)
	resp, err := f.http.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	out, err := io.ReadAll(io.LimitReader(resp.Body, maxMsg))
	if err != nil || len(out) == 0 {
		return nil
	}
	return out
}

// isNetNoise is a client that hung up, which isn't a bad name.
func isNetNoise(err error) bool {
	return errors.Is(err, io.EOF) || strings.Contains(err.Error(), "connection reset")
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}
