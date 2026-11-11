package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"reflect"
	"strconv"
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
	case errors.Is(err, errPoor), errors.Is(err, errNotYet), errors.Is(err, errNotHeld), errors.Is(err, errBadRun), errors.Is(err, errBadTap), errors.Is(err, errBadName), errors.Is(err, errBadPrice), errors.Is(err, errStallFull),
		errors.Is(err, errFullPads), errors.Is(err, errMaxed), errors.Is(err, errSoldOut), errors.Is(err, errFed), errors.Is(err, errLevel), errors.Is(err, errShort), errors.Is(err, errGiven),
		errors.Is(err, errNoBurrow), errors.Is(err, errNotLodge), errors.Is(err, errHaveIt), errors.Is(err, errCap), errors.Is(err, errFirm), errors.Is(err, errCount):
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

func (s *site) apiStarter(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	return s.store.starterClaim(id)
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

func (s *site) apiBuild(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	var in struct {
		Build string `json:"build"`
	}
	if !readJSON(w, r, &in) {
		return nil, nil
	}
	return s.store.build(id, in.Build)
}

func (s *site) apiCollect(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	return s.store.collect(id)
}

func (s *site) apiPick(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	return s.store.pick(id)
}

func (s *site) apiWater(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	return s.store.water(id)
}

func (s *site) apiCook(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	var in struct {
		A string `json:"a"`
		B string `json:"b"`
	}
	if !readJSON(w, r, &in) {
		return nil, nil
	}
	return s.store.cook(id, in.A, in.B)
}

func (s *site) apiSellDish(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	var in struct {
		Dish string `json:"dish"`
	}
	if !readJSON(w, r, &in) {
		return nil, nil
	}
	return s.store.sellDish(id, in.Dish)
}

func readHat(w http.ResponseWriter, r *http.Request, bare bool) (hat, bool) {
	var h hat
	if !readJSON(w, r, &h) {
		return h, false
	}
	if !h.valid() && !(bare && (h.Shape == 0 || h.earned())) {
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

// A hood or a site's hat is earned and never sold, so it can't be asked for.
func (s *site) apiWant(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	var h hat
	if !readJSON(w, r, &h) {
		return nil, nil
	}
	if h.Shape != 0 && !h.valid() {
		writeError(w, http.StatusBadRequest, "no such hat")
		return nil, nil
	}
	return s.store.want(id, h)
}

func (s *site) apiFront(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	var in struct {
		Frog int64 `json:"frog"`
	}
	if !readJSON(w, r, &in) {
		return nil, nil
	}
	return s.store.front(id, in.Frog)
}

func (s *site) apiLay(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	var in struct {
		Frog  int64  `json:"frog"`
		Dye   string `json:"dye"`
		Stamp string `json:"stamp"`
		Drops string `json:"drops"`
	}
	if !readJSON(w, r, &in) {
		return nil, nil
	}
	return s.store.lay(id, in.Frog, in.Dye, in.Stamp, in.Drops)
}

func (s *site) apiFeed(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	var in struct {
		Frog int64  `json:"frog"`
		Dish string `json:"dish"`
	}
	if !readJSON(w, r, &in) {
		return nil, nil
	}
	return s.store.feed(id, in.Frog, in.Dish)
}

// apiDiary is a page of one frog's diary, newest first.
func (s *site) apiDiary(w http.ResponseWriter, r *http.Request) {
	id, _, err := s.pondOf(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	frog, _ := strconv.ParseInt(r.URL.Query().Get("frog"), 10, 64)
	d, err := s.store.diaryOf(id, frog, r.URL.Query().Get("before"))
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *site) apiPet(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	var in struct {
		Spot string `json:"spot"`
	}
	if !readJSON(w, r, &in) {
		return nil, nil
	}
	return s.store.pet(id, in.Spot)
}

func (s *site) apiBuy(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	var in struct {
		Shop string `json:"shop"`
		Slot int    `json:"slot"`
	}
	if !readJSON(w, r, &in) {
		return nil, nil
	}
	return s.store.buy(id, in.Shop, in.Slot)
}

func (s *site) apiDyeLoad(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	var in struct {
		Rocks []string `json:"rocks"`
	}
	if !readJSON(w, r, &in) {
		return nil, nil
	}
	return s.store.dyeLoad(id, in.Rocks)
}

func (s *site) apiDyeCollect(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	return s.store.dyeCollect(id)
}

func (s *site) apiTrinketBox(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	return s.store.trinketBox(id)
}

func (s *site) apiPlace(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	var in struct {
		Spot int    `json:"spot"`
		Kind string `json:"kind"`
	}
	if !readJSON(w, r, &in) {
		return nil, nil
	}
	return s.store.place(id, in.Spot, in.Kind)
}

func (s *site) apiSellTrinket(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	var in struct {
		Kind string `json:"kind"`
	}
	if !readJSON(w, r, &in) {
		return nil, nil
	}
	return s.store.sellTrinket(id, in.Kind)
}

func (s *site) apiJobsCollect(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	return s.store.collectJobs(id)
}

func (s *site) apiJob(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	var in struct {
		Frog   int64  `json:"frog"`
		Job    string `json:"job"`
		Recipe string `json:"recipe"`
	}
	if !readJSON(w, r, &in) {
		return nil, nil
	}
	return s.store.setJob(id, in.Frog, in.Job, in.Recipe)
}

func (s *site) apiBuyPad(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	return s.store.buyPad(id)
}

func (s *site) apiDiveStart(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	return s.store.diveStart(id)
}

func (s *site) apiDiveFinish(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	var in struct {
		Run   string `json:"run"`
		Score int    `json:"score"`
		Gates int    `json:"gates"`
	}
	if !readJSON(w, r, &in) {
		return nil, nil
	}
	return s.store.diveFinish(id, in.Run, in.Score, in.Gates)
}

// The frog other sites draw when a visitor has none of their own.
const houseSeed = 1207

var houseHatched = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

type panel struct {
	House bool `json:"house"`
	// The hat of the site that asked, when this visit is the one that found it.
	Found   *siteView `json:"found,omitempty"`
	Frog    frogView  `json:"frog"`
	Pebbles int       `json:"pebbles"`
	Power   int       `json:"power"`
	Rank    string    `json:"rank"`
	Todo    []todo    `json:"todo"`
}

// apiPanel is what the other sites draw the visitor's frog from. Each is same
// site as pond, so the Lax cookie rides along once the origin is allowed
// credentials. A frog's first time on a site finds that site's hat.
func (s *site) apiPanel(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Vary", "Origin")
	from := siteOf(r.Header.Get("Origin"))
	if from >= 0 {
		w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}
	now := s.store.now()
	if id, _, err := s.pondOf(r); err == nil {
		if v, err := s.store.view(id); err == nil {
			out := panel{Frog: v.Frog, Pebbles: v.Pebbles, Power: v.Share.Power.Total, Rank: v.Share.Power.Rank, Todo: v.Todo}
			if from >= 0 {
				if news, err := s.store.visit(id, from); err == nil && news {
					st := v.Sites[from]
					st.Got = true
					out.Found = &st
				}
			}
			writeJSON(w, http.StatusOK, out)
			return
		}
	}
	house := &pond{Seed: houseSeed, HatchedAt: houseHatched.Unix()}
	writeJSON(w, http.StatusOK, panel{House: true, Frog: frogOf(house, now), Todo: []todo{}})
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
	writeJSON(w, http.StatusOK, map[string]string{"name": titled(frogOf(p, s.store.now()).Name)})
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

// apiDevDay pins a made up day, or with no reading goes back to dash's.
func (s *site) apiDevDay(w http.ResponseWriter, r *http.Request) {
	var in *reading
	if !readJSON(w, r, &in) {
		return
	}
	if in == nil || reflect.DeepEqual(*in, reading{}) {
		s.store.day.pinned.Store(false)
		s.store.day.last.Store(nil)
	} else {
		in.At = time.Now()
		s.store.day.pinned.Store(true)
		s.store.day.last.Store(in)
		if err := s.store.lockMarket(); err != nil {
			fail(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *site) apiNames(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	return map[string][]string{"names": rollNames()}, nil
}

func (s *site) apiRename(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	var in struct {
		Frog int64  `json:"frog"`
		Name string `json:"name"`
	}
	if !readJSON(w, r, &in) {
		return nil, nil
	}
	return s.store.rename(id, in.Frog, in.Name)
}

func (s *site) apiWizard(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	var in struct {
		Kind string `json:"kind"`
	}
	if !readJSON(w, r, &in) {
		return nil, nil
	}
	return s.store.wizard(id, in.Kind)
}

func (s *site) apiStallList(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	var in struct {
		Kind  string `json:"kind"`
		Price int    `json:"price"`
	}
	if !readJSON(w, r, &in) {
		return nil, nil
	}
	return s.store.stallList(id, in.Kind, in.Price)
}

func (s *site) apiStallTake(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	var in struct {
		Slot int `json:"slot"`
	}
	if !readJSON(w, r, &in) {
		return nil, nil
	}
	return s.store.stallTake(id, in.Slot)
}

func (s *site) apiShares(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	var in struct {
		Key  string `json:"key"`
		N    int    `json:"n"`
		Sell bool   `json:"sell"`
	}
	if !readJSON(w, r, &in) {
		return nil, nil
	}
	if in.Sell {
		return s.store.sellShares(id, in.Key, in.N)
	}
	return s.store.buyShares(id, in.Key, in.N)
}

func (s *site) apiStallCollect(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	return s.store.stallCollect(id)
}

func (s *site) apiGift(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	var in struct {
		Who  int    `json:"who"`
		Dish string `json:"dish"`
	}
	if !readJSON(w, r, &in) {
		return nil, nil
	}
	return s.store.gift(id, in.Who, in.Dish)
}

func (s *site) apiLodge(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	var in struct {
		Who int  `json:"who"`
		In  bool `json:"in"`
	}
	if !readJSON(w, r, &in) {
		return nil, nil
	}
	return s.store.lodge(id, in.Who, in.In)
}

func (s *site) apiRent(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	return s.store.collectRent(id)
}

func (s *site) apiFavour(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	return s.store.favour(id)
}

func (s *site) apiArcadeStart(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	var in struct {
		Game string `json:"game"`
	}
	if !readJSON(w, r, &in) {
		return nil, nil
	}
	return s.store.arcadeStart(id, in.Game)
}

func (s *site) apiFishKeep(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	var in struct {
		Kind string `json:"kind"`
	}
	if !readJSON(w, r, &in) {
		return nil, nil
	}
	return s.store.keepFish(id, in.Kind)
}

func (s *site) apiArcadeFinish(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	var in struct {
		Game  string `json:"game"`
		Run   string `json:"run"`
		Score int    `json:"score"`
		// What a round of fishing landed, by kind.
		Fish map[string]int `json:"fish"`
	}
	if !readJSON(w, r, &in) {
		return nil, nil
	}
	return s.store.arcadeFinish(id, in.Game, in.Run, in.Score, in.Fish)
}

func (s *site) apiBugStart(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	return s.store.bugStart(id)
}

func (s *site) apiBugTap(id int64, r *http.Request, w http.ResponseWriter) (any, error) {
	var in struct {
		Run string `json:"run"`
		Pad int    `json:"pad"`
	}
	if !readJSON(w, r, &in) {
		return nil, nil
	}
	return s.store.bugTap(id, in.Run, in.Pad)
}
