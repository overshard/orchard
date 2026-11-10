package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"pond.bythewood.me/web"
)

const keyCookie = "pond_key"

// Host only, so no other bythewood.me site is ever sent the key, and set here
// instead of by script, since Safari wipes what a script stores after a week
// away. Browsers cap a cookie's life at 400 days and every visit renews it.
func setKeyCookie(w http.ResponseWriter, key string) {
	http.SetCookie(w, &http.Cookie{
		Name:     keyCookie,
		Value:    key,
		Path:     "/",
		MaxAge:   400 * 86400,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

// pondOf is the pond the request's cookie opens, with its key, or errNoPond.
func (s *site) pondOf(r *http.Request) (int64, string, error) {
	c, err := r.Cookie(keyCookie)
	if err != nil {
		return 0, "", errNoPond
	}
	key, ok := normalizeKey(c.Value)
	if !ok {
		return 0, "", errNoPond
	}
	id, err := s.store.idOf(key)
	return id, key, err
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// fail turns a store error into the status the page acts on.
func fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, errNoPond):
		writeError(w, http.StatusUnauthorized, "no pond")
	case errors.Is(err, errFull):
		writeError(w, http.StatusTooManyRequests, err.Error())
	case errors.Is(err, errPoor), errors.Is(err, errNotYet), errors.Is(err, errNotHeld), errors.Is(err, errBadRun):
		writeError(w, http.StatusConflict, err.Error())
	default:
		slog.Error("pond", slog.String("path", r.URL.Path), slog.Any("err", err))
		writeError(w, http.StatusInternalServerError, "something went wrong in the pond")
	}
}

// post fences every route that changes anything. Another bythewood.me page is
// same site and would carry the cookie, so the check is same origin, and a
// form can post text/plain across origins without asking where JSON cannot.
func post(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if from := r.Header.Get("Sec-Fetch-Site"); from != "" && from != "same-origin" {
			writeError(w, http.StatusForbidden, "cross origin")
			return
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			writeError(w, http.StatusUnsupportedMediaType, "json only")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		next(w, r)
	}
}

func readJSON(w http.ResponseWriter, r *http.Request, into any) bool {
	if err := json.NewDecoder(r.Body).Decode(into); err != nil {
		writeError(w, http.StatusBadRequest, "bad json")
		return false
	}
	return true
}

// played wraps a move that needs a pond and answers with whatever it returns.
func (s *site) played(move func(id int64, r *http.Request, w http.ResponseWriter) (any, error)) http.HandlerFunc {
	return post(func(w http.ResponseWriter, r *http.Request) {
		id, _, err := s.pondOf(r)
		if err != nil {
			fail(w, r, err)
			return
		}
		out, err := move(id, r, w)
		if err != nil {
			fail(w, r, err)
			return
		}
		if out != nil {
			writeJSON(w, http.StatusOK, out)
		}
	})
}

func (s *site) apiPond(w http.ResponseWriter, r *http.Request) {
	id, _, err := s.pondOf(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	v, err := s.store.view(id)
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// apiGreet is where an egg gets laid, on the first visit with no cookie. It
// is a POST from the page's own script, so a crawler fetching the page lays
// nothing.
func (s *site) apiGreet(w http.ResponseWriter, r *http.Request) {
	id, key, err := s.pondOf(r)
	first := errors.Is(err, errNoPond)
	if first {
		key, id, err = s.store.create()
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	g, err := s.store.greet(id, first)
	if err != nil {
		fail(w, r, err)
		return
	}
	setKeyCookie(w, key)
	writeJSON(w, http.StatusOK, g)
}

func (s *site) apiSpin(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	var in struct {
		Paid bool `json:"paid"`
	}
	if !readJSON(w, r, &in) {
		return nil, nil
	}
	return s.store.spin(id, in.Paid)
}

func (s *site) apiLog(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	var in struct {
		Pick int `json:"pick"`
	}
	if !readJSON(w, r, &in) {
		return nil, nil
	}
	if in.Pick < 0 || in.Pick > 2 {
		writeError(w, http.StatusBadRequest, "there are three logs")
		return nil, nil
	}
	return s.store.turnLog(id, in.Pick)
}

func (s *site) apiBox(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	return s.store.box(id)
}

func (s *site) apiCrack(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	return s.store.crack(id)
}

func readHat(w http.ResponseWriter, r *http.Request, bare bool) (hat, bool) {
	var h hat
	if !readJSON(w, r, &h) {
		return h, false
	}
	if !h.valid() && !(bare && h.Shape == 0) {
		writeError(w, http.StatusBadRequest, "no such hat")
		return h, false
	}
	return h, true
}

func (s *site) apiWear(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	h, ok := readHat(w, r, true)
	if !ok {
		return nil, nil
	}
	return s.store.wear(id, h)
}

func (s *site) apiSell(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	h, ok := readHat(w, r, false)
	if !ok {
		return nil, nil
	}
	return s.store.sell(id, h)
}

func (s *site) apiDiveStart(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	run, err := s.store.diveStart(id)
	return map[string]string{"run": run}, err
}

func (s *site) apiDiveFinish(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	var in struct {
		Run   string `json:"run"`
		Score int    `json:"score"`
	}
	if !readJSON(w, r, &in) {
		return nil, nil
	}
	return s.store.diveFinish(id, in.Run, in.Score)
}

// The frog other sites draw when a visitor has none of their own.
const houseSeed = 1207

var houseHatched = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

type panel struct {
	House    bool     `json:"house"`
	Frog     frogView `json:"frog"`
	DiveBest int      `json:"dive_best"`
}

func (s *site) apiPanel(w http.ResponseWriter, r *http.Request) {
	now := s.store.now()
	if id, _, err := s.pondOf(r); err == nil {
		if p, err := loadPond(s.store.r, id); err == nil {
			writeJSON(w, http.StatusOK, panel{Frog: frogOf(p, now), DiveBest: p.DiveBest})
			return
		}
	}
	house := &pond{Seed: houseSeed, HatchedAt: houseHatched.Unix()}
	writeJSON(w, http.StatusOK, panel{House: true, Frog: frogOf(house, now)})
}

// ---------------------------------------------------------------- the key

func (s *site) apiRestore(w http.ResponseWriter, r *http.Request) {
	if !s.restores.allow(web.ClientIP(r), s.store.now()) {
		writeError(w, http.StatusTooManyRequests, "Too many tries. Give it ten minutes.")
		return
	}
	var in struct {
		Key string `json:"key"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	key, ok := normalizeKey(in.Key)
	if !ok {
		n := len(strings.FieldsFunc(strings.ToLower(in.Key), func(c rune) bool { return c < 'a' || c > 'z' }))
		msg := "One of those isn't a pond word. Check the spelling."
		if n != keyWords {
			msg = "A pond key is six words and that was " + plural(n, "word") + "."
		}
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	id, err := s.store.idOf(key)
	if errors.Is(err, errNoPond) {
		writeError(w, http.StatusNotFound, "No frog has that key. Check the words and their order.")
		return
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	p, err := loadPond(s.store.r, id)
	if err != nil {
		fail(w, r, err)
		return
	}
	setKeyCookie(w, key)
	writeJSON(w, http.StatusOK, map[string]string{"name": titled(newGenome(p.Seed).Name)})
}

func (s *site) apiRotate(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	key, err := s.store.rotate(id)
	if err != nil {
		return nil, err
	}
	setKeyCookie(w, key)
	return map[string]bool{"ok": true}, nil
}

// limiter counts tries in a fixed window, for one caller and for everybody.
// There is no username to key a restore on, so it is per network and site
// wide, and never per address alone, which IPv6 rotation walks straight past.
type limiter struct {
	mu      sync.Mutex
	window  time.Duration
	each    int
	all     int
	started time.Time
	total   int
	seen    map[string]int
}

func newLimiter(window time.Duration, each, all int) *limiter {
	return &limiter{window: window, each: each, all: all, seen: map[string]int{}}
}

func network(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	if addr.Is4() || addr.Is4In6() {
		return addr.Unmap().String()
	}
	p, _ := addr.Prefix(64)
	return p.String()
}

func (l *limiter) allow(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.started) >= l.window {
		l.started, l.total, l.seen = now, 0, map[string]int{}
	}
	net := network(ip)
	if l.total >= l.all || l.seen[net] >= l.each {
		return false
	}
	l.total++
	l.seen[net]++
	return true
}

// ---------------------------------------------------------------- dev

func (s *site) apiDevClock(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Hours float64 `json:"hours"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	s.store.skew.Add(int64(in.Hours * 3600))
	writeJSON(w, http.StatusOK, map[string]int64{"skew": s.store.skew.Load()})
}
