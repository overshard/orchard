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

func (s *site) accountPage(w http.ResponseWriter, r *http.Request) {
	s.renderAccount(w, r, http.StatusOK, newSecret(), "", "")
}

func (s *site) renderAccount(w http.ResponseWriter, r *http.Request, code int, secret, problem, recovery string) {
	page, _ := s.chrome(r, "account")
	a, err := s.vault.Account()
	data := map[string]any{
		"Suite": page, "Account": a, "NoAccount": errors.Is(err, errNoAccount), "NoVault": err != nil && !errors.Is(err, errNoAccount),
		"Signups": s.admin.Signups(), "AdminOK": s.admin.Configured(),
		"Secret": secret, "Spaced": spaced(secret), "Problem": problem, "Recovery": spaced(recovery),
		"Done": doneText[r.URL.Query().Get("done")],
	}
	if err == nil && !a.TOTP {
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
		s.watch.send(r.Context(), ntfy.Message{Title: "pass: signups are on", Body: "Anyone can make an account until they're switched off.",
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
	secret := strings.ToUpper(strings.TrimSpace(r.FormValue("secret")))
	if key, err := base32.StdEncoding.DecodeString(secret); err != nil || len(key) != 20 {
		s.renderAccount(w, r, http.StatusBadRequest, newSecret(), "That setup code was damaged, so here's a new one to scan.", "")
		return
	}
	step, ok := checkTOTP(secret, r.FormValue("code"), time.Now())
	if !ok {
		s.renderAccount(w, r, http.StatusBadRequest, secret, "That code didn't match. Check the phone's clock and try the newest one.", "")
		return
	}
	recovery, err := s.vault.EnableTOTP(a.ID, secret, step, web.ClientIP(r))
	if err != nil {
		slog.Error("pass two step not saved", slog.Any("err", err))
		s.renderAccount(w, r, http.StatusInternalServerError, secret, "It couldn't be saved: "+err.Error(), "")
		return
	}
	s.renderAccount(w, r, http.StatusOK, "", "", recovery)
}

func (s *site) disableTOTP(w http.ResponseWriter, r *http.Request) {
	a, err := s.vault.Account()
	if err != nil || !a.TOTP {
		http.Redirect(w, r, "/account", http.StatusSeeOther)
		return
	}
	if _, ok := checkTOTP(a.Secret, r.FormValue("code"), time.Now()); !ok {
		s.renderAccount(w, r, http.StatusBadRequest, "", "That code didn't match, so two step login is still on.", "")
		return
	}
	if err := s.vault.DisableTOTP(a.ID, web.ClientIP(r)); err != nil {
		slog.Error("pass two step not removed", slog.Any("err", err))
		s.renderAccount(w, r, http.StatusInternalServerError, "", "It couldn't be turned off: "+err.Error(), "")
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
