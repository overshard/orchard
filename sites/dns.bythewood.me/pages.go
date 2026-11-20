package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"dns.bythewood.me/suite"
)

func (s *site) chrome(r *http.Request, on string) (suite.Page, bool) {
	in := s.auth.Authenticated(r)
	p := suite.Page{Tool: "dns", SignedIn: in, Source: sourceURL, Author: author, Year: time.Now().Year(), Meta: "a private resolver"}
	tabs := []suite.Tab{{Name: "home", Href: "/"}, {Name: "docs", Href: "/docs"}, {Name: "source", Href: sourceURL}}
	if in {
		tabs = []suite.Tab{{Name: "overview", Href: "/"}, {Name: "log", Href: "/log"}, {Name: "devices", Href: "/devices"},
			{Name: "lists", Href: "/lists"}, {Name: "docs", Href: "/docs"}}
	}
	for _, t := range tabs {
		t.On = t.Name == on
		p.Tabs = append(p.Tabs, t)
	}
	p.Right = []suite.Seg{{Text: "lists by hagezi"}, {Text: "times are UTC"}}
	h := s.health(time.Now())
	p.Status = []suite.Seg{{Text: h.Word, Kind: h.Level}}
	if in {
		tab := s.lists.tab.Load()
		if tab != nil {
			p.Status = append(p.Status, suite.Seg{Text: comma(len(tab.off)) + " listed"})
		}
		p.Meta = fmt.Sprintf("%s · kept %dd", h.Word, keepDays)
		if !h.Refreshed.IsZero() {
			p.Right[0] = suite.Seg{Text: "lists " + ago(h.Refreshed)}
		}
	}
	return p, in
}

// Health is the one line state of the resolver, for the status line, the
// tab dot and the overview's title.
type Health struct {
	Level     string
	Word      string
	Note      string
	Refreshed time.Time
	Stale     []string
	Failing   int
	Asked     int
	Quiet     []*Device
}

// quietAfter is how long a device can go without asking before it looks like
// it fell off the resolver. A phone asks all day, a computer can sleep a weekend.
func quietAfter(kind string) time.Duration {
	if kind == "phone" {
		return 6 * time.Hour
	}
	return 72 * time.Hour
}

func isQuiet(d *Device, now time.Time) bool {
	return !d.LastSeen.IsZero() && now.Sub(d.Created) > 24*time.Hour && now.Sub(d.LastSeen) > quietAfter(d.Kind)
}

func (s *site) health(now time.Time) Health {
	h := Health{Level: "ok", Word: "answering"}
	states, _ := s.store.ListStates()
	for _, li := range lists {
		st := states[li.Key]
		if st.Fetched.After(h.Refreshed) {
			h.Refreshed = st.Fetched
		}
		if st.Entries == 0 || now.Sub(st.Fetched) > 3*24*time.Hour {
			h.Stale = append(h.Stale, li.Key)
		}
	}
	h.Asked, _ = s.store.Count(Filter{Since: now.Add(-time.Hour)})
	h.Failing, _ = s.store.Count(Filter{Since: now.Add(-time.Hour), Status: "fail"})
	notAfter, _, _ := s.certs.State()
	ds, _ := s.store.Devices()
	for _, d := range ds {
		if isQuiet(d, now) {
			h.Quiet = append(h.Quiet, d)
		}
	}
	var missing []string
	if m := s.lists.missing.Load(); m != nil {
		missing = *m
	}
	switch {
	case h.Asked >= 20 && h.Failing*10 >= h.Asked:
		h.Level, h.Word = "bad", "failing"
		h.Note = fmt.Sprintf("%s of %s lookups failed in the last hour", comma(h.Failing), comma(h.Asked))
	case s.res.upstreamDown(now):
		h.Level, h.Word = "bad", "unbound down"
		h.Note = "unbound isn't answering, so lookups are going to Quad9 until it's back"
	case len(missing) > 0:
		h.Level, h.Word = "warn", "list missing"
		h.Note = strings.Join(missing, ", ") + " couldn't be read off disk, so it blocks nothing until the next refresh"
	case len(h.Quiet) > 0:
		var names []string
		for _, d := range h.Quiet {
			names = append(names, d.Name+" "+ago(d.LastSeen))
		}
		h.Level, h.Word = "warn", "device quiet"
		h.Note = "last asked: " + strings.Join(names, ", ") + ", so check it's still set up"
	case len(h.Stale) > 0:
		h.Level, h.Word = "warn", "lists stale"
		h.Note = strings.Join(h.Stale, ", ") + " hasn't refreshed in three days"
	case !notAfter.IsZero() && time.Until(notAfter) < 14*24*time.Hour:
		h.Level, h.Word = "warn", "certificate ending"
		h.Note = "the DoT certificate runs out " + notAfter.UTC().Format("Jan 2")
	}
	return h
}

func hourLabels(n int, now time.Time) []string {
	out := make([]string, n)
	end := now.UTC().Truncate(time.Hour)
	for i := range out {
		out[i] = end.Add(-time.Duration(n-1-i) * time.Hour).Format("15:00")
	}
	return out
}

func dayLabels(n int, now time.Time) []string {
	out := make([]string, n)
	end := now.UTC().Truncate(24 * time.Hour)
	for i := range out {
		out[i] = end.Add(-time.Duration(n-1-i) * 24 * time.Hour).Format("Jan 2")
	}
	return out
}

// DeviceRow is one device with its last day, for the overview and the list.
type DeviceRow struct {
	*Device
	Total, Blocked int
	Quiet          bool
	ByHour         []float64
	Days           []string
	DayTips        []string
}

func (s *site) deviceRows(now time.Time) []DeviceRow {
	ds, _ := s.store.Devices()
	var out []DeviceRow
	for _, d := range ds {
		t, b := s.store.Count(Filter{Device: d.ID, Since: now.Add(-24 * time.Hour)})
		hourly, _ := s.store.Hourly(d.ID, 24, now)
		dt, db := s.store.Daily(d.ID, 14, now)
		row := DeviceRow{Device: d, Total: t, Blocked: b, ByHour: hourly, Quiet: isQuiet(d, now)}
		for i := range dt {
			kind := ""
			if dt[i] == 0 {
				kind = "n"
				if day := now.UTC().Truncate(24 * time.Hour).Add(-time.Duration(13-i) * 24 * time.Hour); !d.LastSeen.IsZero() && day.After(d.Created) && d.Kind == "phone" {
					kind = "w"
				}
			}
			row.Days = append(row.Days, kind)
			row.DayTips = append(row.DayTips, fmt.Sprintf("%s: %s lookups, %s blocked", dayLabels(14, now)[i], comma(int(dt[i])), comma(int(db[i]))))
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Quiet != out[j].Quiet {
			return out[i].Quiet
		}
		return out[i].Total > out[j].Total
	})
	return out
}

func (s *site) deviceNames() map[int64]string {
	ds, _ := s.store.Devices()
	m := map[int64]string{}
	for _, d := range ds {
		m[d.ID] = d.Name
	}
	return m
}

// Row is a lookup with its device's name, for the log tables.
type Row struct {
	Lookup
	DeviceName string
}

func (s *site) rows(ls []Lookup) []Row {
	names := s.deviceNames()
	out := make([]Row, len(ls))
	for i, l := range ls {
		n := names[l.Device]
		if n == "" {
			n = "removed device"
		}
		out[i] = Row{Lookup: l, DeviceName: n}
	}
	return out
}

func (s *site) home(w http.ResponseWriter, r *http.Request) {
	page, in := s.chrome(r, "overview")
	if !in {
		page, _ = s.chrome(r, "home")
		s.render(w, 200, "landing.html", map[string]any{"Suite": page, "Ways": waysFor(), "Footer": footer()})
		return
	}
	now := time.Now()
	day := Filter{Since: now.Add(-24 * time.Hour)}
	total, blocked := s.store.Count(day)
	hourT, hourB := s.store.Hourly(0, 24, now)
	failed, _ := s.store.Count(Filter{Since: day.Since, Status: "fail"})
	allowed, _ := s.store.Count(Filter{Since: day.Since, Status: "allow"})
	p50, p95 := s.store.Latency(Filter{Since: day.Since, Status: "pass"})
	blockedDomains := s.store.Distinct(Filter{Since: day.Since, Status: "blocked"})
	devices := s.deviceRows(now)
	active := 0
	var activeTicks []string
	for _, d := range devices {
		if d.Total > 0 {
			active++
			activeTicks = append(activeTicks, "")
		} else {
			activeTicks = append(activeTicks, "none")
		}
	}
	recent, _ := s.store.Log(Filter{Status: "blocked", Since: now.Add(-15 * time.Minute), Limit: 12})
	topBlocked := s.store.Top(Filter{Since: day.Since, Status: "blocked"}, "name", 12)
	topAsked := s.store.Top(Filter{Since: day.Since, Status: "pass"}, "name", 12)
	reasons := s.store.Top(Filter{Since: day.Since, Status: "blocked"}, "reason", 8)
	rs, _ := s.store.Rules()
	tab := s.lists.tab.Load()
	listed := 0
	if tab != nil {
		listed = len(tab.off)
	}
	h := s.health(now)
	headline := template.HTML(fmt.Sprintf("%s lookups today, <em>%s blocked</em>.", comma(total), comma(blocked)))
	if total == 0 {
		headline = template.HTML("Nothing asked yet, <em>add a device</em>.")
	}
	if h.Level == "bad" {
		headline = template.HTML(fmt.Sprintf("%s lookups today, <em class=\"bad\">%s failing</em>.", comma(total), comma(h.Failing)))
	}
	s.render(w, 200, "overview.html", map[string]any{
		"Suite": page, "Headline": headline, "Health": h,
		"Total": total, "Blocked": blocked, "Failed": failed, "Allowed": allowed, "P50": p50, "P95": p95,
		"BlockedDomains": blockedDomains, "Active": active, "ActiveTicks": activeTicks,
		"HourT": hourT, "HourB": hourB, "Labels": hourLabels(24, now),
		"Recent": s.rows(recent), "TopBlocked": topBlocked, "TopAsked": topAsked, "Reasons": reasons,
		"Devices": devices, "Rules": len(rs), "Listed": listed, "Dropped": int(s.log.dropped.Load()),
	})
}

func (s *site) filterFrom(r *http.Request) (Filter, url.Values) {
	q := r.URL.Query()
	f := Filter{Status: q.Get("status"), Reason: q.Get("reason"), Search: strings.TrimSpace(q.Get("q"))}
	f.Device, _ = strconv.ParseInt(q.Get("device"), 10, 64)
	f.Before, _ = strconv.ParseInt(q.Get("before"), 10, 64)
	f.After, _ = strconv.ParseInt(q.Get("after"), 10, 64)
	switch q.Get("range") {
	case "15m":
		f.Since = time.Now().Add(-15 * time.Minute)
	case "1h":
		f.Since = time.Now().Add(-time.Hour)
	case "7d":
	default:
		f.Since = time.Now().Add(-24 * time.Hour)
	}
	return f, q
}

// link is the log's own url with one filter changed, for chips and pages.
func link(q url.Values, k, v string) string {
	n := url.Values{}
	for key, vals := range q {
		if key != "before" && key != "after" {
			n[key] = vals
		}
	}
	if v == "" {
		n.Del(k)
	} else {
		n.Set(k, v)
	}
	if e := n.Encode(); e != "" {
		return "/log?" + e
	}
	return "/log"
}

func (s *site) logPage(w http.ResponseWriter, r *http.Request) {
	page, _ := s.chrome(r, "log")
	f, q := s.filterFrom(r)
	f.Limit = 150
	ls, _ := s.store.Log(f)
	cf := f
	cf.Before = 0
	total, blocked := s.store.Count(cf)
	ds, _ := s.store.Devices()
	type opt struct {
		Label, Href string
		On          bool
	}
	opts := func(k string, pairs ...string) []opt {
		var out []opt
		for i := 0; i+1 < len(pairs); i += 2 {
			out = append(out, opt{pairs[i], link(q, k, pairs[i+1]), q.Get(k) == pairs[i+1]})
		}
		return out
	}
	var devOpts []string
	devOpts = append(devOpts, "every device", "")
	for _, d := range ds {
		devOpts = append(devOpts, d.Name, strconv.FormatInt(d.ID, 10))
	}
	reasonOpts := []string{"any reason", ""}
	for _, li := range lists {
		reasonOpts = append(reasonOpts, li.Key, li.Key)
	}
	var older string
	if len(ls) == f.Limit {
		older = link(q, "before", strconv.FormatInt(ls[len(ls)-1].ID, 10))
	}
	var newest int64
	if len(ls) > 0 && f.Before == 0 {
		newest = ls[0].ID
	}
	s.render(w, 200, "log.html", map[string]any{
		"Suite": page, "Rows": s.rows(ls), "Total": total, "BlockedN": blocked, "Q": q, "Search": f.Search,
		"Devices":  opts("device", devOpts...),
		"Statuses": opts("status", "everything", "", "blocked", "blocked", "allowed by me", "allow", "failed", "fail"),
		"Ranges":   opts("range", "15m", "15m", "1h", "1h", "24h", "", "7d", "7d"),
		"Reasons":  opts("reason", reasonOpts...),
		"Older":    older, "Newest": newest, "Live": f.Before == 0, "RowsQuery": rowsQuery(q),
		"Hidden": hiddenFields(q),
	})
}

func rowsQuery(q url.Values) string {
	n := url.Values{}
	for _, k := range []string{"device", "status", "reason", "q", "range"} {
		if v := q.Get(k); v != "" {
			n.Set(k, v)
		}
	}
	return n.Encode()
}

func hiddenFields(q url.Values) map[string]string {
	out := map[string]string{}
	for _, k := range []string{"device", "status", "reason", "range"} {
		if v := q.Get(k); v != "" {
			out[k] = v
		}
	}
	return out
}

// logRows is the rows newer than ?after, for the live tail.
func (s *site) logRows(w http.ResponseWriter, r *http.Request) {
	f, _ := s.filterFrom(r)
	if f.After == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	f.Limit = 100
	ls, _ := s.store.Log(f)
	if len(ls) > 0 {
		w.Header().Set("X-Newest", strconv.FormatInt(ls[0].ID, 10))
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = s.tpl.ExecuteTemplate(w, "logrows", map[string]any{"Rows": s.rows(ls)})
}

func (s *site) devicesPage(w http.ResponseWriter, r *http.Request) {
	page, _ := s.chrome(r, "devices")
	s.render(w, 200, "devices.html", map[string]any{"Suite": page, "Devices": s.deviceRows(time.Now()), "Kinds": kinds, "Toast": r.URL.Query().Get("toast")})
}

var kinds = []string{"phone", "computer", "browser", "other"}

func (s *site) addDevice(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("name"))
	kind := r.FormValue("kind")
	if name == "" || len(name) > 60 {
		http.Redirect(w, r, "/devices?toast=Give+it+a+name", http.StatusSeeOther)
		return
	}
	if !contains(kinds, kind) {
		kind = "other"
	}
	d, err := s.store.AddDevice(name, kind)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = s.reload()
	http.Redirect(w, r, fmt.Sprintf("/devices/%d?toast=Added+%s", d.ID, url.QueryEscape(d.Name)), http.StatusSeeOther)
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func (s *site) deviceFrom(w http.ResponseWriter, r *http.Request) *Device {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	d, err := s.store.Device(id)
	if err != nil {
		s.notFound(w, r)
		return nil
	}
	return d
}

func (s *site) devicePage(w http.ResponseWriter, r *http.Request) {
	d := s.deviceFrom(w, r)
	if d == nil {
		return
	}
	page, _ := s.chrome(r, "devices")
	now := time.Now()
	day := Filter{Device: d.ID, Since: now.Add(-24 * time.Hour)}
	total, blocked := s.store.Count(day)
	hourT, hourB := s.store.Hourly(d.ID, 24, now)
	recent, _ := s.store.Log(Filter{Device: d.ID, Status: "blocked", Limit: 15})
	topBlocked := s.store.Top(Filter{Device: d.ID, Since: day.Since, Status: "blocked"}, "name", 10)
	type listOpt struct {
		List
		On bool
	}
	var lo []listOpt
	for _, li := range lists {
		lo = append(lo, listOpt{li, !d.Off(li.Key)})
	}
	s.render(w, 200, "device.html", map[string]any{
		"Suite": page, "D": d, "Total": total, "Blocked": blocked, "HourT": hourT, "HourB": hourB, "Labels": hourLabels(24, now),
		"Recent": s.rows(recent), "TopBlocked": topBlocked, "Lists": lo, "Kinds": kinds,
		"DoT": d.Token + "." + dotZone, "DoH": baseURL + "/q/" + d.Token, "IPs": s.dohIPs(r.Context()),
		"Toast": r.URL.Query().Get("toast"), "ProbeZone": probeZone,
	})
}

// dohIPs are the addresses dns.bythewood.me answers on, which Windows wants
// alongside the template. They're Cloudflare's, since DoH comes in through the tunnel.
func (s *site) dohIPs(ctx context.Context) []string {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupHost(ctx, "dns.bythewood.me")
	if err != nil {
		return nil
	}
	sort.Strings(addrs)
	return addrs
}

func (s *site) editDevice(w http.ResponseWriter, r *http.Request) {
	d := s.deviceFrom(w, r)
	if d == nil {
		return
	}
	_ = r.ParseForm()
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" || len(name) > 60 {
		name = d.Name
	}
	kind := r.FormValue("kind")
	if !contains(kinds, kind) {
		kind = d.Kind
	}
	on := r.Form["list"]
	var off []string
	for _, li := range lists {
		if !contains(on, li.Key) {
			off = append(off, li.Key)
		}
	}
	if err := s.store.UpdateDevice(d.ID, name, kind, off); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = s.reload()
	http.Redirect(w, r, fmt.Sprintf("/devices/%d?toast=Saved", d.ID), http.StatusSeeOther)
}

func (s *site) deviceOp(w http.ResponseWriter, r *http.Request) {
	d := s.deviceFrom(w, r)
	if d == nil {
		return
	}
	switch r.PathValue("op") {
	case "rotate":
		_ = s.store.RotateToken(d.ID)
		_ = s.reload()
		http.Redirect(w, r, fmt.Sprintf("/devices/%d?toast=New+token,+set+it+up+again", d.ID), http.StatusSeeOther)
	case "delete":
		_ = s.store.DeleteDevice(d.ID)
		_ = s.reload()
		http.Redirect(w, r, "/devices?toast=Removed+"+url.QueryEscape(d.Name), http.StatusSeeOther)
	default:
		s.notFound(w, r)
	}
}

// ListRow is one blocklist with what it did in the last week.
type ListRow struct {
	List
	St        ListState
	Blocked7d int
	OffOn     []string
	Loaded    int
}

func (s *site) listsPage(w http.ResponseWriter, r *http.Request) {
	page, _ := s.chrome(r, "lists")
	now := time.Now()
	states, _ := s.store.ListStates()
	hits := map[string]int{}
	for _, t := range s.store.Top(Filter{Status: "blocked", Since: now.Add(-7 * 24 * time.Hour)}, "reason", 50) {
		hits[t.Key] = t.N
	}
	ds, _ := s.store.Devices()
	tab := s.lists.tab.Load()
	var rows []ListRow
	for i, li := range lists {
		row := ListRow{List: li, St: states[li.Key], Blocked7d: hits[li.Key]}
		if tab != nil && i < len(tab.size) {
			row.Loaded = tab.size[i]
		}
		for _, d := range ds {
			if d.Off(li.Key) {
				row.OffOn = append(row.OffOn, d.Name)
			}
		}
		rows = append(rows, row)
	}
	type ruleRow struct {
		Rule
		Hits int
	}
	rs, _ := s.store.Rules()
	ruleHits := map[string]int{}
	for _, t := range s.store.Top(Filter{Status: "deny", Since: now.Add(-7 * 24 * time.Hour)}, "reason", 200) {
		ruleHits[strings.TrimPrefix(t.Key, "rule ")] = t.N
	}
	var rr []ruleRow
	for _, x := range rs {
		n := ruleHits[x.Domain]
		if x.Action == "allow" {
			n, _ = s.store.Count(Filter{Status: "allow", Search: x.Domain, Since: now.Add(-7 * 24 * time.Hour)})
		}
		rr = append(rr, ruleRow{x, n})
	}
	s.render(w, 200, "lists.html", map[string]any{"Suite": page, "Lists": rows, "Rules": rr, "Toast": r.URL.Query().Get("toast"),
		"Prefill": cleanName(r.URL.Query().Get("domain")), "Action": r.URL.Query().Get("action")})
}

func (s *site) refreshLists(w http.ResponseWriter, r *http.Request) {
	go s.lists.refresh(context.Background())
	http.Redirect(w, r, "/lists?toast=Refreshing,+give+it+a+minute", http.StatusSeeOther)
}

func (s *site) addRule(w http.ResponseWriter, r *http.Request) {
	domain, action := r.FormValue("domain"), r.FormValue("action")
	back := safeBack(r.FormValue("back"), "/lists")
	sep := "?"
	if strings.Contains(back, "?") {
		sep = "&"
	}
	if err := s.store.PutRule(domain, action, strings.TrimSpace(r.FormValue("note"))); err != nil {
		http.Redirect(w, r, back+sep+"toast="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	_ = s.reload()
	word := "Allowed"
	if action == "block" {
		word = "Blocked"
	}
	http.Redirect(w, r, back+sep+"toast="+url.QueryEscape(word+" "+cleanName(domain)), http.StatusSeeOther)
}

// safeBack keeps a redirect on this site. A backslash is refused since
// browsers read /\evil.com as //evil.com.
func safeBack(back, def string) string {
	u, err := url.Parse(back)
	if err != nil || u.Scheme != "" || u.Host != "" || !strings.HasPrefix(back, "/") || strings.HasPrefix(back, "//") || strings.ContainsAny(back, "\\\r\n") {
		return def
	}
	return back
}

func (s *site) deleteRule(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	x, err := s.store.DeleteRule(id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	_ = s.reload()
	http.Redirect(w, r, "/lists?toast="+url.QueryEscape("Removed the rule for "+x.Domain), http.StatusSeeOther)
}

// checkPage says what would happen to a name, and why.
func (s *site) checkPage(w http.ResponseWriter, r *http.Request) {
	page, _ := s.chrome(r, "lists")
	name := cleanName(r.URL.Query().Get("domain"))
	data := map[string]any{"Suite": page, "Name": name}
	if looksLikeDomain(name) {
		v, in := s.res.Explain(name)
		data["V"], data["In"] = v, in
		last, _ := s.store.Log(Filter{Search: name, Limit: 10})
		data["Last"] = s.rows(last)
	} else if name != "" {
		data["Bad"] = true
	}
	s.render(w, 200, "check.html", data)
}

func (s *site) probe(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	h, ok := s.res.probes.get(r.PathValue("nonce"))
	_ = json.NewEncoder(w).Encode(map[string]any{"seen": ok, "device": h.Device, "proto": h.Proto})
}

func (s *site) docs(w http.ResponseWriter, r *http.Request) {
	page, in := s.chrome(r, "docs")
	s.render(w, 200, "docs.html", map[string]any{"Suite": page, "SignedIn": in, "Footer": footer(), "Lists": lists, "KeepDays": keepDays})
}

func (s *site) notFound(w http.ResponseWriter, r *http.Request) {
	page, _ := s.chrome(r, "")
	s.render(w, http.StatusNotFound, "notfound.html", map[string]any{"Suite": page})
}

// suiteState is dns's entry in the merged index. Lookups failing or a list
// gone stale is what needs me.
func (s *site) suiteState(r *http.Request) suite.State {
	now := time.Now()
	h := s.health(now)
	total, blocked := s.store.Count(Filter{Since: now.Add(-24 * time.Hour)})
	st := suite.State{Tool: "dns", Level: h.Level, Note: fmt.Sprintf("%s lookups today, %s blocked", comma(total), comma(blocked)),
		Items: []suite.Item{
			{Kind: "page", Name: "overview", Href: baseURL + "/", Note: "what my devices asked today"},
			{Kind: "page", Name: "log", Href: baseURL + "/log", Note: "every lookup, live"},
			{Kind: "page", Name: "blocked", Href: baseURL + "/log?status=blocked&range=15m", Note: "what was just blocked, to un-break something"},
			{Kind: "page", Name: "devices", Href: baseURL + "/devices", Note: "each device and how to set it up"},
			{Kind: "page", Name: "lists", Href: baseURL + "/lists", Note: "blocklists and my own rules"},
			{Kind: "action", Name: "add a device", Href: baseURL + "/devices#add", Note: "a new token and its setup"},
		}}
	if h.Level != "ok" {
		st.Note = h.Note
		st.Count = 1
	}
	ds, _ := s.store.Devices()
	for _, d := range ds {
		st.Items = append(st.Items, suite.Item{Kind: "device", Name: d.Name, Href: fmt.Sprintf("%s/devices/%d", baseURL, d.ID), Note: d.Kind + ", seen " + ago(d.LastSeen)})
	}
	return st
}

func waysFor() suite.Ways {
	return suite.Ways{
		Host:       "dns.bythewood.me",
		APITitle:   "A token per device",
		APIText:    "Each device gets its own DoT name and DoH URL, so every lookup in the log says which device asked.",
		API:        `<span class="c"># Android, Private DNS</span>` + "\n" + `<span class="w">k3x9…</span>.dns.example.com` + "\n" + `<span class="c"># a browser</span>` + "\n" + `https://dns.example.com/q/<span class="w">k3x9…</span>`,
		AgentTitle: "Ask what was blocked",
		AgentText:  "Over the mcp connector an agent reads the same report, what was asked and blocked and by which list.",
		Agent:      `<span class="c"># why won't the app load?</span>` + "\n" + `<span class="a">dns_report</span> &rarr; <span class="g">blocked by pro</span>`,
		RunText:    "add a device on its page.",
	}
}

func footer() suite.Footer {
	return suite.Footer{Tool: "dns", About: "A private resolver for my own devices, with blocklists and a log of every lookup.",
		Source: sourceURL, Author: author, Year: time.Now().Year(),
		Pages: []suite.Tab{{Name: "Home", Href: "/"}, {Name: "Docs", Href: "/docs"}}}
}
