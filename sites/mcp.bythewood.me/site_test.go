package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mcp.bythewood.me/web"
)

// fakeNet answers the outside world for a test: Claude's client metadata,
// auth's handoff and the tools over the bridge.
type fakeNet struct {
	revoked bool
}

func (f *fakeNet) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	switch {
	case r.URL.String() == knownClients[0]:
		_ = json.NewEncoder(rec).Encode(map[string]any{"client_id": knownClients[0], "client_name": "Claude",
			"redirect_uris": []string{"https://claude.ai/api/mcp/auth_callback"}})
	case r.URL.Host == "orchard-auth:8000" && r.URL.Path == "/verify/handoff":
		if r.URL.Query().Get("code") != "h1" {
			rec.WriteHeader(404)
			break
		}
		_ = json.NewEncoder(rec).Encode(map[string]string{"session": "sess-1"})
	case r.URL.Host == "orchard-auth:8000" && r.URL.Path == "/suite/merged":
		if c, err := r.Cookie("bw_session"); err != nil || c.Value != "sess-1" || f.revoked {
			rec.WriteHeader(401)
			break
		}
		_, _ = rec.WriteString(`{"tools":[{"tool":"status","level":"bad","count":1,"note":"1 of 2 properties down","items":[{"kind":"property","name":"pond.bythewood.me","href":"https://status.bythewood.me/abc","level":"bad","note":"down · 503"}]}]}`)
	default:
		rec.WriteHeader(404)
	}
	return rec.Result(), nil
}

func testSite(t *testing.T) (*site, *fakeNet) {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "mcp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	s, err := newSite(store, web.NewAuthenticatorAt("http://127.0.0.1:1/verify"))
	if err != nil {
		t.Fatal(err)
	}
	net := &fakeNet{}
	s.oauth.http = &http.Client{Transport: net}
	s.bridge = &http.Client{Transport: net, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return s, net
}

func do(t *testing.T, h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func rpc(t *testing.T, h http.Handler, token, method string, params map[string]any) (int, map[string]any) {
	t.Helper()
	if params == nil {
		params = map[string]any{}
	}
	params["_meta"] = map[string]any{"io.modelcontextprotocol/protocolVersion": modernVersion, "io.modelcontextprotocol/clientCapabilities": map[string]any{}}
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	r := httptest.NewRequest("POST", "/mcp", strings.NewReader(string(b)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Mcp-Method", method)
	r.Header.Set("MCP-Protocol-Version", modernVersion)
	if n, ok := params["name"].(string); ok {
		r.Header.Set("Mcp-Name", n)
	}
	rec := do(t, h, r)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestUnsignedRequestGetsAChallenge(t *testing.T) {
	s, _ := testSite(t)
	r := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{}`))
	rec := do(t, s.handler(), r)
	if rec.Code != 401 || !strings.Contains(rec.Header().Get("WWW-Authenticate"), "/.well-known/oauth-protected-resource/mcp") {
		t.Fatalf("got %d %q", rec.Code, rec.Header().Get("WWW-Authenticate"))
	}
}

func TestMetadata(t *testing.T) {
	s, _ := testSite(t)
	var pr, as map[string]any
	_ = json.Unmarshal(do(t, s.handler(), httptest.NewRequest("GET", "/.well-known/oauth-protected-resource/mcp", nil)).Body.Bytes(), &pr)
	_ = json.Unmarshal(do(t, s.handler(), httptest.NewRequest("GET", "/.well-known/oauth-authorization-server", nil)).Body.Bytes(), &as)
	if pr["resource"] != resourceURL || as["issuer"] != baseURL || as["client_id_metadata_document_supported"] != true {
		t.Fatalf("got %v and %v", pr, as)
	}
}

func TestSignInThroughAuthAndCallATool(t *testing.T) {
	s, net := testSite(t)
	h := s.handler()
	verifier := strings.Repeat("v", 50)
	sum := sha256.Sum256([]byte(verifier))
	callback := "https://claude.ai/api/mcp/auth_callback"
	q := url.Values{"response_type": {"code"}, "client_id": {knownClients[0]}, "redirect_uri": {callback}, "state": {"st"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}, "resource": {resourceURL}}

	rec := do(t, h, httptest.NewRequest("GET", "/authorize?"+q.Encode(), nil))
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if rec.Code != 302 || !strings.HasPrefix(loc.String(), connectURL) || loc.Query().Get("client") != "Claude" {
		t.Fatalf("authorize: %d %s %s", rec.Code, loc, rec.Body.String())
	}
	req := loc.Query().Get("req")
	if loc.Query().Get("started") == "" {
		t.Fatalf("authorize didn't say when it started: %s", loc)
	}
	flow := cookieNamed(rec.Result().Cookies(), flowCookie(req))
	if flow == nil || flow.Domain != "" || !flow.HttpOnly || !flow.Secure {
		t.Fatalf("authorize set no host-only flow cookie: %v", rec.Result().Cookies())
	}

	back := httptest.NewRequest("GET", "/oauth/back?req="+url.QueryEscape(req)+"&handoff=h1", nil)
	back.AddCookie(flow)
	rec = do(t, h, back)
	loc, _ = url.Parse(rec.Header().Get("Location"))
	if rec.Code != 302 || !strings.HasPrefix(loc.String(), callback) || loc.Query().Get("state") != "st" || loc.Query().Get("iss") != baseURL {
		t.Fatalf("back: %d %s", rec.Code, loc)
	}
	form := url.Values{"grant_type": {"authorization_code"}, "code": {loc.Query().Get("code")}, "client_id": {knownClients[0]},
		"redirect_uri": {callback}, "code_verifier": {verifier}, "resource": {resourceURL}}
	tr := httptest.NewRequest("POST", "/token", strings.NewReader(form.Encode()))
	tr.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = do(t, h, tr)
	var tok map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &tok)
	access, _ := tok["access_token"].(string)
	refresh, _ := tok["refresh_token"].(string)
	if rec.Code != 200 || access == "" || refresh == "" || tok["scope"] != "read gate:write" {
		t.Fatalf("token: %d %s", rec.Code, rec.Body.String())
	}

	code, out := rpc(t, h, access, "tools/list", nil)
	if code != 200 || len(out["result"].(map[string]any)["tools"].([]any)) != len(tools) {
		t.Fatalf("tools/list: %d %v", code, out)
	}
	code, out = rpc(t, h, access, "tools/call", map[string]any{"name": "orchard_brief", "arguments": map[string]any{}})
	text := out["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if code != 200 || !strings.Contains(text, "pond.bythewood.me **bad**") {
		t.Fatalf("orchard_brief: %d %v", code, out)
	}

	// A refresh token works once.
	rf := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {knownClients[0]}}
	for i, want := range []int{200, 400} {
		r := httptest.NewRequest("POST", "/token", strings.NewReader(rf.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if got := do(t, h, r).Code; got != want {
			t.Fatalf("refresh %d: got %d, want %d", i, got, want)
		}
	}

	// Revoking the session on auth ends the grant on the next call.
	net.revoked = true
	code, _ = rpc(t, h, access, "tools/call", map[string]any{"name": "orchard_brief", "arguments": map[string]any{}})
	if code != 401 {
		t.Fatalf("after revoke: %d", code)
	}
	if code, _ = rpc(t, h, access, "tools/list", nil); code != 401 {
		t.Fatalf("the grant outlived its session: %d", code)
	}
}

// A consent link started in somebody else's browser and forwarded can't be
// finished, even after allowing it on auth.
func TestBackRefusesAnotherBrowser(t *testing.T) {
	s, _ := testSite(t)
	h := s.handler()
	q := url.Values{"response_type": {"code"}, "client_id": {knownClients[0]}, "redirect_uri": {"https://claude.ai/api/mcp/auth_callback"},
		"code_challenge": {"abc"}, "code_challenge_method": {"S256"}}
	start := func() (string, *http.Cookie) {
		rec := do(t, h, httptest.NewRequest("GET", "/authorize?"+q.Encode(), nil))
		loc, _ := url.Parse(rec.Header().Get("Location"))
		req := loc.Query().Get("req")
		return req, cookieNamed(rec.Result().Cookies(), flowCookie(req))
	}

	req, _ := start()
	rec := do(t, h, httptest.NewRequest("GET", "/oauth/back?req="+url.QueryEscape(req)+"&handoff=h1", nil))
	if rec.Code != http.StatusForbidden || rec.Header().Get("Location") != "" {
		t.Fatalf("no cookie: %d %q", rec.Code, rec.Header().Get("Location"))
	}

	req, _ = start()
	_, other := start()
	r := httptest.NewRequest("GET", "/oauth/back?req="+url.QueryEscape(req)+"&handoff=h1", nil)
	r.AddCookie(&http.Cookie{Name: flowCookie(req), Value: other.Value})
	if rec := do(t, h, r); rec.Code != http.StatusForbidden {
		t.Fatalf("another flow's cookie: %d", rec.Code)
	}
}

func cookieNamed(cookies []*http.Cookie, name string) *http.Cookie {
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestLegacyInitialize(t *testing.T) {
	s, _ := testSite(t)
	g, _ := s.store.AddGrant(knownClients[1], "Claude Code", "sess-1", resourceURL, "read gate:write")
	access, _, _ := s.store.Issue(g)
	r := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"x","version":"1"}}}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+access)
	rec := do(t, s.handler(), r)
	body, _ := io.ReadAll(rec.Body)
	if rec.Code != 200 || !strings.Contains(string(body), `"protocolVersion":"2025-06-18"`) {
		t.Fatalf("got %d %s", rec.Code, body)
	}
}

func TestAuthorizeRefusesAStrangeClient(t *testing.T) {
	s, _ := testSite(t)
	q := url.Values{"response_type": {"code"}, "client_id": {"https://evil.example/meta"}, "redirect_uri": {"https://evil.example/cb"}}
	if rec := do(t, s.handler(), httptest.NewRequest("GET", "/authorize?"+q.Encode(), nil)); rec.Code != 400 {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestPages(t *testing.T) {
	s, _ := testSite(t)
	for path, want := range map[string]int{"/": 200, "/nope": 404, "/healthz": 200} {
		if got := do(t, s.handler(), httptest.NewRequest("GET", path, nil)).Code; got != want {
			t.Errorf("%s: got %d, want %d", path, got, want)
		}
	}
}

func TestCheckArgsHoldsCallsToTheSchema(t *testing.T) {
	schema := func(name string) map[string]any {
		tl, ok := toolNamed(name)
		if !ok {
			t.Fatalf("no tool %s", name)
		}
		return tl.Schema
	}
	cases := []struct {
		tool, args string
		ok         bool
	}{
		{"logs_overview", `{"range":"7d","source":"blog"}`, true},
		{"logs_overview", `{"range":"5y"}`, false},
		{"logs_overview", `{}`, true},
		{"logs_overview", ``, true},
		{"repo_readme", `{}`, false},
		{"repo_readme", `{"repo":""}`, false},
		{"repo_readme", `{"repo":"orchard","extra":1}`, false},
		{"cron_runs", `{"limit":200}`, true},
		{"cron_runs", `{"limit":500}`, false},
		{"cron_runs", `{"limit":1.5}`, false},
		{"cron_runs", `{"job_id":"7"}`, false},
		{"logs_search", `{"page":0}`, false},
		{"orchard_brief", `{"x":1}`, false},
		{"analytics_report", `{"property":"blog","days":"3"}`, false},
		{"analytics_report", `[1]`, false},
	}
	for _, c := range cases {
		err := checkArgs(schema(c.tool), json.RawMessage(c.args))
		if (err == nil) != c.ok {
			t.Errorf("%s %s: got %v, want ok=%v", c.tool, c.args, err, c.ok)
		}
	}
}

func TestRefreshRotatesOnceAndReuseEndsTheGrant(t *testing.T) {
	s, _ := testSite(t)
	g, _ := s.store.AddGrant(knownClients[1], "Claude Code", "sess-1", resourceURL, "read gate:write")
	_, refresh, _ := s.store.Issue(g)

	var wg sync.WaitGroup
	var mu sync.Mutex
	won := 0
	for range 8 {
		wg.Go(func() {
			if _, err := s.store.Spend(refresh, knownClients[1]); err == nil {
				mu.Lock()
				won++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if won != 1 {
		t.Fatalf("%d refreshes won with one token, want 1", won)
	}
	if _, err := s.store.Spend(refresh, knownClients[1]); !errors.Is(err, errNoToken) {
		t.Fatalf("a reuse inside the grace got %v, want it refused and the grant kept", err)
	}

	access, next, _ := s.store.Issue(g)
	if _, err := s.store.db.Exec(`UPDATE spent SET at = ?`, time.Now().Add(-reuseGrace-time.Second).Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.Spend(refresh, knownClients[1]); !errors.Is(err, errReused) {
		t.Fatalf("a late reuse got %v, want errReused", err)
	}
	if _, err := s.store.Lookup(access, "access"); err == nil {
		t.Fatal("the grant's access token still works after a reuse")
	}
	if _, err := s.store.Spend(next, knownClients[1]); err == nil {
		t.Fatal("the grant's newest refresh token still works after a reuse")
	}
}

func TestGateWritesNeedTheScope(t *testing.T) {
	s, _ := testSite(t)
	g, _ := s.store.AddGrant(knownClients[1], "Claude Code", "sess-1", resourceURL, "read")
	access, _, _ := s.store.Issue(g)
	for _, name := range []string{"gate_propose", "gate_edit"} {
		_, out := rpc(t, s.handler(), access, "tools/call", map[string]any{"name": name,
			"arguments": map[string]any{"name": "x", "why": "y", "when": "path is /x", "then": "block", "note": "z"}})
		res, _ := out["result"].(map[string]any)
		if res == nil || res["isError"] != true || !strings.Contains(fmt.Sprint(res["content"]), "without gate:write") {
			t.Errorf("%s with a read only grant: %v", name, out)
		}
	}
}

func TestScopeIsKeptInsideWhatWasOffered(t *testing.T) {
	for _, c := range []struct{ requested, allowed, want string }{
		{"", "", "read gate:write"},
		{"read", "", "read"},
		{"read", "read gate:write", "read"},
		{"read gate:write", "read", "read"},
		{"read gate:write", "read gate:write", "read gate:write"},
		{"gate:write", "gate:write", "read gate:write"},
		{"read admin", "read admin", "read"},
	} {
		if got := grantedScope(offeredScope(c.requested), c.allowed); got != c.want {
			t.Errorf("asked %q, auth allowed %q: got %q, want %q", c.requested, c.allowed, got, c.want)
		}
	}
}
