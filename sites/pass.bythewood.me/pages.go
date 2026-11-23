package main

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"pass.bythewood.me/suite"
)

// keepDays is EVENTS_DAYS_RETAIN in compose, how far back the event log goes.
const keepDays = 90

func (s *site) chrome(r *http.Request, on string) (suite.Page, bool) {
	in := s.auth.Authenticated(r)
	p := suite.Page{Tool: "pass", SignedIn: in, Source: sourceURL, Author: author, Year: time.Now().Year(), Meta: "my own password server"}
	tabs := []suite.Tab{{Name: "home", Href: "/"}, {Name: "source", Href: sourceURL}}
	if in {
		tabs = []suite.Tab{{Name: "overview", Href: "/"}, {Name: "logins", Href: "/logins"}, {Name: "devices", Href: "/devices"}, {Name: "account", Href: "/account"}}
	}
	for _, t := range tabs {
		t.On = t.Name == on
		p.Tabs = append(p.Tabs, t)
	}
	srv := s.watch.server()
	p.Right = []suite.Seg{{Text: "times are UTC"}}
	// Signed out sees only whether it's answering, since the rest of the
	// health word, like signups on or no two step, is a hint to an attacker.
	p.Status = []suite.Seg{{Text: "up", Kind: "ok"}}
	if !srv.Up {
		p.Status = []suite.Seg{{Text: "down", Kind: "bad"}}
	}
	if in {
		h := s.health(time.Now())
		p.Status = []suite.Seg{{Text: h.Word, Kind: h.Level}}
		p.Meta = "signups off"
		if s.admin.Signups() {
			p.Meta = "signups on"
		}
		if srv.Version != "" {
			p.Meta = "vaultwarden " + srv.Version + " · " + p.Meta
			p.Right = append([]suite.Seg{{Text: "vaultwarden " + srv.Version}}, p.Right...)
		}
		if h.Failed > 0 {
			p.Status = append(p.Status, suite.Seg{Text: fmt.Sprintf("%d failed today", h.Failed), Kind: "bad"})
		}
	}
	return p, in
}

// Health is the one line state for the status line, the tab dot and the title.
type Health struct {
	Level, Word, Note string
	Failed            int
	NewDevices        []Device
}

func (s *site) health(now time.Time) Health {
	h := Health{Level: "ok", Word: "up"}
	srv := s.watch.server()
	evs, evErr := s.vault.Events(now.Add(-24*time.Hour), 1000)
	for _, e := range evs {
		if e.Failed() {
			h.Failed++
		}
	}
	ds, _ := s.vault.Devices()
	for _, d := range ds {
		if now.Sub(d.Created) < 24*time.Hour {
			h.NewDevices = append(h.NewDevices, d)
		}
	}
	switch {
	case !srv.Up:
		h.Level, h.Word, h.Note = "bad", "down", "Vaultwarden isn't answering, so the apps are working from their cached copy"
	case evErr != nil && !missingTable(evErr):
		h.Level, h.Word, h.Note = "warn", "unreadable", "Vaultwarden's database couldn't be read: "+evErr.Error()
	case s.admin.Signups():
		h.Level, h.Word, h.Note = "warn", "signups on", "Anyone can make an account until signups are switched off on the account page"
	case s.noTOTP():
		h.Level, h.Word, h.Note = "warn", "no two step", "Two step login is off, so the master password alone opens the vault. Turn it on from the account page"
	case h.Failed > 0:
		h.Level, h.Word = "warn", "failed sign in"
		h.Note = fmt.Sprintf("%d failed sign in %s in the last day", h.Failed, plural(h.Failed, "attempt", "attempts"))
	case len(h.NewDevices) > 0:
		h.Level, h.Word = "warn", "new device"
		h.Note = fmt.Sprintf("%s (%s) signed in for the first time %s", h.NewDevices[0].Name, deviceKind(h.NewDevices[0].Type), ago(h.NewDevices[0].Created))
	case srv.Behind():
		h.Level, h.Word, h.Note = "warn", "update out", "Vaultwarden "+srv.Latest+" is out and this is "+srv.Version
	}
	return h
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// Addr is one address in the event log and how it went.
type Addr struct {
	IP       string
	OK, Fail int
	Last     time.Time
}

func (a Addr) Total() int { return a.OK + a.Fail }

func addrs(evs []Event) []Addr {
	by := map[string]*Addr{}
	for _, e := range evs {
		if e.Type != evLogin && !e.Failed() {
			continue
		}
		a := by[e.IP]
		if a == nil {
			a = &Addr{IP: e.IP}
			by[e.IP] = a
		}
		if e.Failed() {
			a.Fail++
		} else {
			a.OK++
		}
		if e.At.After(a.Last) {
			a.Last = e.At
		}
	}
	out := make([]Addr, 0, len(by))
	for _, a := range by {
		out = append(out, *a)
	}
	// Addresses that only ever failed go first, since they're the ones to look at.
	sort.Slice(out, func(i, j int) bool {
		si, sj := out[i].OK == 0, out[j].OK == 0
		if si != sj {
			return si
		}
		if out[i].Total() != out[j].Total() {
			return out[i].Total() > out[j].Total()
		}
		if !out[i].Last.Equal(out[j].Last) {
			return out[i].Last.After(out[j].Last)
		}
		return out[i].IP < out[j].IP
	})
	return out
}

func ipsOf(evs []Event, as []Addr) []string {
	var out []string
	for _, e := range evs {
		out = append(out, e.IP)
	}
	for _, a := range as {
		out = append(out, a.IP)
	}
	return out
}

// maxes is the largest total and the largest failure count, so an address
// that only failed is drawn against other failures and not against home.
func maxes(as []Addr) (int, int) {
	total, fail := 0, 0
	for _, a := range as {
		total, fail = max(total, a.Total()), max(fail, a.Fail)
	}
	return total, fail
}

// hourly buckets the last day by hour, oldest first, for a spark.
func hourly(evs []Event, now time.Time, keep func(Event) bool) []float64 {
	out := make([]float64, 24)
	end := now.Truncate(time.Hour).Add(time.Hour)
	for _, e := range evs {
		if !keep(e) {
			continue
		}
		i := 23 - int(end.Sub(e.At)/time.Hour)
		if i >= 0 && i < 24 {
			out[i]++
		}
	}
	return out
}

func hourLabels(now time.Time) []string {
	out := make([]string, 24)
	start := now.Truncate(time.Hour).Add(-23 * time.Hour)
	for i := range out {
		out[i] = start.Add(time.Duration(i) * time.Hour).UTC().Format("15:00")
	}
	return out
}

func (s *site) home(w http.ResponseWriter, r *http.Request) {
	page, in := s.chrome(r, "overview")
	if !in {
		page, _ = s.chrome(r, "home")
		s.render(w, 200, "landing.html", map[string]any{"Suite": page, "Footer": footer(), "Ways": waysFor()})
		return
	}
	now := time.Now().UTC()
	h := s.health(now)
	srv := s.watch.server()
	hold, herr := s.vault.Holdings()
	month, _ := s.vault.Events(now.Add(-30*24*time.Hour), 5000)
	devices, _ := s.vault.Devices()

	var day []Event
	logins, failed := 0, 0
	for _, e := range month {
		if now.Sub(e.At) <= 24*time.Hour {
			day = append(day, e)
			if e.Type == evLogin {
				logins++
			}
			if e.Failed() {
				failed++
			}
		}
	}
	active := 0
	var ticks []string
	for _, d := range devices {
		if now.Sub(d.LastSeen) <= 24*time.Hour {
			active++
			ticks = append(ticks, "")
		} else {
			ticks = append(ticks, "none")
		}
	}
	recent := month
	if len(recent) > 12 {
		recent = recent[:12]
	}
	shown := devices
	if len(shown) > 8 {
		shown = shown[:8]
	}
	as := addrs(month)
	if len(as) > 6 {
		as = as[:6]
	}
	maxAddr, maxFail := maxes(as)
	where := s.net.places(r.Context(), ipsOf(recent, as))
	s.render(w, 200, "overview.html", map[string]any{"Where": where,
		"Suite": page, "Health": h, "Server": srv, "Signups": s.admin.Signups(),
		"Hold": hold, "NoVault": herr != nil, "VaultErr": errText(herr),
		"Logins": logins, "Failed": failed, "Active": active, "Ticks": ticks, "Devices": devices, "Shown": shown,
		"Recent": recent, "Addrs": as, "MaxAddr": maxAddr, "MaxFail": maxFail,
		"HourOK":   hourly(day, now, func(e Event) bool { return e.Type == evLogin }),
		"HourBad":  hourly(day, now, func(e Event) bool { return e.Failed() }),
		"Labels":   hourLabels(now),
		"Headline": headline(len(devices), failed, h),
		"Kinds":    holdingRows(hold),
	})
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// headline is the overview's title, the device count and whatever needs me.
func headline(devices, failed int, h Health) template.HTML {
	lead := fmt.Sprintf("%s %s", spell(devices), plural(devices, "device", "devices"))
	switch {
	case h.Level == "bad":
		return template.HTML(lead + `, <em class="bad">Vaultwarden is down</em>.`)
	case failed > 0:
		return template.HTML(fmt.Sprintf(`%s, <em class="bad">%s failed %s</em>.`, lead, strings.ToLower(spell(failed)), plural(failed, "sign in", "sign ins")))
	case h.Level == "warn":
		return template.HTML(fmt.Sprintf(`%s, <em class="warn">%s</em>.`, lead, template.HTMLEscapeString(h.Word)))
	}
	return template.HTML(lead + `, <em>nothing failed</em> today.`)
}

func spell(n int) string {
	words := []string{"No", "One", "Two", "Three", "Four", "Five", "Six", "Seven", "Eight", "Nine", "Ten"}
	if n >= 0 && n < len(words) {
		return words[n]
	}
	return comma(n)
}

type Row struct {
	Key  string
	N    int
	Note string
	Kind string
}

func holdingRows(h Holdings) []Row {
	rows := []Row{{"logins", h.Logins, "", ""}, {"with a code", h.TOTP, "totp", ""}, {"secure notes", h.Notes, "", ""},
		{"cards", h.Cards, "", ""}, {"identities", h.IDs, "", ""}, {"ssh keys", h.SSHKeys, "", ""}}
	if h.Other > 0 {
		rows = append(rows, Row{"other", h.Other, "", ""})
	}
	return rows
}

func (s *site) loginsPage(w http.ResponseWriter, r *http.Request) {
	page, _ := s.chrome(r, "logins")
	now := time.Now().UTC()
	ranges := map[string]time.Duration{"24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour, "30d": 30 * 24 * time.Hour, "90d": keepDays * 24 * time.Hour}
	rng := r.URL.Query().Get("range")
	if _, ok := ranges[rng]; !ok {
		rng = "30d"
	}
	show := r.URL.Query().Get("show")
	if show != "failed" && show != "account" {
		show = "all"
	}
	ip := strings.TrimSpace(r.URL.Query().Get("ip"))
	evs, err := s.vault.Events(now.Add(-ranges[rng]), 5000)
	var rows []Event
	ok, failed, wrongPass, wrongCode, account := 0, 0, 0, 0, 0
	for _, e := range evs {
		switch {
		case e.Type == evLogin:
			ok++
		case e.Type == evFailedLogin:
			failed++
			wrongPass++
		case e.Type == evFailed2FA:
			failed++
			wrongCode++
		default:
			account++
		}
		if show == "failed" && !e.Failed() || show == "account" && (e.Type == evLogin || e.Failed()) || ip != "" && e.IP != ip {
			continue
		}
		rows = append(rows, e)
	}
	total := len(rows)
	if len(rows) > 300 {
		rows = rows[:300]
	}
	as := addrs(evs)
	maxAddr, maxFail := maxes(as)
	onlyFailed := 0
	for _, a := range as {
		if a.OK == 0 {
			onlyFailed++
		}
	}
	shownAddrs := as
	if len(shownAddrs) > 12 {
		shownAddrs = shownAddrs[:12]
	}
	where := s.net.places(r.Context(), ipsOf(rows, shownAddrs))
	why := []Row{{"wrong password", wrongPass, "", "bad"}, {"wrong two step code", wrongCode, "", "bad"}}
	var tabs []suite.Tab
	for _, k := range []string{"24h", "7d", "30d", "90d"} {
		tabs = append(tabs, suite.Tab{Name: k, Href: loginsURL(k, show, ip), On: k == rng})
	}
	var shows []suite.Tab
	for _, k := range []string{"all", "failed", "account"} {
		shows = append(shows, suite.Tab{Name: k, Href: loginsURL(rng, k, ip), On: k == show})
	}
	days := 0
	switch rng {
	case "24h":
		days = 1
	case "7d":
		days = 7
	case "30d":
		days = 30
	default:
		days = keepDays
	}
	s.render(w, 200, "logins.html", map[string]any{
		"Suite": page, "Where": where, "Range": rng, "Show": show, "IP": ip, "Ranges": tabs, "Shows": shows, "NoVault": err != nil,
		"Rows": rows, "Total": total, "OK": ok, "Failed": failed, "Account": account,
		"Addrs": shownAddrs, "AllAddrs": len(as), "MaxAddr": maxAddr, "MaxFail": maxFail, "OnlyFailed": onlyFailed, "Why": why, "MaxWhy": max(wrongPass, wrongCode, 1),
		"Days": dayStrip(evs, now, min(days, 30)),
	})
}

func loginsURL(rng, show, ip string) string {
	q := url.Values{"range": {rng}, "show": {show}}
	if ip != "" {
		q.Set("ip", ip)
	}
	return "/logins?" + q.Encode()
}

// dayStrip is one tick a day, oldest first, red where a sign in failed.
func dayStrip(evs []Event, now time.Time, days int) []Tick {
	out := make([]Tick, days)
	start := now.Truncate(24*time.Hour).AddDate(0, 0, -(days - 1))
	for i := range out {
		out[i].Day = start.AddDate(0, 0, i)
	}
	for _, e := range evs {
		i := int(e.At.Sub(start) / (24 * time.Hour))
		if i < 0 || i >= days {
			continue
		}
		if e.Failed() {
			out[i].Bad++
		} else if e.Type == evLogin {
			out[i].OK++
		}
	}
	return out
}

type Tick struct {
	Day     time.Time
	OK, Bad int
}

func (t Tick) Kind() string {
	switch {
	case t.Bad > 0:
		return "b"
	case t.OK == 0:
		return "n"
	}
	return ""
}

func (t Tick) Tip() string {
	d := t.Day.Format("Jan 2")
	switch {
	case t.Bad > 0:
		return fmt.Sprintf("%s: %d failed, %d signed in", d, t.Bad, t.OK)
	case t.OK == 0:
		return d + ": nothing"
	}
	return fmt.Sprintf("%s: %d signed in", d, t.OK)
}

func (s *site) devicesPage(w http.ResponseWriter, r *http.Request) {
	page, _ := s.chrome(r, "devices")
	now := time.Now().UTC()
	ds, err := s.vault.Devices()
	h := s.health(now)
	kinds := map[string]int{}
	active, quiet := 0, 0
	for _, d := range ds {
		kinds[deviceKind(d.Type)]++
		if now.Sub(d.LastSeen) <= 24*time.Hour {
			active++
		}
		if now.Sub(d.LastSeen) > 30*24*time.Hour {
			quiet++
		}
	}
	var byKind []Row
	for k, n := range kinds {
		byKind = append(byKind, Row{Key: k, N: n})
	}
	sort.Slice(byKind, func(i, j int) bool {
		if byKind[i].N != byKind[j].N {
			return byKind[i].N > byKind[j].N
		}
		return byKind[i].Key < byKind[j].Key
	})
	maxKind := 1
	if len(byKind) > 0 {
		maxKind = byKind[0].N
	}
	s.render(w, 200, "devices.html", map[string]any{
		"Suite": page, "Devices": ds, "NoVault": err != nil, "New": h.NewDevices,
		"Active": active, "Quiet": quiet, "ByKind": byKind, "MaxKind": maxKind,
		"Headline": deviceHeadline(len(ds), len(h.NewDevices), quiet),
	})
}

func deviceHeadline(n, fresh, quiet int) template.HTML {
	lead := fmt.Sprintf("%s %s", spell(n), plural(n, "device", "devices"))
	switch {
	case fresh > 0:
		return template.HTML(fmt.Sprintf(`%s, <em class="warn">%s new today</em>.`, lead, strings.ToLower(spell(fresh))))
	case quiet > 0:
		return template.HTML(fmt.Sprintf(`%s, <em>%s quiet for a month</em>.`, lead, strings.ToLower(spell(quiet))))
	}
	return template.HTML(lead + `, <em>all in use</em>.`)
}

func (s *site) notFound(w http.ResponseWriter, r *http.Request) {
	page, _ := s.chrome(r, "")
	s.render(w, http.StatusNotFound, "notfound.html", map[string]any{"Suite": page})
}

// suiteState is pass's entry in the merged index. A failed sign in, a new
// device or Vaultwarden down is what needs me.
func (s *site) suiteState(r *http.Request) suite.State {
	h := s.health(time.Now())
	hold, _ := s.vault.Holdings()
	st := suite.State{Tool: "pass", Level: h.Level, Note: fmt.Sprintf("%s items, nothing failed today", comma(hold.Items())),
		Items: []suite.Item{
			{Kind: "page", Name: "overview", Href: baseURL + "/", Note: "the vault and who signed in"},
			{Kind: "page", Name: "logins", Href: baseURL + "/logins", Note: "every sign in and failure"},
			{Kind: "page", Name: "failed sign ins", Href: baseURL + "/logins?show=failed", Note: "wrong passwords and codes"},
			{Kind: "page", Name: "devices", Href: baseURL + "/devices", Note: "each app signed in"},
		}}
	if h.Level != "ok" {
		st.Note, st.Count = h.Note, 1
	}
	return st
}

func waysFor() suite.Ways {
	return suite.Ways{
		Host:       "pass.bythewood.me",
		APITitle:   "The Bitwarden apps",
		APIText:    "The browser extension, the desktop app and the phone app take a self-hosted server URL when you sign in, and talk to Vaultwarden from then on.",
		API:        `<span class="c"># Bitwarden, Log in, Accessing</span>` + "\n" + `self-hosted &rarr; <span class="w">https://pass.example.com</span>`,
		AgentTitle: "Ask who signed in",
		AgentText:  "Sign ins, failures and devices are log records in logging, so an agent reading logs over the mcp connector sees them too.",
		Agent:      `<span class="c"># anything odd on pass?</span>` + "\n" + `<span class="a">logs_search</span> source=pass &rarr; <span class="g">2 wrong passwords</span>`,
		RunText:    "point a Bitwarden app at localhost.",
	}
}

func footer() suite.Footer {
	return suite.Footer{Tool: "pass", About: "A password server for my own devices, with every sign in and device it has seen.",
		Source: sourceURL, Author: author, Year: time.Now().Year(),
		Pages: []suite.Tab{{Name: "Home", Href: "/"}}}
}
