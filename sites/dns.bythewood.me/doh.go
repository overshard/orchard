package main

import (
	"crypto/subtle"
	"encoding/base64"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/miekg/dns"

	"dns.bythewood.me/web"
)

const maxMsg = 65535

// doh answers DNS over HTTPS at /q/<token>, GET with ?dns= or POST with the
// message as the body, the two forms RFC 8484 has.
func (s *site) doh(w http.ResponseWriter, r *http.Request) {
	d := s.res.device(r.PathValue("token"))
	if d == nil {
		plainNoStore(w, http.StatusNotFound, "404 page not found")
		return
	}
	var raw []byte
	var err error
	switch r.Method {
	case http.MethodGet:
		raw, err = base64.RawURLEncoding.DecodeString(strings.TrimRight(r.URL.Query().Get("dns"), "="))
	case http.MethodPost:
		if ct := r.Header.Get("Content-Type"); ct != "application/dns-message" {
			plainNoStore(w, http.StatusUnsupportedMediaType, "send application/dns-message")
			return
		}
		raw, err = io.ReadAll(io.LimitReader(r.Body, maxMsg+1))
	default:
		plainNoStore(w, http.StatusMethodNotAllowed, "GET or POST")
		return
	}
	if err != nil || len(raw) == 0 || len(raw) > maxMsg {
		plainNoStore(w, http.StatusBadRequest, "no DNS message")
		return
	}
	s.serveWire(w, d, raw, Client{Proto: "doh", IP: web.ClientIP(r), ASN: r.Header.Get("Gate-Asn"), Org: r.Header.Get("Gate-Org"), Country: r.Header.Get("Gate-Country")})
}

// bridgeKey is shared by the app and the DoT front, so another container on
// the bridge can't ask through /internal/ as a device or test tokens there.
var bridgeKey = os.Getenv("DNS_BRIDGE_PASSWORD")

// fromFront refuses everything when the key is unset, since an empty key
// would let any container on the bridge in.
func fromFront(r *http.Request) bool {
	return bridgeKey != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Dns-Key")), []byte(bridgeKey)) == 1
}

// internalQuery is the DoT front's way in, over the bridge. Caddy answers
// /internal/ with a 404 on the public name.
func (s *site) internalQuery(w http.ResponseWriter, r *http.Request) {
	if !fromFront(r) {
		plainNoStore(w, http.StatusForbidden, "not from the front")
		return
	}
	d := s.res.device(r.Header.Get("X-Dns-Token"))
	if d == nil {
		plainNoStore(w, http.StatusForbidden, "unknown token")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxMsg+1))
	if err != nil || len(raw) == 0 || len(raw) > maxMsg {
		plainNoStore(w, http.StatusBadRequest, "no DNS message")
		return
	}
	s.serveWire(w, d, raw, Client{Proto: "dot", IP: r.Header.Get("X-Dns-Ip"), ASN: r.Header.Get("X-Dns-Asn"), Org: r.Header.Get("X-Dns-Org"), Country: r.Header.Get("X-Dns-Country")})
}

func (s *site) internalToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !fromFront(r) {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	if s.res.device(r.PathValue("token")) == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *site) serveWire(w http.ResponseWriter, d *Device, raw []byte, c Client) {
	req := new(dns.Msg)
	if err := req.Unpack(raw); err != nil {
		plainNoStore(w, http.StatusBadRequest, "that isn't a DNS message")
		return
	}
	resp := s.res.AnswerFor(d, req, c)
	pad(req, resp)
	out, err := resp.Pack()
	if err != nil {
		plainNoStore(w, http.StatusInternalServerError, "couldn't pack the answer")
		return
	}
	// no-store, so every lookup reaches here and lands in the log instead of
	// being answered from Cloudflare's cache.
	w.Header().Set("Content-Type", "application/dns-message")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(out)
}

func plainNoStore(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(msg + "\n"))
}
