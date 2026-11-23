package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// admin drives Vaultwarden's admin API over the bridge, for the few settings
// the page changes. Caddy never routes /admin, so only this container reaches it.
type admin struct {
	base   string
	token  string
	config string
	http   *http.Client

	mu  sync.Mutex
	jwt string
	exp time.Time
}

func newAdmin(base, token, vaultPath string) *admin {
	return &admin{base: base, token: token, config: filepath.Join(filepath.Dir(vaultPath), "config.json"),
		// Never follow the login's redirect, the cookie is on that response.
		http: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (a *admin) Configured() bool { return a.token != "" }

// Signups reads the setting Vaultwarden saved last. A missing file means it
// never changed from the default compose sets, which is off.
func (a *admin) Signups() bool {
	b, err := os.ReadFile(a.config)
	if err != nil {
		return false
	}
	var c struct {
		Signups *bool `json:"signups_allowed"`
	}
	if json.Unmarshal(b, &c) != nil || c.Signups == nil {
		return false
	}
	return *c.Signups
}

// session signs in with the token and hands back the cookie, kept for a while
// since Vaultwarden allows few admin sign ins a minute. It marks the cookie
// Secure, so it's carried by hand over the plain HTTP bridge.
func (a *admin) session(ctx context.Context) (string, error) {
	if !a.Configured() {
		return "", errors.New("PASS_ADMIN_TOKEN is unset")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.jwt != "" && time.Now().Before(a.exp) {
		return a.jwt, nil
	}
	form := url.Values{"token": {a.token}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.base+"/admin", strings.NewReader(form))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.http.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return "", errors.New("Vaultwarden has had too many admin sign ins, try again in a few minutes")
	}
	for _, c := range resp.Cookies() {
		if c.Name == "VW_ADMIN" && c.Value != "" {
			// Vaultwarden's admin session lasts 20 minutes by default.
			a.jwt, a.exp = c.Value, time.Now().Add(15*time.Minute)
			return a.jwt, nil
		}
	}
	return "", fmt.Errorf("Vaultwarden refused the admin token, answered %d", resp.StatusCode)
}

func (a *admin) post(ctx context.Context, path string, body any) error {
	jwt, err := a.session(ctx)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.base+"/admin"+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "VW_ADMIN", Value: jwt})
	resp, err := a.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		a.mu.Lock()
		a.jwt = ""
		a.mu.Unlock()
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Vaultwarden answered %d to %s", resp.StatusCode, path)
	}
	return nil
}

// SetSignups saves signups on or off live. Vaultwarden replaces the whole saved
// config with what's sent, so whatever else is in it goes back with it.
func (a *admin) SetSignups(ctx context.Context, on bool) error {
	cfg := map[string]any{}
	if b, err := os.ReadFile(a.config); err == nil {
		if err := json.Unmarshal(b, &cfg); err != nil {
			return fmt.Errorf("config.json didn't parse, so it was left alone: %w", err)
		}
	}
	cfg["signups_allowed"] = on
	return a.post(ctx, "/config", cfg)
}

// CloseSignups switches signups off if they're on, and says whether it did.
func (a *admin) CloseSignups(ctx context.Context) (bool, error) {
	if !a.Signups() || !a.Configured() {
		return false, nil
	}
	return true, a.SetSignups(ctx, false)
}

// SignOutEverywhere ends every session the user has, so each app has to sign
// in again with the master password.
func (a *admin) SignOutEverywhere(ctx context.Context, user string) error {
	return a.post(ctx, "/users/"+url.PathEscape(user)+"/deauth", struct{}{})
}
