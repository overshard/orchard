package main

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/acme"
)

// The DoT certificate is a Let's Encrypt wildcard for every device name. The
// box has no open port 80 or 443 to answer a challenge on, so it proves the
// name with a TXT record written through Cloudflare's API.
const (
	certName   = "*." + dotZone
	challenge  = "_acme-challenge." + dotZone
	zoneName   = "bythewood.me"
	renewAhead = 30 * 24 * time.Hour
	cfAPI      = "https://api.cloudflare.com/client/v4"
)

type certs struct {
	dir     string
	acctKey string
	token   string
	http    *http.Client

	mu       sync.Mutex
	notAfter time.Time
	last     string
	lastAt   time.Time
}

func newCerts(dir, dataDir, token string) *certs {
	c := &certs{dir: dir, acctKey: filepath.Join(dataDir, "acme-account.pem"), token: token, http: &http.Client{Timeout: 30 * time.Second}}
	c.notAfter = c.readNotAfter()
	return c
}

// State is what the pages say about the certificate.
func (c *certs) State() (notAfter time.Time, note string, at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.notAfter, c.last, c.lastAt
}

func (c *certs) note(s string) {
	c.mu.Lock()
	c.last, c.lastAt = s, time.Now()
	c.mu.Unlock()
}

func (c *certs) readNotAfter() time.Time {
	b, err := os.ReadFile(filepath.Join(c.dir, "cert.pem"))
	if err != nil {
		return time.Time{}
	}
	blk, _ := pem.Decode(b)
	if blk == nil {
		return time.Time{}
	}
	cert, err := x509.ParseCertificate(blk.Bytes)
	if err != nil {
		return time.Time{}
	}
	return cert.NotAfter
}

// keep runs once a day and renews when the certificate is missing or inside
// renewAhead of running out.
func (c *certs) keep() {
	for {
		c.once()
		time.Sleep(24 * time.Hour)
	}
}

func (c *certs) once() {
	if c.token == "" {
		c.note("waiting for CLOUDFLARE_TOKEN in .env")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := c.ensureRecord(ctx); err != nil {
		c.note("dns record: " + err.Error())
		slog.Error("dot record", slog.Any("err", err))
	}
	c.mu.Lock()
	na := c.notAfter
	c.mu.Unlock()
	if time.Until(na) > renewAhead {
		c.note("valid, nothing to do")
		return
	}
	if err := c.issue(ctx); err != nil {
		c.note("renewal failed: " + err.Error())
		slog.Error("dot certificate", slog.Any("err", err))
		return
	}
	c.mu.Lock()
	c.notAfter = c.readNotAfter()
	c.mu.Unlock()
	c.note("issued")
	slog.Info("dot certificate issued", slog.Time("not_after", c.readNotAfter()))
}

func (c *certs) account() (crypto.Signer, error) {
	if b, err := os.ReadFile(c.acctKey); err == nil {
		if blk, _ := pem.Decode(b); blk != nil {
			return x509.ParseECPrivateKey(blk.Bytes)
		}
	}
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	der, _ := x509.MarshalECPrivateKey(k)
	if err := os.WriteFile(c.acctKey, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		return nil, err
	}
	return k, nil
}

func (c *certs) issue(ctx context.Context) error {
	key, err := c.account()
	if err != nil {
		return err
	}
	cl := &acme.Client{Key: key, DirectoryURL: acme.LetsEncryptURL}
	if _, err := cl.Register(ctx, &acme.Account{}, acme.AcceptTOS); err != nil && !errors.Is(err, acme.ErrAccountAlreadyExists) {
		return fmt.Errorf("register: %w", err)
	}
	order, err := cl.AuthorizeOrder(ctx, acme.DomainIDs(certName))
	if err != nil {
		return fmt.Errorf("order: %w", err)
	}
	zone, err := c.zoneID(ctx)
	if err != nil {
		return err
	}
	for _, u := range order.AuthzURLs {
		z, err := cl.GetAuthorization(ctx, u)
		if err != nil {
			return err
		}
		if z.Status == acme.StatusValid {
			continue
		}
		var ch *acme.Challenge
		for _, x := range z.Challenges {
			if x.Type == "dns-01" {
				ch = x
			}
		}
		if ch == nil {
			return errors.New("no dns-01 challenge offered")
		}
		val, err := cl.DNS01ChallengeRecord(ch.Token)
		if err != nil {
			return err
		}
		rec, err := c.addTXT(ctx, zone, val)
		if err != nil {
			return err
		}
		defer c.delRecord(context.Background(), zone, rec)
		if err := waitTXT(ctx, val); err != nil {
			return err
		}
		if _, err := cl.Accept(ctx, ch); err != nil {
			return fmt.Errorf("accept: %w", err)
		}
		if _, err := cl.WaitAuthorization(ctx, z.URI); err != nil {
			return fmt.Errorf("authorization: %w", err)
		}
	}
	if order, err = cl.WaitOrder(ctx, order.URI); err != nil {
		return fmt.Errorf("order: %w", err)
	}
	certKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: certName}, DNSNames: []string{certName}}, certKey)
	if err != nil {
		return err
	}
	chain, _, err := cl.CreateOrderCert(ctx, order.FinalizeURL, csr, true)
	if err != nil {
		return fmt.Errorf("finalize: %w", err)
	}
	var certPEM bytes.Buffer
	for _, der := range chain {
		_ = pem.Encode(&certPEM, &pem.Block{Type: "CERTIFICATE", Bytes: der})
	}
	kder, _ := x509.MarshalECPrivateKey(certKey)
	// The key is written before the certificate, since the front reloads on the
	// certificate's change and must find the matching key already there.
	if err := writeAtomic(filepath.Join(c.dir, "key.pem"), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kder}), 0o640); err != nil {
		return err
	}
	return writeAtomic(filepath.Join(c.dir, "cert.pem"), certPEM.Bytes(), 0o644)
}

func writeAtomic(path string, b []byte, mode os.FileMode) error {
	tmp := path + ".new"
	if err := os.WriteFile(tmp, b, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// waitTXT asks the zone's own nameservers until the new record shows, so
// Let's Encrypt isn't asked to look before it's there. A public resolver
// would hold on to an earlier NXDOMAIN.
func waitTXT(ctx context.Context, val string) error {
	ns, err := net.DefaultResolver.LookupNS(ctx, zoneName)
	if err != nil || len(ns) == 0 {
		return fmt.Errorf("finding %s's nameservers: %v", zoneName, err)
	}
	auth := net.JoinHostPort(strings.TrimSuffix(ns[0].Host, "."), "53")
	r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "udp", auth)
	}}
	for i := 0; i < 30; i++ {
		got, _ := r.LookupTXT(ctx, challenge)
		for _, g := range got {
			if g == val {
				time.Sleep(10 * time.Second)
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
	return errors.New("the TXT record never showed")
}

type cfResult struct {
	Success bool                       `json:"success"`
	Errors  []struct{ Message string } `json:"errors"`
	Result  json.RawMessage            `json:"result"`
}

func (c *certs) cf(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequestWithContext(ctx, method, cfAPI+path, rd)
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out cfResult
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("cloudflare answered %d", resp.StatusCode)
	}
	if !out.Success {
		var msgs []string
		for _, e := range out.Errors {
			msgs = append(msgs, e.Message)
		}
		return nil, fmt.Errorf("cloudflare: %s", strings.Join(msgs, ", "))
	}
	return out.Result, nil
}

func (c *certs) zoneID(ctx context.Context) (string, error) {
	raw, err := c.cf(ctx, http.MethodGet, "/zones?name="+zoneName, nil)
	if err != nil {
		return "", err
	}
	var zs []struct{ ID string }
	if json.Unmarshal(raw, &zs) != nil || len(zs) == 0 {
		return "", errors.New("the token can't see " + zoneName)
	}
	return zs[0].ID, nil
}

type cfRecord struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied *bool  `json:"proxied,omitempty"`
	Comment string `json:"comment,omitempty"`
}

func (c *certs) addTXT(ctx context.Context, zone, val string) (string, error) {
	raw, err := c.cf(ctx, http.MethodPost, "/zones/"+zone+"/dns_records", cfRecord{Type: "TXT", Name: challenge, Content: val, TTL: 60})
	if err != nil {
		return "", err
	}
	var r cfRecord
	_ = json.Unmarshal(raw, &r)
	return r.ID, nil
}

func (c *certs) delRecord(ctx context.Context, zone, id string) {
	if id == "" {
		return
	}
	if _, err := c.cf(ctx, http.MethodDelete, "/zones/"+zone+"/dns_records/"+id, nil); err != nil {
		slog.Warn("removing the challenge record", slog.Any("err", err))
	}
}

// ensureRecord makes sure every device name points at the box, unproxied,
// since Cloudflare's proxy only carries HTTP.
func (c *certs) ensureRecord(ctx context.Context) error {
	zone, err := c.zoneID(ctx)
	if err != nil {
		return err
	}
	raw, err := c.cf(ctx, http.MethodGet, "/zones/"+zone+"/dns_records?type=A&name="+certName, nil)
	if err != nil {
		return err
	}
	var have []cfRecord
	_ = json.Unmarshal(raw, &have)
	off := false
	want := cfRecord{Type: "A", Name: certName, Content: publicIP, TTL: 3600, Proxied: &off, Comment: "orchard dns, DoT on 853"}
	if len(have) == 0 {
		_, err = c.cf(ctx, http.MethodPost, "/zones/"+zone+"/dns_records", want)
		return err
	}
	if have[0].Content != publicIP || have[0].Proxied == nil || *have[0].Proxied {
		_, err = c.cf(ctx, http.MethodPut, "/zones/"+zone+"/dns_records/"+have[0].ID, want)
	}
	return err
}
