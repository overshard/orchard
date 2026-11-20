package main

import (
	"encoding/base64"
	"io"
	"net/http"
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
	s.serveWire(w, d, raw, "doh", web.ClientIP(r))
}

// internalQuery is the DoT front's way in, over the bridge. Caddy answers
// /internal/ with a 404 on the public name.
func (s *site) internalQuery(w http.ResponseWriter, r *http.Request) {
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
	s.serveWire(w, d, raw, "dot", r.Header.Get("X-Dns-Ip"))
}

func (s *site) internalToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.res.device(r.PathValue("token")) == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *site) serveWire(w http.ResponseWriter, d *Device, raw []byte, proto, ip string) {
	req := new(dns.Msg)
	if err := req.Unpack(raw); err != nil {
		plainNoStore(w, http.StatusBadRequest, "that isn't a DNS message")
		return
	}
	out, err := s.res.Answer(d, req, proto, ip).Pack()
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
