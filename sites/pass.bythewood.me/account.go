package main

import (
	"encoding/base32"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"pass.bythewood.me/ntfy"
	"pass.bythewood.me/web"
)

// noTOTP is an account that exists with no second step at all.
func (s *site) noTOTP() bool {
	a, err := s.vault.Account()
	return err == nil && !a.TOTP && a.Others == 0
}

var doneText = map[string]string{
	"signups-on":  "Signups are on",
	"signups-off": "Signups are off",
	"totp-off":    "Two step login is off",
	"signed-out":  "Every device is signed out",
}

// pendingCookie holds the secret being set up, so a reload shows the same QR
// code. It's host only and lasts as long as the setup does.
const pendingCookie = "pass_totp_pending"

func (s *site) accountPage(w http.ResponseWriter, r *http.Request) {
	secret := ""
	if c, err := r.Cookie(pendingCookie); err == nil && validSecret(c.Value) {
		secret = c.Value
	} else {
		secret = newSecret()
		http.SetCookie(w, &http.Cookie{Name: pendingCookie, Value: secret, Path: "/account", MaxAge: 900,
			HttpOnly: true, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https", SameSite: http.SameSiteStrictMode})
	}
	s.renderAccount(w, r, http.StatusOK, secret, "", "", false)
}

func validSecret(v string) bool {
	key, err := base32.StdEncoding.DecodeString(strings.ToUpper(strings.TrimSpace(v)))
	return err == nil && len(key) == 20
}

func (s *site) renderAccount(w http.ResponseWriter, r *http.Request, code int, secret, problem, recovery string, enabled bool) {
	page, _ := s.chrome(r, "account")
	a, err := s.vault.Account()
	data := map[string]any{
		"Suite": page, "Account": a, "NoAccount": errors.Is(err, errNoAccount), "NoVault": err != nil && !errors.Is(err, errNoAccount),
		"Signups": s.admin.Signups(), "AdminOK": s.admin.Configured(), "TwoStep": a.TOTP || a.Others > 0,
		"Secret": secret, "Spaced": spaced(secret), "Problem": problem, "Recovery": spaced(recovery), "Enabled": enabled,
		"Done": doneText[r.URL.Query().Get("done")], "Locked": err == nil && s.offLocked(a.ID, time.Now()),
	}
	if err == nil && !a.TOTP && secret != "" {
		data["QR"] = template.URL(qrPNG(otpURI(secret, a.Email)))
	}
	s.render(w, code, "account.html", data)
}

func (s *site) setSignups(w http.ResponseWriter, r *http.Request) {
	on := r.FormValue("on") == "true"
	if err := s.admin.SetSignups(r.Context(), on); err != nil {
		slog.Error("pass signups not changed", slog.Any("err", err))
		http.Error(w, "Vaultwarden didn't take the change: "+err.Error(), http.StatusBadGateway)
		return
	}
	word := "off"
	if on {
		word = "on"
	}
	slog.Warn("pass signups switched "+word, slog.String("ip", web.ClientIP(r)))
	if on {
		s.watch.send(r.Context(), ntfy.Message{Title: "pass: signups are on", Body: "Anyone can make an account until they're switched off, which happens by itself once one is made.",
			Priority: "high", Tags: "lock", Link: baseURL + "/account", Label: "Account"})
	}
	http.Redirect(w, r, "/account?done=signups-"+word, http.StatusSeeOther)
}

func (s *site) enableTOTP(w http.ResponseWriter, r *http.Request) {
	a, err := s.vault.Account()
	if err != nil {
		http.Error(w, "there's no account to turn it on for", http.StatusConflict)
		return
	}
	// Replacing a working authenticator goes through turning it off, which
	// asks for a current code.
	if a.TOTP {
		http.Redirect(w, r, "/account", http.StatusSeeOther)
		return
	}
	secret := strings.ToUpper(strings.TrimSpace(r.FormValue("secret")))
	if !validSecret(secret) {
		http.SetCookie(w, &http.Cookie{Name: pendingCookie, Path: "/account", MaxAge: -1})
		s.renderAccount(w, r, http.StatusBadRequest, "", "That setup code was damaged. Reload the page for a new one.", "", false)
		return
	}
	step, ok := checkTOTP(secret, r.FormValue("code"), time.Now())
	if !ok {
		s.renderAccount(w, r, http.StatusBadRequest, secret, "That code didn't match. Make sure the app scanned this page's QR code, and that the phone's clock is right.", "", false)
		return
	}
	recovery, err := s.vault.EnableTOTP(a.ID, secret, step, web.ClientIP(r))
	if err != nil {
		slog.Error("pass two step not saved", slog.Any("err", err))
		s.renderAccount(w, r, http.StatusInternalServerError, secret, "It couldn't be saved: "+err.Error(), "", false)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: pendingCookie, Path: "/account", MaxAge: -1})
	s.renderAccount(w, r, http.StatusOK, "", "", recovery, true)
}

const offMisses, offWindow = 5, 15 * time.Minute

// offLocked is the turn-off form after a run of wrong codes, since a stolen
// orchard session could otherwise guess at it. The misses are events in the
// database, so a restart doesn't clear them.
func (s *site) offLocked(user string, now time.Time) bool {
	n, err := s.vault.PageMisses(user, now.Add(-offWindow))
	return err == nil && n >= offMisses
}

func (s *site) disableTOTP(w http.ResponseWriter, r *http.Request) {
	a, err := s.vault.Account()
	if err != nil || !a.TOTP {
		http.Redirect(w, r, "/account", http.StatusSeeOther)
		return
	}
	now := time.Now()
	if s.offLocked(a.ID, now) {
		s.renderAccount(w, r, http.StatusTooManyRequests, "", "Too many wrong codes, so turning it off is locked for a few minutes.", "", false)
		return
	}
	// A code at or before the last step used is a replay, the same rule
	// Vaultwarden keeps at sign in.
	if step, ok := checkTOTP(a.Secret, r.FormValue("code"), now); !ok || step <= a.LastUsed {
		ip := web.ClientIP(r)
		if err := s.vault.FailedOff(a.ID, ip); err != nil {
			slog.Error("pass wrong code not recorded", slog.Any("err", err))
		}
		if s.offLocked(a.ID, now) {
			slog.Warn("pass two step turn off locked", slog.String("ip", ip))
		}
		s.renderAccount(w, r, http.StatusBadRequest, "", "That code didn't match, or was already used, so two step login is still on.", "", false)
		return
	}
	if err := s.vault.DisableTOTP(a.ID, web.ClientIP(r)); err != nil {
		slog.Error("pass two step not removed", slog.Any("err", err))
		s.renderAccount(w, r, http.StatusInternalServerError, "", "It couldn't be turned off: "+err.Error(), "", false)
		return
	}
	http.Redirect(w, r, "/account?done=totp-off", http.StatusSeeOther)
}

func (s *site) signOutEverywhere(w http.ResponseWriter, r *http.Request) {
	a, err := s.vault.Account()
	if err != nil {
		http.Redirect(w, r, "/account", http.StatusSeeOther)
		return
	}
	if err := s.admin.SignOutEverywhere(r.Context(), a.ID); err != nil {
		slog.Error("pass sign out failed", slog.Any("err", err))
		http.Error(w, "Vaultwarden didn't sign them out: "+err.Error(), http.StatusBadGateway)
		return
	}
	slog.Warn("pass signed every device out", slog.String("ip", web.ClientIP(r)))
	http.Redirect(w, r, "/account?done=signed-out", http.StatusSeeOther)
}
