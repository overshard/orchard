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
	"time"
)

// admin drives Vaultwarden's admin API over the bridge, for the few settings
// the page changes. Caddy never routes /admin, so only this container reaches it.
type admin struct {
	base   string
	token  string
	config string
	http   *http.Client
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

// session signs in with the token and hands back the cookie. Vaultwarden marks
// it Secure, so it's carried by hand since the bridge is plain HTTP.
func (a *admin) session(ctx context.Context) (string, error) {
	if !a.Configured() {
		return "", errors.New("PASS_ADMIN_TOKEN is unset")
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
	for _, c := range resp.Cookies() {
		if c.Name == "VW_ADMIN" && c.Value != "" {
			return c.Value, nil
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
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Vaultwarden answered %d to %s", resp.StatusCode, path)
	}
	return nil
}

// SetSignups saves signups on or off live. Vaultwarden writes the whole saved
// config from what's sent, and signups is the only thing the page ever saves.
func (a *admin) SetSignups(ctx context.Context, on bool) error {
	return a.post(ctx, "/config", map[string]bool{"signups_allowed": on})
}

// SignOutEverywhere ends every session the user has, so each app has to sign
// in again with the master password.
func (a *admin) SignOutEverywhere(ctx context.Context, user string) error {
	return a.post(ctx, "/users/"+url.PathEscape(user)+"/deauth", struct{}{})
}
