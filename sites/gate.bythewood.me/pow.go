package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// A challenged visitor's page hashes a signed seed with counters until the
// SHA-256 starts with powZeros zero hex digits, then posts the answer to
// /__gate/solve on its own host for a cookie the challenge rules accept.
const (
	powZeros   = 4
	seedFor    = 10 * time.Minute
	passFor    = 7 * 24 * time.Hour
	passCookie = "gate_pass"
)

func (e *engine) sign(parts ...string) string {
	m := hmac.New(sha256.New, e.secret)
	m.Write([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(m.Sum(nil))[:32]
}

func (e *engine) seed(host string, now time.Time) string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	ts, nonce := strconv.FormatInt(now.Unix(), 36), hex.EncodeToString(b)
	return ts + "." + nonce + "." + e.sign("seed", ts, nonce, host)
}

func (e *engine) seedOK(seed, host string, now time.Time) bool {
	parts := strings.Split(seed, ".")
	if len(parts) != 3 {
		return false
	}
	ts, err := strconv.ParseInt(parts[0], 36, 64)
	if err != nil {
		return false
	}
	at := time.Unix(ts, 0)
	if now.Sub(at) > seedFor || at.After(now.Add(time.Minute)) {
		return false
	}
	return hmac.Equal([]byte(parts[2]), []byte(e.sign("seed", parts[0], parts[1], host)))
}

func worked(seed, nonce string) bool {
	if nonce == "" || len(nonce) > 16 {
		return false
	}
	sum := sha256.Sum256([]byte(seed + nonce))
	return strings.HasPrefix(hex.EncodeToString(sum[:]), strings.Repeat("0", powZeros))
}

// The pass is tied to the host and the browser's user agent, so a solved
// cookie can't be lifted and handed to a farm of different clients as is.
func (e *engine) pass(host, ua string, until time.Time) string {
	exp := strconv.FormatInt(until.Unix(), 36)
	return exp + "." + e.sign("pass", host, exp, ua)
}

func (e *engine) passed(r *http.Request, q *req) bool {
	c, err := r.Cookie(passCookie)
	if err != nil {
		return false
	}
	exp, sig, ok := strings.Cut(c.Value, ".")
	if !ok {
		return false
	}
	n, err := strconv.ParseInt(exp, 36, 64)
	if err != nil || time.Now().Unix() > n {
		return false
	}
	return hmac.Equal([]byte(sig), []byte(e.sign("pass", strings.ToLower(q.Host), exp, q.UA)))
}

var challengeTpl *template.Template

func (e *engine) challenge(w http.ResponseWriter, r *http.Request, q *req) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'unsafe-inline'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	w.Header().Set("X-Robots-Tag", "noindex")
	w.WriteHeader(http.StatusForbidden)
	back := q.Path
	if q.Query != "" {
		back += "?" + q.Query
	}
	if q.Method != http.MethodGet && q.Method != http.MethodHead {
		back = "/"
	}
	err := challengeTpl.ExecuteTemplate(w, "challenge.html", map[string]any{
		"Seed": e.seed(strings.ToLower(q.Host), time.Now()), "Zeros": powZeros, "Back": back, "Host": q.Host,
	})
	if err != nil {
		slog.Error("render challenge", slog.Any("err", err))
	}
}

// solve is reached on the visitor's own host through Caddy's /__gate/ route.
func (e *engine) solve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	host := strings.ToLower(r.Host)
	if h, _, ok := strings.Cut(host, ":"); ok {
		host = h
	}
	seed, nonce := r.PostFormValue("seed"), r.PostFormValue("nonce")
	now := time.Now()
	if !e.seedOK(seed, host, now) || !worked(seed, nonce) {
		plain(w, http.StatusBadRequest, "That didn't check out. Go back and let the page finish.")
		return
	}
	until := now.Add(passFor)
	http.SetCookie(w, &http.Cookie{Name: passCookie, Value: e.pass(host, r.Header.Get("User-Agent"), until), Path: "/",
		Expires: until, MaxAge: int(passFor / time.Second), HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	back := r.PostFormValue("back")
	if !strings.HasPrefix(back, "/") || strings.HasPrefix(back, "//") || strings.HasPrefix(back, "/\\") {
		back = "/"
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func powScript(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(powJS)
}

var powJS = []byte(`(async () => {
  const f = document.getElementById("pow");
  const bar = document.getElementById("bar");
  const seed = f.elements.seed.value;
  const zeros = Number(f.dataset.zeros);
  const enc = new TextEncoder();
  const start = performance.now();
  for (let n = 0; ; n++) {
    const sum = new Uint8Array(await crypto.subtle.digest("SHA-256", enc.encode(seed + n)));
    let ok = true;
    for (let i = 0; i < zeros; i++) {
      const nib = i % 2 === 0 ? sum[i >> 1] >> 4 : sum[i >> 1] & 15;
      if (nib !== 0) { ok = false; break; }
    }
    if (n % 2000 === 0) bar.style.transform = "scaleX(" + Math.min(0.95, n / 90000) + ")";
    if (ok) {
      f.elements.nonce.value = String(n);
      document.getElementById("took").textContent = n.toLocaleString() + " hashes, " + ((performance.now() - start) / 1000).toFixed(1) + "s";
      bar.style.transform = "scaleX(1)";
      f.submit();
      return;
    }
  }
})();
`)
