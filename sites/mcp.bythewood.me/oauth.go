package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// resourceURL is what the connector is added as, and every token is for it.
const resourceURL = baseURL + "/mcp"

// knownClients are the client metadata documents allowed to start a sign in.
// One owner and two clients, so a fixed list costs nothing and means /authorize
// never fetches a URL a stranger chose.
var knownClients = []string{
	"https://claude.ai/oauth/mcp-oauth-client-metadata",
	"https://claude.ai/oauth/claude-code-client-metadata",
}

const (
	connectURL  = "https://auth.bythewood.me/connect"
	handoffURL  = "http://orchard-auth:8000/verify/handoff"
	pendingTTL  = 10 * time.Minute
	authCodeTTL = time.Minute
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func protectedResource(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"resource":                 resourceURL,
		"resource_name":            "orchard",
		"authorization_servers":    []string{baseURL},
		"scopes_supported":         []string{"read"},
		"bearer_methods_supported": []string{"header"},
	})
}

func authServerMetadata(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"issuer":                                         baseURL,
		"authorization_endpoint":                         baseURL + "/authorize",
		"token_endpoint":                                 baseURL + "/token",
		"response_types_supported":                       []string{"code"},
		"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":               []string{"S256"},
		"token_endpoint_auth_methods_supported":          []string{"none"},
		"scopes_supported":                               []string{"read"},
		"client_id_metadata_document_supported":          true,
		"authorization_response_iss_parameter_supported": true,
	})
}

// clientDoc is the part of a client metadata document this reads.
type clientDoc struct {
	ClientID     string   `json:"client_id"`
	ClientName   string   `json:"client_name"`
	RedirectURIs []string `json:"redirect_uris"`
	fetched      time.Time
}

type oauth struct {
	store *Store
	http  *http.Client

	mu      sync.Mutex
	docs    map[string]clientDoc
	pending map[string]pendingAuth
	codes   map[string]authCode
}

type pendingAuth struct {
	ClientID, ClientName, RedirectURI, Challenge, State, Resource string
	expires                                                       time.Time
}

type authCode struct {
	Grant                            int64
	ClientID, RedirectURI, Challenge string
	expires                          time.Time
}

func newOAuth(store *Store) *oauth {
	return &oauth{store: store, http: &http.Client{Timeout: 5 * time.Second},
		docs: map[string]clientDoc{}, pending: map[string]pendingAuth{}, codes: map[string]authCode{}}
}

func (o *oauth) clientDoc(id string) (clientDoc, error) {
	if !slices.Contains(knownClients, id) {
		return clientDoc{}, fmt.Errorf("%s isn't a client this server knows", id)
	}
	o.mu.Lock()
	d, ok := o.docs[id]
	o.mu.Unlock()
	if ok && time.Since(d.fetched) < time.Hour {
		return d, nil
	}
	resp, err := o.http.Get(id)
	if err != nil {
		return clientDoc{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return clientDoc{}, fmt.Errorf("client metadata answered %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&d); err != nil {
		return clientDoc{}, err
	}
	if d.ClientID != id || len(d.RedirectURIs) == 0 {
		return clientDoc{}, fmt.Errorf("client metadata doesn't describe %s", id)
	}
	d.fetched = time.Now()
	o.mu.Lock()
	o.docs[id] = d
	o.mu.Unlock()
	return d, nil
}

// redirectAllowed matches a registered redirect exactly, except that a
// loopback one may come back on any port, since a CLI picks a free one.
func redirectAllowed(registered []string, got string) bool {
	if slices.Contains(registered, got) {
		return true
	}
	g, err := url.Parse(got)
	if err != nil || g.Scheme != "http" || !isLoopback(g.Hostname()) {
		return false
	}
	for _, reg := range registered {
		u, err := url.Parse(reg)
		if err == nil && u.Scheme == "http" && isLoopback(u.Hostname()) && u.Hostname() == g.Hostname() && u.Path == g.Path {
			return true
		}
	}
	return false
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (o *oauth) sweep(now time.Time) {
	for k, v := range o.pending {
		if now.After(v.expires) {
			delete(o.pending, k)
		}
	}
	for k, v := range o.codes {
		if now.After(v.expires) {
			delete(o.codes, k)
		}
	}
}

// authorize checks the request and sends the browser to auth, which asks you
// with a fresh code and hands back a session of the connector's own.
func (o *oauth) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	clientID, redirect := q.Get("client_id"), q.Get("redirect_uri")
	doc, err := o.clientDoc(clientID)
	if err != nil {
		slog.Info("authorize refused", slog.String("client", clientID), slog.Any("err", err))
		o.store.AddEvent("refused", clientID, err.Error())
		http.Error(w, "unknown client: "+err.Error(), http.StatusBadRequest)
		return
	}
	if !redirectAllowed(doc.RedirectURIs, redirect) {
		http.Error(w, "that redirect_uri isn't one the client registered", http.StatusBadRequest)
		return
	}
	// From here an error can go back to the client, since its redirect is real.
	fail := func(code, desc string) {
		v := url.Values{"error": {code}, "error_description": {desc}, "iss": {baseURL}}
		if s := q.Get("state"); s != "" {
			v.Set("state", s)
		}
		http.Redirect(w, r, withQuery(redirect, v), http.StatusFound)
	}
	switch {
	case q.Get("response_type") != "code":
		fail("unsupported_response_type", "only code")
		return
	case q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256":
		fail("invalid_request", "PKCE with S256 is required")
		return
	case q.Get("resource") != "" && strings.TrimSuffix(q.Get("resource"), "/") != resourceURL:
		fail("invalid_target", "the only resource here is "+resourceURL)
		return
	}
	id := randomToken()
	name := doc.ClientName
	if name == "" {
		name = "an agent"
	}
	o.mu.Lock()
	o.sweep(time.Now())
	o.pending[id] = pendingAuth{ClientID: clientID, ClientName: name, RedirectURI: redirect, Challenge: q.Get("code_challenge"),
		State: q.Get("state"), Resource: resourceURL, expires: time.Now().Add(pendingTTL)}
	o.mu.Unlock()
	cu, _ := url.Parse(clientID)
	ru, _ := url.Parse(redirect)
	host := cu.Host
	if ru != nil && ru.Hostname() != cu.Hostname() {
		host += ", back to " + ru.Hostname()
	}
	v := url.Values{"req": {id}, "client": {name}, "host": {host}, "back": {baseURL + "/oauth/back"}}
	http.Redirect(w, r, connectURL+"?"+v.Encode(), http.StatusFound)
}

func withQuery(base string, v url.Values) string {
	if strings.Contains(base, "?") {
		return base + "&" + v.Encode()
	}
	return base + "?" + v.Encode()
}

// back is where auth returns, with a one minute handoff for the session it
// made, or word that you said no.
func (o *oauth) back(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	o.mu.Lock()
	p, ok := o.pending[q.Get("req")]
	delete(o.pending, q.Get("req"))
	o.mu.Unlock()
	if !ok || time.Now().After(p.expires) {
		http.Error(w, "that sign in has expired, start it again from the client", http.StatusBadRequest)
		return
	}
	v := url.Values{"iss": {baseURL}}
	if p.State != "" {
		v.Set("state", p.State)
	}
	if q.Get("denied") != "" {
		o.store.AddEvent("denied", p.ClientName, "you said no on auth")
		v.Set("error", "access_denied")
		http.Redirect(w, r, withQuery(p.RedirectURI, v), http.StatusFound)
		return
	}
	session, err := o.collect(r, q.Get("handoff"))
	if err != nil {
		slog.Error("handoff failed", slog.Any("err", err))
		v.Set("error", "server_error")
		http.Redirect(w, r, withQuery(p.RedirectURI, v), http.StatusFound)
		return
	}
	grant, err := o.store.AddGrant(p.ClientID, p.ClientName, session, p.Resource)
	if err != nil {
		slog.Error("saving grant", slog.Any("err", err))
		v.Set("error", "server_error")
		http.Redirect(w, r, withQuery(p.RedirectURI, v), http.StatusFound)
		return
	}
	code := randomToken()
	o.mu.Lock()
	o.codes[code] = authCode{Grant: grant, ClientID: p.ClientID, RedirectURI: p.RedirectURI, Challenge: p.Challenge, expires: time.Now().Add(authCodeTTL)}
	o.mu.Unlock()
	slog.Info("connector allowed", slog.String("client", p.ClientName), slog.Int64("grant", grant))
	o.store.AddEvent("allowed", p.ClientName, p.ClientID)
	v.Set("code", code)
	http.Redirect(w, r, withQuery(p.RedirectURI, v), http.StatusFound)
}

func (o *oauth) collect(r *http.Request, handoff string) (string, error) {
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, handoffURL+"?code="+url.QueryEscape(handoff), nil)
	resp, err := o.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("auth answered %d", resp.StatusCode)
	}
	var got struct{ Session string }
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&got); err != nil || got.Session == "" {
		return "", fmt.Errorf("auth's handoff was empty")
	}
	return got.Session, nil
}

func tokenError(w http.ResponseWriter, code, desc string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": code, "error_description": desc})
}

func (o *oauth) token(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		tokenError(w, "invalid_request", "a form body please")
		return
	}
	if res := r.PostForm.Get("resource"); res != "" && strings.TrimSuffix(res, "/") != resourceURL {
		tokenError(w, "invalid_target", "the only resource here is "+resourceURL)
		return
	}
	var grant int64
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		o.mu.Lock()
		c, ok := o.codes[r.PostForm.Get("code")]
		delete(o.codes, r.PostForm.Get("code"))
		o.mu.Unlock()
		switch {
		case !ok || time.Now().After(c.expires):
			tokenError(w, "invalid_grant", "that code is used or expired")
			return
		case c.ClientID != r.PostForm.Get("client_id") || c.RedirectURI != r.PostForm.Get("redirect_uri"):
			tokenError(w, "invalid_grant", "client_id or redirect_uri doesn't match the code")
			return
		case !pkceMatches(r.PostForm.Get("code_verifier"), c.Challenge):
			tokenError(w, "invalid_grant", "code_verifier doesn't match")
			return
		}
		grant = c.Grant
	case "refresh_token":
		rt := r.PostForm.Get("refresh_token")
		g, err := o.store.Lookup(rt, "refresh")
		if err != nil || g.ClientID != r.PostForm.Get("client_id") {
			o.store.AddEvent("refresh refused", r.PostForm.Get("client_id"), "spent, expired or revoked")
			tokenError(w, "invalid_grant", "that refresh token is spent, expired or revoked")
			return
		}
		_ = o.store.Spend(rt)
		grant = g.ID
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported_grant_type"})
		return
	}
	access, refresh, err := o.store.Issue(grant)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server_error"})
		return
	}
	writeJSON(w, 200, map[string]any{
		"access_token":  access,
		"token_type":    "Bearer",
		"expires_in":    int(accessTTL / time.Second),
		"refresh_token": refresh,
		"scope":         "read",
	})
}

func pkceMatches(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	got := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(got), []byte(challenge)) == 1
}

// challenge is the 401 that starts a sign in, pointing at the metadata.
func challenge(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+baseURL+`/.well-known/oauth-protected-resource/mcp", scope="read"`)
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_token", "error_description": "sign in through auth.bythewood.me"})
}
