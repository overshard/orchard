package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func selfSigned(t *testing.T, dir string) {
	t.Helper()
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: certName}, DNSNames: []string{certName},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	kd, _ := x509.MarshalECPrivateKey(k)
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "cert.pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "key.pem"), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd}), 0o600)
}

func TestDoTFront(t *testing.T) {
	old := bridgeKey
	bridgeKey = "front-key"
	t.Cleanup(func() { bridgeKey = old })
	s, d := testSite(t)
	app := httptest.NewServer(s.handler())
	defer app.Close()
	gateAsked := make(chan string, 16)
	gate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gateAsked <- r.Host + " " + r.Header.Get("X-Forwarded-Uri")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer gate.Close()
	dir := t.TempDir()
	selfSigned(t, dir)
	f := newFront(dir, app.URL, gate.URL)
	if !f.loadCert() {
		t.Fatal("no certificate")
	}
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go f.accept(ln)

	c := &dns.Client{Net: "tcp-tls", Timeout: 5 * time.Second, TLSConfig: &tls.Config{ServerName: d.Token + "." + dotZone, InsecureSkipVerify: true}}
	m, _, err := c.Exchange(query("ads.example.net", dns.TypeA), ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Answer) != 1 || m.Answer[0].(*dns.A).A.String() != "0.0.0.0" {
		t.Errorf("blocked over DoT: %v", m.Answer)
	}
	if got := <-gateAsked; got != "dot."+dotZone+" /" {
		t.Errorf("gate should be asked under a fixed host, got %q", got)
	}

	bad := &dns.Client{Net: "tcp-tls", Timeout: 5 * time.Second, TLSConfig: &tls.Config{ServerName: "aaaaaaaaaaaaaaaa." + dotZone, InsecureSkipVerify: true}}
	if _, _, err := bad.Exchange(query("ok.example.org", dns.TypeA), ln.Addr().String()); err == nil {
		t.Error("an unknown token must fail the handshake")
	}
	time.Sleep(100 * time.Millisecond)
	f.mu.Lock()
	n := 0
	if b := f.bad[netipLoopback()]; b != nil {
		n = b.n
	}
	f.mu.Unlock()
	if n != 1 {
		t.Errorf("the bad handshake should count once, got %d", n)
	}
}
