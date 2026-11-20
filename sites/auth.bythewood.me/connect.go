package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// mcpBack is the one place a connect may return to. Anything else in ?back=
// is refused, so this can't be turned into a way to hand a session to some
// other site.
const mcpBack = "https://mcp.bythewood.me/oauth/back"

// connectorTTL is how long an agent's session lives. It reads only, and
// revoking it on the sessions page ends it at once.
const connectorTTL = 90 * 24 * time.Hour

const connectorUA = "connector: "

type connectRequest struct {
	Req, Client, Host string
}

// handoffs are sessions minted for a connector, waiting for mcp to collect
// them over the bridge. Memory is enough since each lives a minute.
type handoffs struct {
	mu sync.Mutex
	m  map[string]handoff
}

type handoff struct {
	session string
	expires time.Time
}

func (h *handoffs) put(session string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	code := base64.RawURLEncoding.EncodeToString(raw)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.m == nil {
		h.m = map[string]handoff{}
	}
	now := time.Now()
	for k, v := range h.m {
		if now.After(v.expires) {
			delete(h.m, k)
		}
	}
	h.m[code] = handoff{session: session, expires: now.Add(time.Minute)}
	return code, nil
}

func (h *handoffs) take(code string) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	v, ok := h.m[code]
	delete(h.m, code)
	if !ok || time.Now().After(v.expires) {
		return "", false
	}
	return v.session, true
}

func readConnect(r *http.Request) (connectRequest, bool) {
	c := connectRequest{Req: r.FormValue("req"), Client: strings.TrimSpace(r.FormValue("client")), Host: r.FormValue("host")}
	if r.FormValue("back") != mcpBack || c.Req == "" || len(c.Req) > 128 || len(c.Client) > 80 || len(c.Host) > 200 {
		return c, false
	}
	if c.Client == "" {
		c.Client = "an agent"
	}
	return c, true
}

// connectForm asks whether an agent may read every tool as you. It wants a
// fresh code first, the same as anything that hands out a credential.
func (s *site) connectForm(w http.ResponseWriter, r *http.Request, l live) {
	c, ok := readConnect(r)
	if !ok {
		http.Error(w, "that connect link isn't one mcp.bythewood.me made", http.StatusBadRequest)
		return
	}
	data := s.page(r, "Connect an agent", "")
	data.Connect = c
	s.renderer.Render(w, http.StatusOK, "connect.html", data)
}

func (s *site) connectSubmit(w http.ResponseWriter, r *http.Request, l live) {
	c, ok := readConnect(r)
	if !ok {
		http.Error(w, "that connect link isn't one mcp.bythewood.me made", http.StatusBadRequest)
		return
	}
	back := url.Values{"req": {c.Req}}
	if r.FormValue("allow") != "yes" {
		back.Set("denied", "1")
		s.handBack(w, r, c, mcpBack+"?"+back.Encode())
		return
	}
	value, err := newConnectorSession(s.db, r, c.Client)
	if err != nil {
		s.fail(w, r, "making the connector's session", err)
		return
	}
	code, err := s.handoffs.put(value)
	if err != nil {
		s.fail(w, r, "handing the session over", err)
		return
	}
	audit(s.db, r, evConnectorAdded, c.Client)
	back.Set("handoff", code)
	s.handBack(w, r, c, mcpBack+"?"+back.Encode())
}

// handBack leaves by a meta refresh and not a redirect, since Chrome holds a
// form's redirects to form-action 'self' and mcp's /oauth/back is another host.
func (s *site) handBack(w http.ResponseWriter, r *http.Request, c connectRequest, to string) {
	data := s.page(r, "Connect an agent", "")
	data.Connect = c
	data.Refresh = to
	s.renderer.Render(w, http.StatusOK, "connect.html", data)
}

// collectHandoff is mcp picking up the session over the bridge. Caddy refuses
// everything under /verify on the public hostname.
func (s *site) collectHandoff(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	value, ok := s.handoffs.take(r.URL.Query().Get("code"))
	if !ok {
		http.Error(w, "no such handoff", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"session": value})
}

// newConnectorSession is newSession for an agent. It never gets sudo, and its
// user agent names the connector so the sessions page can say what it is.
func newConnectorSession(db *sql.DB, r *http.Request, client string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	value := base64.RawURLEncoding.EncodeToString(raw)
	now := time.Now()
	c := requestContext(r)
	res, err := db.Exec(`
        INSERT INTO sessions (hash, created, last_seen, expires, sudo_at, ip, country, city, ua, cf_ray, method)
        VALUES (?, ?, ?, ?, 0, ?, ?, ?, ?, ?, ?)`,
		sessionHash(value), now.Unix(), now.Unix(), now.Add(connectorTTL).Unix(),
		c.IP, c.Country, c.City, connectorUA+client, c.Ray, methodConnector)
	if err != nil {
		return "", err
	}
	if id, err := res.LastInsertId(); err == nil {
		markDay(db, id, now)
	}
	return value, nil
}
