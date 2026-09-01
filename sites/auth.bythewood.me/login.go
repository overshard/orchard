package main

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// defaultNext is where a login with nowhere particular to go ends up.
const defaultNext = "/account"

// safeNext keeps the post-login redirect on this platform. A relative path is
// fine, and so is any https bythewood.me host, which is what lets analytics
// send somebody here and get them back afterwards.
//
// "starts with /" is not enough on its own: "//evil.example" is
// protocol-relative and "/\evil.example" is followed off-site by some browsers.
func safeNext(next string) string {
	if next == "" {
		return defaultNext
	}
	if strings.HasPrefix(next, "/") {
		if strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") {
			return defaultNext
		}
		return next
	}

	u, err := url.Parse(next)
	if err != nil || u.Scheme != "https" {
		return defaultNext
	}
	host := u.Hostname()
	if host == "bythewood.me" || strings.HasSuffix(host, ".bythewood.me") {
		return next
	}
	return defaultNext
}

// landing is the public page. It says what this is and nothing about who runs
// it that the repository does not already say.
func (s *site) landing(w http.ResponseWriter, r *http.Request) {
	if _, err := lookupSession(s.db, r); err == nil {
		http.Redirect(w, r, defaultNext, http.StatusSeeOther)
		return
	}
	data := s.page(r, "Sign in", "")
	s.renderer.Render(w, http.StatusOK, "home.html", data)
}

func (s *site) loginForm(w http.ResponseWriter, r *http.Request) {
	if _, err := lookupSession(s.db, r); err == nil {
		http.Redirect(w, r, safeNext(r.URL.Query().Get("next")), http.StatusSeeOther)
		return
	}
	data := s.page(r, "Sign in", "")
	data.Next = r.URL.Query().Get("next")
	s.renderer.Render(w, http.StatusOK, "login.html", data)
}

// loginSubmit takes the username and pushes a code.
//
// The reply is the same page whether or not the username was right, because
// this site is open source and the username is in it, so pretending otherwise
// would be theatre. What actually bounds the abuse is the per account ceiling
// and the one-outstanding-code rule, both in otp.go.
func (s *site) loginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	next := r.PostFormValue("next")

	if !loginBucket.take() {
		audit(s.db, r, evRateLimited, "login")
		s.loginError(w, r, next, "Too many attempts just now. Wait a moment and try again.", http.StatusTooManyRequests)
		return
	}

	user, err := loadUser(s.db)
	if err != nil {
		s.uninitialized(w, r)
		return
	}

	audit(s.db, r, evCodeRequested, "")

	supplied := strings.ToLower(strings.TrimSpace(r.PostFormValue("username")))
	if supplied != user.Username {
		time.Sleep(failedDelay)
		s.loginError(w, r, next, "No code could be sent for that name.", http.StatusUnauthorized)
		return
	}

	if !s.notifier.configured() {
		s.loginError(w, r, next,
			"Codes cannot be delivered right now. Use a recovery code instead.", http.StatusServiceUnavailable)
		return
	}

	code, browser, err := startLogin(s.db, r)
	switch {
	case errors.Is(err, errOutstanding):
		// Not an error to the person holding the phone: a code is already on
		// its way, so send them to the form to type it rather than sending a
		// second one.
		s.codePage(w, r, next, "A code was already sent. Check your phone.", http.StatusOK)
		return
	case errors.Is(err, errCeiling):
		audit(s.db, r, evCeilingHit, "")
		s.loginError(w, r, next,
			"Too many codes have been sent in the last hour. Use a recovery code.", http.StatusTooManyRequests)
		return
	case err != nil:
		s.fail(w, r, "starting a login", err)
		return
	}

	if err := s.notifier.publish(r.Context(), codeMessage(code, requestContext(r))); err != nil {
		s.fail(w, r, "publishing a login code", err)
		return
	}
	// Only after ntfy accepted it, so a failed publish does not spend one of
	// the hour's five.
	_ = recordSend(s.db)
	audit(s.db, r, evCodeSent, "")

	writePendingCookie(w, browser)
	s.codePage(w, r, next, "", http.StatusOK)
}

// codeSubmit checks the six digits against the outstanding row for this browser.
func (s *site) codeSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	next := r.PostFormValue("next")

	if !codeBucket.take() {
		audit(s.db, r, evRateLimited, "code")
		s.codePage(w, r, next, "Too many attempts just now.", http.StatusTooManyRequests)
		return
	}

	code := strings.TrimSpace(r.PostFormValue("code"))
	err := finishLogin(s.db, r, code)
	switch {
	case errors.Is(err, errNoPending):
		audit(s.db, r, evCodeExpired, "")
		clearPendingCookie(w)
		s.loginError(w, r, next, "That code has expired. Start again.", http.StatusUnauthorized)
		return
	case errors.Is(err, errBadCode):
		time.Sleep(failedDelay)
		audit(s.db, r, evCodeFailed, "")
		s.codePage(w, r, next, "That code is not right.", http.StatusUnauthorized)
		return
	case err != nil:
		s.fail(w, r, "checking a login code", err)
		return
	}

	clearPendingCookie(w)
	s.openSession(w, r, next, "a code")
}

// recoveryForm and recoverySubmit are the break-glass, and they exist because
// ntfy is a container behind the same tunnel as the sites this gates. A bad
// tunnel would otherwise lock the operator out of logging and status at exactly
// the moment they are needed.
func (s *site) recoveryForm(w http.ResponseWriter, r *http.Request) {
	data := s.page(r, "Recovery code", "")
	data.Next = r.URL.Query().Get("next")
	s.renderer.Render(w, http.StatusOK, "recovery.html", data)
}

func (s *site) recoverySubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	next := r.PostFormValue("next")

	if !loginBucket.take() {
		audit(s.db, r, evRateLimited, "recovery")
		s.recoveryError(w, r, next, "Too many attempts just now.", http.StatusTooManyRequests)
		return
	}

	remaining, err := useRecoveryCode(s.db, r.PostFormValue("code"))
	if errors.Is(err, errBadRecovery) {
		time.Sleep(failedDelay)
		audit(s.db, r, evRecoveryFailed, "")
		s.recoveryError(w, r, next, "That recovery code is not valid.", http.StatusUnauthorized)
		return
	}
	if err != nil {
		s.fail(w, r, "spending a recovery code", err)
		return
	}

	audit(s.db, r, evRecoveryUsed, "")
	// Queues on ntfy and lands when it comes back, which is the case a recovery
	// code is for.
	go s.notifier.notify(recoveryMessage(requestContext(r), remaining))

	s.openSession(w, r, next, "a recovery code")
}

// openSession is the one place a session is created, so the alert and the audit
// record cannot drift apart from the cookie.
func (s *site) openSession(w http.ResponseWriter, r *http.Request, next, how string) {
	value, err := newSession(s.db, r)
	if err != nil {
		s.fail(w, r, "opening a session", err)
		return
	}
	writeSessionCookie(w, value)
	audit(s.db, r, evLogin, how)
	go s.notifier.notify(sessionMessage(requestContext(r), how))

	http.Redirect(w, r, safeNext(next), http.StatusSeeOther)
}

func (s *site) logout(w http.ResponseWriter, r *http.Request) {
	if l, err := lookupSession(s.db, r); err == nil {
		_ = revokeSession(s.db, l.ID)
	}
	clearSessionCookie(w)
	audit(s.db, r, evLogout, "")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// codeForm is the page somebody lands on if they reload the code step, or come
// back to the tab after reading the notification.
func (s *site) codeForm(w http.ResponseWriter, r *http.Request) {
	if _, err := lookupSession(s.db, r); err == nil {
		http.Redirect(w, r, defaultNext, http.StatusSeeOther)
		return
	}
	if pendingBrowser(r) == "" {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	s.codePage(w, r, r.URL.Query().Get("next"), "", http.StatusOK)
}
