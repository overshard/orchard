package main

import (
	"fmt"
	"html/template"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gate.bythewood.me/suite"
)

func (s *site) chrome(r *http.Request, on string) (suite.Page, bool) {
	in := s.auth.Authenticated(r)
	p := suite.Page{Tool: "gate", SignedIn: in, Source: sourceURL, Author: author, Year: time.Now().Year(), Meta: "in front of every site"}
	tabs := []suite.Tab{{Name: "home", Href: "/"}, {Name: "docs", Href: "/docs"}, {Name: "source", Href: sourceURL}}
	if in {
		tabs = []suite.Tab{{Name: "overview", Href: "/"}, {Name: "rules", Href: "/rules"}, {Name: "traffic", Href: "/traffic"},
			{Name: "bans", Href: "/bans"}, {Name: "settings", Href: "/settings"}, {Name: "docs", Href: "/docs"}}
	}
	for _, t := range tabs {
		t.On = t.Name == on
		p.Tabs = append(p.Tabs, t)
	}
	mode := s.engine.Mode()
	p.Right = []suite.Seg{{Text: "country and ASN by DB-IP"}, {Text: "times are UTC"}}
	switch mode {
	case modeOn:
		p.Status = []suite.Seg{{Text: "enforcing", Kind: "ok"}}
	case modeWatch:
		p.Status = []suite.Seg{{Text: "watching only, " + left(s.engine.until.Load(), time.Now()) + " left", Kind: "warn"}}
	default:
		p.Status = []suite.Seg{{Text: "off, " + left(s.engine.until.Load(), time.Now()) + " left", Kind: "bad"}}
	}
	if in {
		rules := *s.engine.rules.Load()
		waiting := 0
		all, _ := s.store.Rules()
		for _, r := range all {
			if r.Pending {
				waiting++
			}
		}
		p.Status = append(p.Status, suite.Seg{Text: count(len(rules), "rule", "rules")})
		if waiting > 0 {
			p.Status = append(p.Status, suite.Seg{Text: fmt.Sprintf("● %d waiting", waiting), Kind: "warn"})
		}
		p.Meta = fmt.Sprintf("%s · %s", p.Status[0].Text, count(len(rules), "rule", "rules"))
		if built := s.geo.Built(); built != "" {
			p.Right[0] = suite.Seg{Text: "DB-IP " + built}
		}
	}
	return p, in
}

func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%s %s", comma(n), many)
}

func (s *site) home(w http.ResponseWriter, r *http.Request) {
	page, in := s.chrome(r, "overview")
	if !in {
		page, _ = s.chrome(r, "home")
		s.render(w, 200, "landing.html", map[string]any{"Suite": page, "Ways": waysFor(), "Footer": footer()})
		return
	}
	now := time.Now()
	day := s.store.Day(24*time.Hour, now)
	uses := s.ruleRows(24*time.Hour, now)
	var waiting []RuleUse
	watched, hits := make(Series, 24), make(Series, 24)
	would, shadowed := 0, 0
	for _, u := range uses {
		if u.Pending {
			waiting = append(waiting, u)
		}
		if u.ShadowBy != "" {
			shadowed++
		}
		would += u.Watched
		watched = sumSeries(24, watched, u.WatchedByHour)
		hits = sumSeries(24, hits, u.ByHour)
	}
	f := TrafficFilter{Window: 24 * time.Hour}
	bans, _ := s.store.Bans()
	nets, netTotal := s.store.NetworksByHour(24*time.Hour, now)
	sv := s.store.Solves(24*time.Hour, now)
	held, _ := s.store.HeldChanges()
	s.render(w, 200, "overview.html", map[string]any{"Suite": page, "Day": day, "Headline": headline(day, len(waiting)+len(held), s.engine.Mode()),
		"Held": held, "Msg": r.URL.Query().Get("msg"),
		"Rules": uses, "Waiting": waiting, "Labels": hourLabels(24, now), "Solves": sv, "Watched": watched, "Would": would,
		"RuleHits": hits, "Shadowed": shadowed, "NetSeries": nets, "NetTotal": netTotal,
		"Networks": s.store.Groups("asn", f, "refused", 5, now), "Countries": s.store.Groups("country", f, "refused", 5, now),
		"Hosts": s.store.Groups("host", f, "refused", 5, now), "Paths": s.store.TopPaths(TrafficFilter{Window: 24 * time.Hour, Verdict: "refused"}, 5, now),
		"Events": s.eventRows(s.store.Events(f, "", 12, now), now), "Bans": len(bans), "Mode": s.engine.Mode()})
}

// sortUses puts proposals first, then the busiest, with rules that did nothing
// at the end in the order they were written.
func sortUses(u []RuleUse) []RuleUse {
	out := append([]RuleUse(nil), u...)
	key := func(x RuleUse) int {
		if x.Pending {
			return 1 << 30
		}
		return x.Hits + x.Watched
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && key(out[j]) > key(out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func topHits(u []RuleUse) int {
	top := 1
	for _, x := range u {
		top = max(top, x.Hits+x.Watched)
	}
	return top
}

func headline(d Day, waiting int, mode string) template.HTML {
	switch {
	case mode == modeOff:
		return `Gate is <em class="bad">off</em>, every request goes through.`
	case mode == modeWatch:
		return template.HTML(fmt.Sprintf(`Watching only, <em>%s would have been refused</em>.`, comma(d.Refused())))
	case waiting > 0:
		return template.HTML(fmt.Sprintf(`%s refused today, <em>%s waiting</em>.`, comma(d.Refused()), count(waiting, "change", "changes")))
	case d.Checked == 0:
		return `Nothing checked yet, <em>waiting on Caddy</em>.`
	}
	return template.HTML(fmt.Sprintf(`%s refused today, <em>nothing waiting</em>.`, comma(d.Refused())))
}

func (s *site) rulesPage(w http.ResponseWriter, r *http.Request) {
	page, _ := s.chrome(r, "rules")
	now := time.Now()
	window := windowOf(r.URL.Query().Get("range"), 24*time.Hour)
	uses := s.ruleRows(window, now)
	changes, total, _ := s.store.Changes(30)
	s.render(w, 200, "rules.html", map[string]any{"Suite": page, "Rules": uses, "TopRule": topHits(uses),
		"Changes": changes, "Version": total, "Range": rangeName(window), "Msg": r.URL.Query().Get("msg")})
}

func (s *site) newRulePage(w http.ResponseWriter, r *http.Request) {
	page, _ := s.chrome(r, "rules")
	q := r.URL.Query()
	rule := Rule{Action: actBlock, Status: 403, When: q.Get("when"), Name: q.Get("name")}
	s.render(w, 200, "rule.html", map[string]any{"Suite": page, "Rule": rule, "New": true, "Ends": ""})
}

func (s *site) rulePage(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	rule, err := s.store.Rule(id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	page, _ := s.chrome(r, "rules")
	now := time.Now()
	use := s.store.RuleUses([]Rule{rule}, 7*24*time.Hour, now)[0]
	f := TrafficFilter{Rule: rule.Name, Window: 7 * 24 * time.Hour}
	var lock []string
	if rule.Watching && r.URL.Query().Get("confirm") != "" {
		on := rule
		on.Watching = false
		lock = s.lockoutFor(&on)
	}
	s.render(w, 200, "rule.html", map[string]any{"Suite": page, "Rule": rule, "Use": use, "Ends": "", "ConfirmOn": lock,
		"Saved":  r.URL.Query().Get("saved") != "",
		"Events": s.store.Events(f, "", 40, now), "Paths": s.store.TopPaths(f, 8, now), "IPs": s.store.TopIPs(f, 8, now)})
}

// ruleFromForm reads the rule form. Ends is a choice of how long from now,
// with keep meaning leave it as it is.
func ruleFromForm(r *http.Request, rule Rule) Rule {
	rule.Name = strings.TrimSpace(r.FormValue("name"))
	rule.Note = strings.TrimSpace(r.FormValue("note"))
	rule.When = strings.ReplaceAll(strings.TrimSpace(r.FormValue("when")), "\r\n", "\n")
	rule.Action = r.FormValue("action")
	rule.Status, _ = strconv.Atoi(r.FormValue("status"))
	rule.Message = strings.TrimSpace(r.FormValue("message"))
	rule.Rate, _ = strconv.Atoi(r.FormValue("rate"))
	rule.BanAfter, _ = strconv.Atoi(r.FormValue("ban_after"))
	h, _ := strconv.Atoi(r.FormValue("ban_hours"))
	rule.BanFor = time.Duration(h) * time.Hour
	rule.Quiet = r.FormValue("quiet") != ""
	rule.Watching = r.FormValue("watching") != ""
	switch e := r.FormValue("ends"); e {
	case "", "keep":
	case "never":
		rule.Expires = time.Time{}
	default:
		if d, err := strconv.Atoi(e); err == nil && d > 0 {
			rule.Expires = time.Now().UTC().Add(time.Duration(d) * time.Hour)
		}
	}
	return rule
}

// needsConfirm is a save that would turn on a rule catching signing in
// without the second tick that says so.
func (s *site) needsConfirm(r *http.Request, rule *Rule) []string {
	if rule.Watching || r.FormValue("lockout_ok") == "1" || rule.validate() != nil {
		return nil
	}
	return s.lockoutFor(rule)
}

func (s *site) addRule(w http.ResponseWriter, r *http.Request) {
	rule := ruleFromForm(r, Rule{Author: "you"})
	if lock := s.needsConfirm(r, &rule); len(lock) > 0 {
		page, _ := s.chrome(r, "rules")
		s.render(w, http.StatusConflict, "rule.html", map[string]any{"Suite": page, "Rule": rule, "New": true, "Lockout": lock, "Ends": r.FormValue("ends")})
		return
	}
	if err := s.store.AddRule(&rule, "you"); err != nil {
		page, _ := s.chrome(r, "rules")
		s.render(w, http.StatusBadRequest, "rule.html", map[string]any{"Suite": page, "Rule": rule, "New": true, "Error": err.Error(), "Ends": r.FormValue("ends")})
		return
	}
	_ = s.reload()
	http.Redirect(w, r, fmt.Sprintf("/rules/%d?saved=1", rule.ID), http.StatusSeeOther)
}

func (s *site) editRule(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	old, err := s.store.Rule(id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	rule := ruleFromForm(r, old)
	rule.Pending = false
	if lock := s.needsConfirm(r, &rule); len(lock) > 0 {
		page, _ := s.chrome(r, "rules")
		use := s.store.RuleUses([]Rule{old}, 7*24*time.Hour, time.Now())[0]
		s.render(w, http.StatusConflict, "rule.html", map[string]any{"Suite": page, "Rule": rule, "Use": use, "Lockout": lock, "Ends": r.FormValue("ends")})
		return
	}
	if err := s.store.UpdateRule(&rule, "you"); err != nil {
		page, _ := s.chrome(r, "rules")
		use := s.store.RuleUses([]Rule{old}, 7*24*time.Hour, time.Now())[0]
		s.render(w, http.StatusBadRequest, "rule.html", map[string]any{"Suite": page, "Rule": rule, "Use": use, "Error": err.Error(), "Ends": r.FormValue("ends")})
		return
	}
	_ = s.reload()
	http.Redirect(w, r, fmt.Sprintf("/rules/%d?saved=1", rule.ID), http.StatusSeeOther)
}

// ruleOp is the one click changes: turn a watching rule on, put one back to
// watching, dismiss a proposal and remove a rule.
func (s *site) ruleOp(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	rule, err := s.store.Rule(id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	back := "/rules"
	if b := r.FormValue("back"); strings.HasPrefix(b, "/") && !strings.HasPrefix(b, "//") {
		back = b
	}
	switch r.PathValue("op") {
	case "on":
		rule.Watching, rule.Pending = false, false
		if lock := s.needsConfirm(r, &rule); len(lock) > 0 {
			http.Redirect(w, r, fmt.Sprintf("/rules/%d?confirm=1", rule.ID), http.StatusSeeOther)
			return
		}
		err = s.store.UpdateRule(&rule, "you")
	case "watch":
		rule.Watching, rule.Pending = true, false
		err = s.store.UpdateRule(&rule, "you")
	case "keep":
		rule.Pending = false
		err = s.store.UpdateRule(&rule, "you")
	case "delete":
		err = s.store.DeleteRule(rule, "you")
		if back == fmt.Sprintf("/rules/%d", rule.ID) {
			back = "/rules"
		}
	default:
		s.notFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = s.reload()
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func windowOf(v string, def time.Duration) time.Duration {
	switch v {
	case "1h":
		return time.Hour
	case "24h":
		return 24 * time.Hour
	case "7d":
		return 7 * 24 * time.Hour
	case "30d":
		return 30 * 24 * time.Hour
	}
	return def
}

func rangeName(d time.Duration) string {
	switch d {
	case time.Hour:
		return "1h"
	case 7 * 24 * time.Hour:
		return "7d"
	case 30 * 24 * time.Hour:
		return "30d"
	}
	return "24h"
}

func filterOf(q url.Values) TrafficFilter {
	f := TrafficFilter{Window: windowOf(q.Get("range"), 24*time.Hour), Country: strings.ToUpper(q.Get("country")),
		Host: strings.ToLower(q.Get("host")), IP: q.Get("ip"), Rule: q.Get("rule"), Verdict: q.Get("verdict")}
	f.ASN, _ = strconv.Atoi(strings.TrimPrefix(strings.ToUpper(q.Get("asn")), "AS"))
	return f
}

// Query is the filter back as a query string with one key changed.
func (f TrafficFilter) With(key, val string) string {
	v := url.Values{}
	set := func(k, x string) {
		if x != "" {
			v.Set(k, x)
		}
	}
	if f.ASN != 0 {
		set("asn", itoa(f.ASN))
	}
	set("country", f.Country)
	set("host", f.Host)
	set("ip", f.IP)
	set("rule", f.Rule)
	set("verdict", f.Verdict)
	if r := rangeName(f.Window); r != "24h" {
		set("range", r)
	}
	if val == "" {
		v.Del(key)
	} else {
		v.Set(key, val)
	}
	if len(v) == 0 {
		return "/traffic"
	}
	return "/traffic?" + v.Encode()
}

func (f TrafficFilter) Any() bool {
	return f.ASN != 0 || f.Country != "" || f.Host != "" || f.IP != "" || f.Rule != "" || f.Verdict != ""
}

// Draft is a rule written from the filter, for the button that starts one.
func (f TrafficFilter) Draft() string {
	var l []string
	if f.Host != "" {
		l = append(l, "host is "+f.Host)
	}
	if f.ASN != 0 {
		l = append(l, "asn is "+itoa(f.ASN))
	}
	if f.Country != "" {
		l = append(l, "country is "+f.Country)
	}
	if f.IP != "" {
		l = append(l, "ip is "+f.IP)
	}
	return strings.Join(l, "\n")
}

func (s *site) trafficPage(w http.ResponseWriter, r *http.Request) {
	page, _ := s.chrome(r, "traffic")
	now := time.Now()
	f := filterOf(r.URL.Query())
	day := s.store.Day(f.Window, now)
	if f.Any() {
		day = s.filteredDay(f, now)
	}
	s.render(w, 200, "traffic.html", map[string]any{"Suite": page, "F": f, "Range": rangeName(f.Window), "Day": day,
		"Networks": s.store.Groups("asn", f, "", 12, now), "Refusing": s.store.Groups("asn", f, "refused", 12, now),
		"Countries": s.store.Groups("country", f, "", 10, now), "Hosts": s.store.Groups("host", f, "", 14, now),
		"Paths": s.store.TopPaths(f, 12, now), "IPs": s.store.TopIPs(f, 12, now), "Events": s.eventRows(s.store.Events(f, "", 60, now), now),
		"Ranges": []string{"1h", "24h", "7d", "30d"}})
}

// filteredDay is Day narrowed to one filter, summed from the traffic table.
// That table has no rule or address, so a rule's totals come from its own hits
// and an address's from the sampled events.
func (s *site) filteredDay(f TrafficFilter, now time.Time) Day {
	if f.Rule != "" {
		if r, err := s.store.RuleNamed(f.Rule); err == nil {
			return s.store.ruleDay(r, f.Window, now)
		}
		return s.store.eventDay(TrafficFilter{Rule: f.Rule, Window: f.Window}, now)
	}
	if f.IP != "" {
		return s.store.eventDay(f, now)
	}
	n := hours(f.Window)
	d := Day{ByHour: make(Series, n), BlockedByHour: make(Series, n), ChallengedByHour: make(Series, n)}
	where, args := f.where(now)
	rows, err := s.store.db.Query(`SELECT hour, verdict, SUM(n) FROM traffic WHERE `+where+` GROUP BY hour, verdict`, args...)
	if err != nil {
		return d
	}
	defer rows.Close()
	for rows.Next() {
		var hour int64
		var v string
		var c int
		if rows.Scan(&hour, &v, &c) != nil {
			continue
		}
		i := slot(hour, now, n)
		if i < 0 || i >= n {
			continue
		}
		d.Checked += c
		d.ByHour[i] += float64(c)
		switch v {
		case vPass:
			d.Passed += c
		case vExempt:
			d.Exempt += c
		case vSolved:
			d.Solved += c
		case vBlock, vBanned, vLimit:
			if v == vBlock {
				d.Blocked += c
			} else if v == vBanned {
				d.Banned += c
			} else {
				d.Limited += c
			}
			d.BlockedByHour[i] += float64(c)
		case vChallenge:
			d.Challenged += c
			d.ChallengedByHour[i] += float64(c)
		}
	}
	return d
}

func (s *site) bansPage(w http.ResponseWriter, r *http.Request) {
	page, _ := s.chrome(r, "bans")
	bans, _ := s.store.Bans()
	rows := make([]BanRow, len(bans))
	refused := s.store.BanRefusals(bans)
	now := time.Now()
	total := 0
	for i, b := range bans {
		rows[i] = BanRow{Ban: b, Refused: refused[b.IP.String()]}
		total += rows[i].Refused
		if span := b.Until.Sub(b.Created); span > 0 {
			rows[i].Left = max(2, int(100*b.Until.Sub(now)/span))
		}
	}
	s.render(w, 200, "bans.html", map[string]any{"Suite": page, "Bans": rows, "Refused": total, "Error": r.URL.Query().Get("error")})
}

func (s *site) addBan(w http.ResponseWriter, r *http.Request) {
	ip, err := netip.ParseAddr(strings.TrimSpace(r.FormValue("ip")))
	if err != nil {
		http.Redirect(w, r, "/bans?error="+url.QueryEscape("That isn't an address."), http.StatusSeeOther)
		return
	}
	if s.engine.orchard(ip) {
		http.Redirect(w, r, "/bans?error="+url.QueryEscape("That's one of orchard's own addresses, which gate never refuses."), http.StatusSeeOther)
		return
	}
	h, _ := strconv.Atoi(r.FormValue("hours"))
	if h <= 0 {
		h = 24
	}
	now := time.Now().UTC()
	b := Ban{IP: ip.Unmap(), Until: now.Add(time.Duration(h) * time.Hour), Rule: "by hand", Created: now}
	if err := s.store.AddBan(b); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.engine.addBan(b)
	s.store.Change("you", "ban", "banned "+b.IP.String()+" for "+until(b.Until))
	http.Redirect(w, r, "/bans", http.StatusSeeOther)
}

// liftBan takes one address or the picked ones from the bulk bar.
func (s *site) liftBan(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	for _, v := range r.Form["ip"] {
		ip, err := netip.ParseAddr(v)
		if err != nil {
			continue
		}
		_ = s.store.Lift(ip)
		s.engine.lift(ip)
		s.store.Change("you", "ban", "lifted "+ip.String())
	}
	http.Redirect(w, r, "/bans", http.StatusSeeOther)
}

// BanRow is a ban with what it refused since it started and the share of its
// time still to run.
type BanRow struct {
	Ban
	Refused, Left int
}

// BanRefusals counts each ban's refused requests from the sampled events.
func (s *Store) BanRefusals(bans []Ban) map[string]int {
	out := map[string]int{}
	for _, b := range bans {
		var n int
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM events WHERE ip = ? AND verdict = 'banned' AND at >= ?`, b.IP.String(), b.Created.UnixMilli()).Scan(&n)
		out[b.IP.String()] = n
	}
	return out
}

func (s *site) settingsPage(w http.ResponseWriter, r *http.Request) {
	page, _ := s.chrome(r, "settings")
	until := ""
	if s.engine.Mode() != modeOn {
		until = left(s.engine.until.Load(), time.Now())
	}
	s.render(w, 200, "settings.html", map[string]any{"Suite": page, "Mode": s.engine.Mode(), "Until": until, "UntilAt": s.engine.until.Load(), "Self": s.store.Setting("self"),
		"Built": s.geo.Built(), "Saved": r.URL.Query().Get("saved") != ""})
}

func (s *site) saveSettings(w http.ResponseWriter, r *http.Request) {
	if m := r.FormValue("mode"); m == modeOn || m == modeWatch || m == modeOff {
		d, ok := modeFor[r.FormValue("for")]
		if !ok {
			d = time.Hour
		}
		until := "0"
		what := "mode " + m
		if m != modeOn {
			until = strconv.FormatInt(time.Now().Add(d).Unix(), 10)
			what += " for " + r.FormValue("for")
		}
		if m != s.engine.Mode() || m != modeOn {
			s.store.Change("you", "gate", what)
		}
		_ = s.store.SetSetting("mode_until", until)
		_ = s.store.SetSetting("mode", m)
	}
	if r.Form.Has("self") {
		var keep []string
		for _, p := range selfAddrs(r.FormValue("self")) {
			keep = append(keep, p.String())
		}
		_ = s.store.SetSetting("self", strings.Join(keep, " "))
	}
	_ = s.reload()
	http.Redirect(w, r, "/settings?saved=1", http.StatusSeeOther)
}

func (s *site) docs(w http.ResponseWriter, r *http.Request) {
	page, in := s.chrome(r, "docs")
	s.render(w, 200, "docs.html", map[string]any{"Suite": page, "SignedIn": in, "Footer": footer()})
}

func (s *site) notFound(w http.ResponseWriter, r *http.Request) {
	page, _ := s.chrome(r, "")
	s.render(w, http.StatusNotFound, "notfound.html", map[string]any{"Suite": page})
}

// suiteState is gate's entry in the merged index. A proposal waiting is the
// one thing that needs you.
func (s *site) suiteState(r *http.Request) suite.State {
	now := time.Now()
	day := s.store.Day(24*time.Hour, now)
	rules, _ := s.store.Rules()
	st := suite.State{Tool: "gate", Level: "ok", Note: fmt.Sprintf("%s refused of %s checked today", comma(day.Refused()), comma(day.Checked)),
		Items: []suite.Item{
			{Kind: "page", Name: "overview", Href: baseURL + "/", Note: "what gate refused today"},
			{Kind: "page", Name: "rules", Href: baseURL + "/rules", Note: "every rule and what it caught"},
			{Kind: "page", Name: "traffic", Href: baseURL + "/traffic", Note: "by network, country, host and address"},
			{Kind: "page", Name: "bans", Href: baseURL + "/bans", Note: "addresses refused everywhere for a while"},
			{Kind: "action", Name: "new rule", Href: baseURL + "/rules/new", Note: "write a rule"},
		}}
	waiting := 0
	for _, r := range rules {
		it := suite.Item{Kind: "rule", Name: r.Name, Href: fmt.Sprintf("%s/rules/%d", baseURL, r.ID), Note: r.Then() + ", " + oneLine(r.When)}
		if r.Pending {
			waiting++
			it.Level = "warn"
			it.Note = "proposed by " + r.Author + ", watching. " + it.Note
		}
		st.Items = append(st.Items, it)
	}
	held, _ := s.store.HeldChanges()
	waiting += len(held)
	switch {
	case s.engine.Mode() == modeOff:
		st.Level, st.Note = "warn", "off for "+left(s.engine.until.Load(), now)+", every request goes through"
	case waiting > 0:
		st.Level, st.Count = "warn", waiting
		st.Note = count(waiting, "change", "changes") + " waiting, " + st.Note
	case s.engine.Mode() == modeWatch:
		st.Level, st.Note = "warn", "watching only for "+left(s.engine.until.Load(), now)+", "+st.Note
	}
	return st
}

func waysFor() suite.Ways {
	return suite.Ways{
		Host:       "gate.bythewood.me",
		APITitle:   "One line in Caddy",
		APIText:    "Every site block imports one snippet that asks gate first, and nothing else about blocking lives in the Caddyfile.",
		API:        `<span class="a">forward_auth</span> orchard-gate:8000 {` + "\n" + `  uri /check` + "\n" + `  header_up X-Gate-Ip {client_ip}` + "\n}",
		AgentTitle: "Ask, then propose",
		AgentText:  "Over the mcp connector an agent reads what gate saw and proposes a rule, which watches until you turn it on.",
		Agent:      `<span class="c"># anything hammering the feed?</span>` + "\n" + `<span class="a">gate_propose</span> &rarr; <span class="g">watching</span>`,
		RunText:    "point Caddy's forward_auth at it.",
	}
}

func footer() suite.Footer {
	return suite.Footer{Tool: "gate", About: "Checks every request to my sites against rules I can change while it runs.",
		Source: sourceURL, Author: author, Year: time.Now().Year(),
		Pages: []suite.Tab{{Name: "Home", Href: "/"}, {Name: "Docs", Href: "/docs"}}}
}
