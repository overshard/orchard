package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"slices"
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
	NewNets   []Net
}

// newNets is a device turning up on a network it hadn't asked from in the
// last day, after it had settled on others. A token read off the wire and used
// somewhere else looks like this, and so does a trip.
func newNets(all []Net, ds []*Device, now time.Time) []Net {
	byDev := map[int64][]Net{}
	for _, n := range all {
		byDev[n.Device] = append(byDev[n.Device], n)
	}
	var out []Net
	for _, d := range ds {
		ns := byDev[d.ID]
		older := false
		for _, n := range ns {
			if now.Sub(n.First) > 24*time.Hour {
				older = true
			}
		}
		if !older {
			continue
		}
		for _, n := range ns {
			if now.Sub(n.First) <= 24*time.Hour {
				out = append(out, n)
			}
		}
	}
	return out
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
	h.NewNets = newNets(s.store.Nets(0), ds, now)
	names := map[int64]string{}
	for _, d := range ds {
		names[d.ID] = d.Name
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
	case len(h.NewNets) > 0:
		var parts []string
		for _, n := range h.NewNets {
			parts = append(parts, fmt.Sprintf("%s on AS%s %s (%s)", names[n.Device], n.ASN, n.Org, n.Country))
		}
		h.Level, h.Word = "warn", "new network"
		h.Note = strings.Join(parts, ", ") + ", new in the last day, so make a new token if that wasn't me"
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
	// DaySum says in words what the strip shows, for a screen reader or a
	// finger that can't hover.
	DaySum string
}

func (s *site) deviceRows(now time.Time) []DeviceRow {
	ds, _ := s.store.Devices()
	var out []DeviceRow
	for _, d := range ds {
		t, b := s.store.Count(Filter{Device: d.ID, Since: now.Add(-24 * time.Hour)})
		hourly, _ := s.store.Hourly(d.ID, 24, now)
		dt, db := s.store.Daily(d.ID, 14, now)
		row := DeviceRow{Device: d, Total: t, Blocked: b, ByHour: hourly, Quiet: isQuiet(d, now)}
		silent := 0
		for i := range dt {
			kind := ""
			if dt[i] == 0 {
				kind = "n"
				if day := now.UTC().Truncate(24 * time.Hour).Add(-time.Duration(13-i) * 24 * time.Hour); !d.LastSeen.IsZero() && day.After(d.Created) && d.Kind == "phone" {
					kind = "w"
				}
			}
			if dt[i] == 0 {
				silent++
			}
			row.Days = append(row.Days, kind)
			row.DayTips = append(row.DayTips, fmt.Sprintf("%s: %s lookups, %s blocked", dayLabels(14, now)[i], comma(int(dt[i])), comma(int(db[i]))))
		}
		row.DaySum = daySum(silent, len(dt), "no lookups", "lookups every day")
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
	type tick struct {
		Key string
		N   int
	}
	var listTicks []tick
	if tab != nil {
		listed = len(tab.off)
		for i, li := range lists {
			if i < len(tab.size) {
				listTicks = append(listTicks, tick{li.Key, tab.size[i]})
			}
		}
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
		"HourA": s.store.HourlyOf("allow", 24, now), "HourF": s.store.HourlyOf("fail", 24, now), "HourMs": s.store.HourlyMs(24, now),
		"ListTicks": listTicks, "ListMax": maxTick(listTicks, func(t tick) int { return t.N }),
		"Recent": s.rows(recent), "TopBlocked": topBlocked, "TopAsked": topAsked, "Reasons": reasons,
		"Devices": devices, "Rules": len(rs), "Listed": listed, "Dropped": int(s.log.dropped.Load()),
		"Cache": cachePct(), "FellBack": int(s.res.fellBack.Load()),
	})
}

// cachePct is unbound's cache hit share as a percent, or empty when unbound
// can't be asked.
func cachePct() string {
	st, err := unboundStats(unboundCtl)
	if err != nil {
		return ""
	}
	share, ok := cacheShare(st)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%.0f%%", share*100)
}

func maxTick[T any](xs []T, n func(T) int) int {
	m := 0
	for _, x := range xs {
		m = max(m, n(x))
	}
	return m
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
		if key != "before" && key != "after" && key != "toast" {
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
	type sel struct {
		Label, Value string
		On           bool
	}
	devSel := []sel{{"every device", "", q.Get("device") == ""}}
	for _, d := range ds {
		id := strconv.FormatInt(d.ID, 10)
		devSel = append(devSel, sel{d.Name, id, q.Get("device") == id})
	}
	reasonSel := []sel{{"any reason", "", q.Get("reason") == ""}}
	for _, li := range lists {
		reasonSel = append(reasonSel, sel{li.Key, li.Key, q.Get("reason") == li.Key})
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
		"DeviceSel": devSel, "ReasonSel": reasonSel,
		"Statuses": opts("status", "everything", "", "blocked", "blocked", "allowed by me", "allow", "failed", "fail"),
		"Ranges":   opts("range", "15m", "15m", "1h", "1h", "24h", "", "7d", "7d"),
		"Older":    older, "Newest": newest, "Live": f.Before == 0, "RowsQuery": rowsQuery(q),
		"Hidden": hiddenFields(q), "Back": link(q, "", ""),
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
	back := "/log"
	if rq := rowsQuery(r.URL.Query()); rq != "" {
		back += "?" + rq
	}
	_ = s.tpl.ExecuteTemplate(w, "logrows", map[string]any{"Rows": s.rows(ls), "Back": back, "Drawn": true})
}

func (s *site) devicesPage(w http.ResponseWriter, r *http.Request) {
	page, _ := s.chrome(r, "devices")
	s.render(w, 200, "devices.html", map[string]any{"Suite": page, "Devices": s.deviceRows(time.Now()), "Kinds": kinds, "Now": time.Now(), "Toast": r.URL.Query().Get("toast")})
}

var kinds = []string{"phone", "computer", "browser", "other"}

// pauses is the minutes a device's blocking can be paused for.
var pauses = []int{5, 30, 60}

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
	primary, rest := setupFor(d.Kind)
	s.render(w, 200, "device.html", map[string]any{
		"Suite": page, "D": d, "Total": total, "Blocked": blocked, "HourT": hourT, "HourB": hourB, "Labels": hourLabels(24, now),
		"Recent": s.rows(recent), "TopBlocked": topBlocked, "Lists": lo, "Kinds": kinds,
		"DoT": d.Token + "." + dotZone, "DoH": baseURL + "/q/" + d.Token, "IPs": s.dohIPs(r.Context()),
		"Toast": r.URL.Query().Get("toast"), "ProbeZone": probeZone, "BoxIP": publicIP,
		"Primary": primary, "Rest": rest, "Quiet": isQuiet(d, now), "Nets": s.store.Nets(d.ID),
		"Paused": d.PausedAt(now), "Pauses": pauses,
	})
}

// setupFor puts the setup a kind of device most likely needs first, and the
// rest behind a fold.
func setupFor(kind string) (primary, rest []string) {
	all := []string{"android", "apple", "browser", "windows", "linux"}
	switch kind {
	case "phone":
		primary = []string{"android", "apple"}
	case "computer":
		primary = []string{"windows", "apple", "linux"}
	case "browser":
		primary = []string{"browser"}
	default:
		return all, nil
	}
	for _, w := range all {
		if !contains(primary, w) {
			rest = append(rest, w)
		}
	}
	return primary, rest
}

// appleProfile is a configuration profile that points an iPhone, iPad or Mac
// at this device's DoH URL, on Wi-Fi and cellular both.
func (s *site) appleProfile(w http.ResponseWriter, r *http.Request) {
	d := s.deviceFrom(w, r)
	if d == nil {
		return
	}
	var name bytes.Buffer
	_ = xml.EscapeText(&name, []byte(d.Name))
	inner, outer := stableUUID(d.Token+"/dns"), stableUUID(d.Token+"/profile")
	body := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>PayloadContent</key>
	<array>
		<dict>
			<key>DNSSettings</key>
			<dict>
				<key>DNSProtocol</key>
				<string>HTTPS</string>
				<key>ServerURL</key>
				<string>` + baseURL + `/q/` + d.Token + `</string>
			</dict>
			<key>PayloadDisplayName</key>
			<string>dns.bythewood.me</string>
			<key>PayloadIdentifier</key>
			<string>me.bythewood.dns.` + inner + `</string>
			<key>PayloadType</key>
			<string>com.apple.dnsSettings.managed</string>
			<key>PayloadUUID</key>
			<string>` + inner + `</string>
			<key>PayloadVersion</key>
			<integer>1</integer>
		</dict>
	</array>
	<key>PayloadDisplayName</key>
	<string>dns for ` + name.String() + `</string>
	<key>PayloadIdentifier</key>
	<string>me.bythewood.dns.profile.` + outer + `</string>
	<key>PayloadRemovalDisallowed</key>
	<false/>
	<key>PayloadType</key>
	<string>Configuration</string>
	<key>PayloadUUID</key>
	<string>` + outer + `</string>
	<key>PayloadVersion</key>
	<integer>1</integer>
</dict>
</plist>
`
	w.Header().Set("Content-Type", "application/x-apple-aspen-config")
	w.Header().Set("Content-Disposition", `attachment; filename="dns-`+strconv.FormatInt(d.ID, 10)+`.mobileconfig"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(body))
}

// stableUUID is a UUID made from a string, so downloading the profile again
// replaces the one installed instead of adding a second.
func stableUUID(s string) string {
	h := sha256.Sum256([]byte(s))
	h[6] = h[6]&0x0f | 0x50
	h[8] = h[8]&0x3f | 0x80
	return strings.ToUpper(fmt.Sprintf("%x-%x-%x-%x-%x", h[0:4], h[4:6], h[6:8], h[8:10], h[10:16]))
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
	if err := s.store.UpdateDevice(d.ID, name, kind, off, r.FormValue("safe") == "on"); err != nil {
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
	case "pause":
		min, _ := strconv.Atoi(r.FormValue("minutes"))
		if !slices.Contains(pauses, min) {
			http.Redirect(w, r, fmt.Sprintf("/devices/%d", d.ID), http.StatusSeeOther)
			return
		}
		_ = s.store.PauseDevice(d.ID, time.Now().Add(time.Duration(min)*time.Minute))
		_ = s.reload()
		http.Redirect(w, r, fmt.Sprintf("/devices/%d?toast=Blocking+paused+for+%dm", d.ID, min), http.StatusSeeOther)
	case "resume":
		_ = s.store.PauseDevice(d.ID, time.Time{})
		_ = s.reload()
		http.Redirect(w, r, fmt.Sprintf("/devices/%d?toast=Blocking+back+on", d.ID), http.StatusSeeOther)
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
	Days      []string
	DayTips   []string
	DaySum    string
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
	daily := s.store.DailyByReason(7, now)
	labels := dayLabels(7, now)
	var rows []ListRow
	for i, li := range lists {
		row := ListRow{List: li, St: states[li.Key], Blocked7d: hits[li.Key]}
		silent := 0
		for j := 0; j < 7; j++ {
			n := 0
			if daily[li.Key] != nil {
				n = daily[li.Key][j]
			}
			k := ""
			if n == 0 {
				k = "n"
				silent++
			}
			row.Days = append(row.Days, k)
			row.DayTips = append(row.DayTips, fmt.Sprintf("%s: %s blocked", labels[j], comma(n)))
		}
		row.DaySum = daySum(silent, 7, "nothing blocked", "blocks every day")
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
		Last time.Time
	}
	rs, _ := s.store.Rules()
	ruleHits := map[string]int{}
	for _, st := range []string{"deny", "rewrite"} {
		for _, t := range s.store.Top(Filter{Status: st, Since: now.Add(-7 * 24 * time.Hour)}, "reason", 200) {
			ruleHits[strings.TrimPrefix(t.Key, "rule ")] += t.N
		}
	}
	last := s.store.LastByReason()
	find := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	var rr []ruleRow
	for _, x := range rs {
		if find != "" && !strings.Contains(x.Domain, find) && !strings.Contains(strings.ToLower(x.Note), find) {
			continue
		}
		n := ruleHits[x.Domain]
		if x.Action == "allow" {
			n, _ = s.store.Count(Filter{Status: "allow", Search: x.Domain, Since: now.Add(-7 * 24 * time.Hour)})
		}
		rr = append(rr, ruleRow{x, n, last["rule "+x.Domain]})
	}
	s.render(w, 200, "lists.html", map[string]any{"Suite": page, "Lists": rows, "Rules": rr, "AllRules": len(rs), "Toast": r.URL.Query().Get("toast"),
		"Prefill": cleanName(r.URL.Query().Get("domain")), "Action": r.URL.Query().Get("action"), "Find": find,
		"Devices": ds, "Names": s.deviceNames(), "Days": labels})
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
	device, _ := strconv.ParseInt(r.FormValue("device"), 10, 64)
	if r.FormValue("only") != "" {
		device, _ = strconv.ParseInt(r.FormValue("only"), 10, 64)
	}
	if err := s.store.PutRuleFor(domain, action, strings.TrimSpace(r.FormValue("note")), device, r.FormValue("target")); err != nil {
		http.Redirect(w, r, back+sep+"toast="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	_ = s.reload()
	word := map[string]string{"allow": "Allowed", "block": "Blocked", "rewrite": "Rewrote"}[action]
	where := ""
	if device != 0 {
		if d, err := s.store.Device(device); err == nil {
			where = " on " + d.Name
		}
	}
	http.Redirect(w, r, back+sep+"toast="+url.QueryEscape(word+" "+cleanName(domain)+where), http.StatusSeeOther)
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
	back := safeBack(r.FormValue("back"), "/lists")
	sep := "?"
	if strings.Contains(back, "?") {
		sep = "&"
	}
	http.Redirect(w, r, back+sep+"toast="+url.QueryEscape("Removed the rule for "+x.Domain), http.StatusSeeOther)
}

// deleteRules removes every rule picked on the lists page.
func (s *site) deleteRules(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	n := 0
	for _, v := range r.Form["id"] {
		id, _ := strconv.ParseInt(v, 10, 64)
		if _, err := s.store.DeleteRule(id); err == nil {
			n++
		}
	}
	_ = s.reload()
	http.Redirect(w, r, "/lists?toast="+url.QueryEscape("Removed "+count(n, "rule", "rules")), http.StatusSeeOther)
}

// checkPage says what would happen to a name, and why.
func (s *site) checkPage(w http.ResponseWriter, r *http.Request) {
	page, _ := s.chrome(r, "lists")
	name := cleanName(r.URL.Query().Get("domain"))
	data := map[string]any{"Suite": page, "Name": name}
	if looksLikeDomain(name) {
		v, in := s.res.Explain(name)
		data["V"], data["In"] = v, in
		if v.Rule != 0 {
			if x, err := s.store.Rule(v.Rule); err == nil {
				data["Mine"] = x
			}
		}
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

// daySum is a day strip in words, like "2 of 14 days with no lookups".
func daySum(silent, days int, none, every string) string {
	switch silent {
	case 0:
		return every
	case days:
		return fmt.Sprintf("%d days, all with %s", days, none)
	}
	return fmt.Sprintf("%d of %d days with %s", silent, days, none)
}
