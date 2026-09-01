package main

import (
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// PageData is what every template gets. There is no analytics snippet here:
// site is a login form, and pointing a third script at the page somebody types
// a one time code into is the wrong trade even when the third script is ours.
type PageData struct {
	Title       string
	Description string
	Path        string
	Canonical   string
	Staging     bool
	Year        int
	BaseURL     string
	SourceURL   string
	SiteName    string
	AuthorName  string
	Script      string
	Styles      []string
	PageScript  string
	PageStyles  []string
	Analytics   bool
	AnalyticsID string

	Authenticated bool
	Username      string

	// Set by whichever handler needs them.
	Next      string
	Error     string
	Notice    string
	InSudo    bool
	Sessions  []Session
	Events    []Event
	Remaining int
	NtfyHost  string
	NtfyUser  string
	NtfyTopic string
	NewCodes  []string
	User      User
}

func (s *site) page(r *http.Request, title, description string) PageData {
	_, err := lookupSession(s.db, r)
	name := ""
	if u, uerr := loadUser(s.db); uerr == nil {
		name = u.Username
	}
	return PageData{
		Title:         title,
		Description:   description,
		Path:          r.URL.Path,
		Canonical:     baseURL + r.URL.Path,
		Staging:       Staging,
		Authenticated: err == nil,
		Username:      name,
		Year:          time.Now().Year(),
		BaseURL:       baseURL,
		SourceURL:     sourceURL,
		SiteName:      siteName,
		AuthorName:    authorName,
		Script:        s.baseScript,
		Styles:        s.baseStyles,
		PageScript:    s.pagesScript,
		PageStyles:    s.pagesStyles,
		Analytics:     !Staging,
		AnalyticsID:   analyticsID,
	}
}

func (s *site) loginError(w http.ResponseWriter, r *http.Request, next, msg string, status int) {
	data := s.page(r, "Sign in", "")
	data.Next = next
	data.Error = msg
	s.renderer.Render(w, status, "login.html", data)
}

func (s *site) recoveryError(w http.ResponseWriter, r *http.Request, next, msg string, status int) {
	data := s.page(r, "Recovery code", "")
	data.Next = next
	data.Error = msg
	s.renderer.Render(w, status, "recovery.html", data)
}

func (s *site) codePage(w http.ResponseWriter, r *http.Request, next, msg string, status int) {
	data := s.page(r, "Enter your code", "")
	data.Next = next
	if status >= 400 {
		data.Error = msg
	} else {
		data.Notice = msg
	}
	s.renderer.Render(w, status, "code.html", data)
}

// uninitialized is what a fresh install serves until `make auth-init` has run.
// It names the command rather than offering to create the account over HTTP,
// which would let whoever found the hostname first claim it.
func (s *site) uninitialized(w http.ResponseWriter, r *http.Request) {
	data := s.page(r, "Not set up", "This installation has no account yet.")
	s.renderer.Render(w, http.StatusServiceUnavailable, "uninitialized.html", data)
}

func (s *site) fail(w http.ResponseWriter, r *http.Request, doing string, err error) {
	slog.Error("auth: "+doing+" failed",
		slog.String("component", "auth"),
		slog.Any("err", err))
	data := s.page(r, "Something went wrong", "That did not work.")
	data.Error = "Something went wrong " + doing + "."
	s.renderer.Render(w, http.StatusInternalServerError, "error.html", data)
}

// requireAuth gates everything the operator alone may reach, carrying the
// original destination so a bookmark survives the login.
func (s *site) requireAuth(next func(http.ResponseWriter, *http.Request, live)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l, err := lookupSession(s.db, r)
		if err != nil {
			http.Redirect(w, r, "/login?next="+returnTo(r), http.StatusSeeOther)
			return
		}
		next(w, r, l)
	}
}

// requireSudo additionally wants a login inside the last few minutes, so a
// cookie stolen later cannot rotate the credentials that would keep it alive.
func (s *site) requireSudo(next func(http.ResponseWriter, *http.Request, live)) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request, l live) {
		if !l.inSudo() {
			http.Redirect(w, r, "/login?next="+returnTo(r), http.StatusSeeOther)
			return
		}
		next(w, r, l)
	})
}

// returnTo is the current URL, escaped for the ?next= that survives a login.
func returnTo(r *http.Request) string {
	return template.URLQueryEscaper(r.URL.RequestURI())
}

func (s *site) account(w http.ResponseWriter, r *http.Request, l live) {
	user, err := loadUser(s.db)
	if err != nil {
		s.uninitialized(w, r)
		return
	}
	remaining, err := countRecoveryCodes(s.db)
	if err != nil {
		s.fail(w, r, "counting recovery codes", err)
		return
	}

	data := s.page(r, "Account", "")
	data.User = user
	data.Remaining = remaining
	data.InSudo = l.inSudo()
	s.renderer.Render(w, http.StatusOK, "account.html", data)
}

func (s *site) sessions(w http.ResponseWriter, r *http.Request, l live) {
	list, err := listSessions(s.db, l.Hash)
	if err != nil {
		s.fail(w, r, "listing sessions", err)
		return
	}
	data := s.page(r, "Sessions", "Where this account is signed in.")
	data.Sessions = list
	s.renderer.Render(w, http.StatusOK, "sessions.html", data)
}

func (s *site) revoke(w http.ResponseWriter, r *http.Request, l live) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	id, err := strconv.ParseInt(r.PostFormValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := revokeSession(s.db, id); err != nil {
		s.fail(w, r, "revoking a session", err)
		return
	}
	audit(s.db, r, evSessionRevoked, "one")

	// Revoking your own is a logout, and leaving the cookie in place would
	// leave the browser holding one this site no longer honours.
	if id == l.ID {
		clearSessionCookie(w)
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/sessions", http.StatusSeeOther)
}

func (s *site) revokeOthers(w http.ResponseWriter, r *http.Request, l live) {
	n, err := revokeOthers(s.db, l.ID)
	if err != nil {
		s.fail(w, r, "revoking sessions", err)
		return
	}
	audit(s.db, r, evSessionRevoked, strconv.FormatInt(n, 10)+" others")
	http.Redirect(w, r, "/sessions", http.StatusSeeOther)
}

// security holds the credentials half. The ntfy password is not on it and
// cannot be: ntfy stores it hashed and will not hand one back, and the only
// ways around that are keeping a second reversible copy here or giving this
// container the Docker socket. `make ntfy-passwd` is how it is changed.
func (s *site) security(w http.ResponseWriter, r *http.Request, l live) {
	user, err := loadUser(s.db)
	if err != nil {
		s.uninitialized(w, r)
		return
	}
	remaining, err := countRecoveryCodes(s.db)
	if err != nil {
		s.fail(w, r, "counting recovery codes", err)
		return
	}

	data := s.page(r, "Security", "Recovery codes and how the phone connects.")
	data.User = user
	data.Remaining = remaining
	data.InSudo = l.inSudo()
	data.NtfyHost = ntfyPublicURL
	data.NtfyUser = user.NtfyAccount
	data.NtfyTopic = ntfyTopic
	s.renderer.Render(w, http.StatusOK, "security.html", data)
}

func (s *site) rotateRecovery(w http.ResponseWriter, r *http.Request, l live) {
	codes, err := regenerateRecoveryCodes(s.db)
	if err != nil {
		s.fail(w, r, "generating recovery codes", err)
		return
	}
	audit(s.db, r, evRecoveryRotated, "")

	user, _ := loadUser(s.db)
	data := s.page(r, "Recovery codes", "Written down once and never shown again.")
	data.User = user
	data.NewCodes = codes
	data.Remaining = len(codes)
	// Rendered rather than redirected, because a redirect would need the codes
	// to survive somewhere between two requests and there is nowhere safe to
	// put them.
	s.renderer.Render(w, http.StatusOK, "codes.html", data)
}

func (s *site) changeUsername(w http.ResponseWriter, r *http.Request, l live) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name := r.PostFormValue("username")
	if err := setUsername(s.db, name); err != nil {
		user, _ := loadUser(s.db)
		remaining, _ := countRecoveryCodes(s.db)
		data := s.page(r, "Account", "")
		data.User = user
		data.Remaining = remaining
		data.InSudo = true
		data.Error = err.Error()
		s.renderer.Render(w, http.StatusBadRequest, "account.html", data)
		return
	}
	audit(s.db, r, evUsernameChanged, "")
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}

func (s *site) activity(w http.ResponseWriter, r *http.Request, l live) {
	events, err := recentEvents(s.db, 200)
	if err != nil {
		s.fail(w, r, "reading the activity log", err)
		return
	}
	data := s.page(r, "Activity", "Every authentication event, newest first.")
	data.Events = events
	s.renderer.Render(w, http.StatusOK, "activity.html", data)
}

// verify is what the other sites call over the bridge to turn a cookie into an
// answer. Caddy refuses this path on the public hostname, so reaching it really
// does mean being inside the network.
//
// It returns the username and nothing else. A site behind this needs to know
// somebody is signed in, and there is one somebody.
func (s *site) verify(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")

	l, err := lookupSession(s.db, r)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false})
		return
	}
	user, err := loadUser(s.db)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":       true,
		"username": user.Username,
		"sudo":     l.inSudo(),
	})
}

func (s *site) notFound(w http.ResponseWriter, r *http.Request) {
	data := s.page(r, "404", "That page does not exist.")
	s.renderer.Render(w, http.StatusNotFound, "notfound.html", data)
}
