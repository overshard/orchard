package main

import (
	"database/sql"
	"fmt"
	"html/template"
	"slices"
	"strings"
	"time"

	"auth.bythewood.me/suite"
)

// newPlaceWindow is how long a city or country has to go unseen before a sign
// in from it is called a new place.
const newPlaceWindow = 30 * 24 * time.Hour

// connectorIdle is how long a connector can go without a call before it sorts
// to the top as one worth revoking.
const connectorIdle = 7 * 24 * time.Hour

// stripDays is how many days the per session strip covers.
const stripDays = 14

// place is how a session or an event reads as somewhere, empty when Cloudflare
// gave no country.
func place(city, country string) string {
	switch {
	case country == "":
		return ""
	case city == "":
		return country
	}
	return city + ", " + country
}

// places answers whether a place is new at a moment, from the sign ins in the
// events table, oldest first.
type places struct{ logins []Event }

func placesFrom(events []Event) places {
	var p places
	for _, e := range events {
		if e.Kind == evLogin {
			p.logins = append(p.logins, e)
		}
	}
	slices.SortFunc(p.logins, func(a, b Event) int { return a.TS.Compare(b.TS) })
	return p
}

// isNew is true when nobody signed in from where in the window before at but
// somebody signed in from somewhere. With no sign ins at all in the window
// there's nothing to compare against, so nothing is called new.
func (p places) isNew(where string, at time.Time) bool {
	if where == "" {
		return false
	}
	known := false
	for _, e := range p.logins {
		if !e.TS.Before(at) {
			break
		}
		if at.Sub(e.TS) > newPlaceWindow {
			continue
		}
		known = true
		if place(e.City, e.Country) == where {
			return false
		}
	}
	return known
}

// sessionRow is a session as the sessions page shows it.
type sessionRow struct {
	Session
	Name      string
	Where     string
	Connector bool
	NewPlace  bool
	Idle      bool
	Days      []string
	Active7   int
}

// eventRow is an event as the activity page shows it.
type eventRow struct {
	Event
	Where    string
	NewPlace bool
}

type placeCount struct {
	Where    string
	N, Width int
	New      bool
	old      int
}

// tile is one of the checkup tiles, linked to the page that fixes it.
type tile struct {
	Label, Value, Kind, Delta, Href, Tip string
	Spark                                template.HTML
	Ticks                                []string
}

// overview is everything the account, sessions and activity pages summarise,
// read in one go.
type overview struct {
	Browsers, Connectors []sessionRow
	Recent               []eventRow
	Places               []placeCount
	NewPlaces            []eventRow
	Checkup              []tile
	Remaining            int
}

// allEvents is the whole local events table, newest first. The sweeper keeps
// it near 2000 rows, so reading it whole is cheap.
func allEvents(db *sql.DB) ([]Event, error) { return recentEvents(db, 2500) }

// activeDays maps a session to the UTC days it was used.
func activeDays(db *sql.DB, since time.Time) map[int64]map[int64]bool {
	out := map[int64]map[int64]bool{}
	rows, err := db.Query(`SELECT session_id, day FROM session_days WHERE day >= ?`, since.Unix()/86400)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id, day int64
		if rows.Scan(&id, &day) == nil {
			if out[id] == nil {
				out[id] = map[int64]bool{}
			}
			out[id][day] = true
		}
	}
	return out
}

func buildOverview(db *sql.DB, current []byte, now time.Time) (overview, error) {
	var ov overview
	list, err := listSessions(db, current)
	if err != nil {
		return ov, err
	}
	events, err := allEvents(db)
	if err != nil {
		return ov, err
	}
	ov.Remaining, err = countRecoveryCodes(db)
	if err != nil {
		return ov, err
	}
	pl := placesFrom(events)
	today := now.Unix() / 86400
	days := activeDays(db, now.AddDate(0, 0, -stripDays))

	for _, s := range list {
		row := sessionRow{Session: s, Name: browser(s.UA), Where: place(s.City, s.Country)}
		row.Connector = strings.HasPrefix(s.UA, connectorUA)
		if row.Connector {
			row.Name = strings.TrimPrefix(s.UA, connectorUA)
			row.Idle = now.Sub(s.LastSeen) > connectorIdle
		} else {
			row.NewPlace = pl.isNew(row.Where, s.Created)
		}
		// The row's own times count too, since session_days is newer than
		// most sessions.
		seen := days[s.ID]
		if seen == nil {
			seen = map[int64]bool{}
		}
		seen[s.Created.Unix()/86400] = true
		seen[s.LastSeen.Unix()/86400] = true
		for d := today - stripDays + 1; d <= today; d++ {
			if seen[d] {
				row.Days = append(row.Days, "")
				if d > today-7 {
					row.Active7++
				}
			} else {
				row.Days = append(row.Days, "n")
			}
		}
		if row.Connector {
			ov.Connectors = append(ov.Connectors, row)
		} else {
			ov.Browsers = append(ov.Browsers, row)
		}
	}
	// Trouble first. listSessions is newest first, and a stable sort keeps
	// that order within each group.
	slices.SortStableFunc(ov.Browsers, func(a, b sessionRow) int {
		return rankBool(b.NewPlace) - rankBool(a.NewPlace)
	})
	slices.SortStableFunc(ov.Connectors, func(a, b sessionRow) int {
		return rankBool(b.Idle) - rankBool(a.Idle)
	})

	counts := map[string]*placeCount{}
	var order []string
	for _, e := range events {
		if e.Kind != evLogin || now.Sub(e.TS) > newPlaceWindow {
			continue
		}
		row := eventRow{Event: e, Where: place(e.City, e.Country), NewPlace: pl.isNew(place(e.City, e.Country), e.TS)}
		if len(ov.Recent) < 6 {
			ov.Recent = append(ov.Recent, row)
		}
		if row.NewPlace {
			ov.NewPlaces = append(ov.NewPlaces, row)
		}
		key := row.Where
		if key == "" {
			key = "unknown"
		}
		if counts[key] == nil {
			counts[key] = &placeCount{Where: key}
			order = append(order, key)
		}
		counts[key].N++
		if !row.NewPlace {
			counts[key].old++
		}
	}
	most := 0
	for _, k := range order {
		most = max(most, counts[k].N)
	}
	for _, k := range order {
		c := counts[k]
		c.New = c.old == 0
		c.Width = max(4, c.N*100/max(most, 1))
		ov.Places = append(ov.Places, *c)
	}
	slices.SortStableFunc(ov.Places, func(a, b placeCount) int { return b.N - a.N })
	if len(ov.Places) > 6 {
		ov.Places = ov.Places[:6]
	}

	ov.Checkup = checkup(ov, activityStats(db, now, events), events, now)
	return ov, nil
}

func rankBool(b bool) int {
	if b {
		return 1
	}
	return 0
}

// dayLabels names the last n days, oldest first, for the crosshair.
func dayLabels(now time.Time, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = now.AddDate(0, 0, i-n+1).UTC().Format("Jan 2")
	}
	return out
}

// checkup is the eight tiles over the account and sessions pages, each one
// linking to where its problem gets fixed.
func checkup(ov overview, st eventStats, events []Event, now time.Time) []tile {
	labels := dayLabels(now, 30)
	var t []tile

	newBrowsers := 0
	var bt []string
	for _, b := range ov.Browsers {
		if b.NewPlace {
			newBrowsers++
			bt = append(bt, "warn")
		} else {
			bt = append(bt, "")
		}
	}
	t = append(t, tile{Label: "browsers", Value: itoa(len(ov.Browsers)), Href: "/sessions",
		Delta: plural(newBrowsers, "new place", "new places", "none from a new place"), Ticks: bt,
		Kind: warnIf(newBrowsers > 0)})

	idle := 0
	var ct []string
	for _, c := range ov.Connectors {
		if c.Idle {
			idle++
			ct = append(ct, "warn")
		} else {
			ct = append(ct, "")
		}
	}
	cd := "none idle"
	if len(ov.Connectors) == 0 {
		cd = "none allowed"
	} else if idle > 0 {
		cd = count(idle, "idle a week", "idle a week")
	}
	t = append(t, tile{Label: "connectors", Value: itoa(len(ov.Connectors)), Href: "/sessions#connectors", Delta: cd, Ticks: ct})

	np := tile{Label: "new places · 30d", Value: itoa(len(ov.NewPlaces)), Href: "/activity?f=new", Delta: "every sign in from a known place"}
	if n := len(ov.NewPlaces); n > 0 {
		np.Kind = "warn"
		np.Delta = ov.NewPlaces[0].Where + ", " + ago(ov.NewPlaces[0].TS)
	}
	var npt []string
	for i := len(ov.Recent) - 1; i >= 0; i-- {
		npt = append(npt, warnIf(ov.Recent[i].NewPlace))
	}
	np.Ticks = npt
	t = append(t, np)

	byRecovery, byCode := 0, 0
	for _, e := range events {
		if e.Kind != evLogin || now.Sub(e.TS) > 30*24*time.Hour {
			continue
		}
		switch e.Detail {
		case "a recovery code":
			byRecovery++
		case "a code":
			byCode++
		}
	}
	sd := "all by phone code"
	switch {
	case st.SignIns == 0:
		sd = "none in 30 days"
	case byRecovery == st.SignIns:
		sd = "all by recovery code"
	case byRecovery > 0:
		sd = count(byRecovery, "by recovery code", "by recovery code")
	}
	t = append(t, tile{Label: "sign ins · 30d", Value: itoa(st.SignIns), Href: "/activity?f=signin", Delta: sd,
		Spark: suite.SparkT("ok", st.SignInDays, labels)})

	unused := max(0, st.Codes-byCode)
	t = append(t, tile{Label: "codes sent · 30d", Value: itoa(st.Codes), Href: "/activity?f=signin",
		Delta: plural(unused, "not used", "not used", "every one used"), Spark: suite.SparkT("flat", st.CodeDays, labels)})

	rk := "flat"
	if st.Refused > 0 {
		rk = "warn"
	}
	t = append(t, tile{Label: "refused · 30d", Value: itoa(st.Refused), Href: "/activity?f=refused", Kind: warnIf(st.Refused > 0),
		Delta: "wrong codes and limits hit", Spark: suite.SparkT(rk, st.RefusedDays, labels)})

	var rt []string
	for i := range recoveryCount {
		if i < ov.Remaining {
			rt = append(rt, "")
		} else {
			rt = append(rt, "none")
		}
	}
	rd := "none used in 30 days"
	if st.Recovery > 0 {
		rd = count(st.Recovery, "used in 30 days", "used in 30 days")
	}
	t = append(t, tile{Label: "recovery codes", Value: fmt.Sprintf("%d of %d", ov.Remaining, recoveryCount), Href: "/security",
		Kind: warnIf(ov.Remaining < 3), Delta: rd, Ticks: rt})

	old := tile{Label: "oldest session", Value: "none", Href: "/sessions", Delta: "no browsers signed in"}
	if len(ov.Browsers) > 0 {
		o := ov.Browsers[0]
		for _, b := range ov.Browsers {
			if b.Created.Before(o.Created) {
				o = b
			}
		}
		old.Value = shortAge(now.Sub(o.Created))
		old.Delta = o.Name
		if o.Where != "" {
			old.Delta += ", " + o.Where
		}
		for range ov.Browsers {
			old.Ticks = append(old.Ticks, "")
		}
	}
	return append(t, old)
}

func warnIf(b bool) string {
	if b {
		return "warn"
	}
	return ""
}

// plural is count with its own wording for none.
func plural(n int, one, many, none string) string {
	if n == 0 {
		return none
	}
	return count(n, one, many)
}

// shortAge is a duration as the tiles show it, 3h or 21d.
func shortAge(d time.Duration) string {
	if d < 24*time.Hour {
		return itoa(int(d.Hours())) + "h"
	}
	return itoa(int(d.Hours()/24)) + "d"
}

// until is how far off an expiry is, as the tables show it.
func until(t time.Time) string {
	d := time.Until(t)
	switch {
	case d <= 0:
		return "now"
	case d < time.Hour:
		return "in " + itoa(int(d.Minutes())) + "m"
	case d < 24*time.Hour:
		return "in " + itoa(int(d.Hours())) + "h"
	}
	return "in " + itoa(int(d.Hours()/24)) + "d"
}

// methodLabel is sessions.method as a person reads it.
func methodLabel(m string) string {
	switch m {
	case methodCode:
		return "phone code"
	case methodRecovery:
		return "recovery code"
	case methodConnector:
		return "your consent"
	}
	return ""
}

// activityFilter is one chip over the activity table.
type activityFilter struct {
	Key, Name string
	N         int
}

// filterMatch says whether an event belongs under a chip.
func filterMatch(key string, e eventRow) bool {
	switch key {
	case "signin":
		switch e.Kind {
		case evLogin, evLogout, evCodeRequested, evCodeSent, evCodeExpired:
			return true
		}
	case "refused":
		return eventClass(e.Kind) == "is-warn"
	case "connectors":
		return e.Kind == evConnectorAdded || (e.Kind == evSessionRevoked && strings.HasPrefix(e.Detail, connectorUA))
	case "recovery":
		switch e.Kind {
		case evRecoveryUsed, evRecoveryFailed, evRecoveryRotated:
			return true
		}
	case "new":
		return e.NewPlace
	}
	return false
}

var activityFilters = []activityFilter{
	{Key: "", Name: "all"}, {Key: "signin", Name: "sign ins"}, {Key: "refused", Name: "refused"},
	{Key: "connectors", Name: "connectors"}, {Key: "recovery", Name: "recovery"}, {Key: "new", Name: "new place"},
}

// activityView is the activity page's table, chips and tiles, the table capped
// at limit rows after the filter.
func activityView(events []Event, key string, limit int, st eventStats, ov overview, now time.Time) ([]eventRow, []activityFilter, []tile) {
	pl := placesFrom(events)
	all := make([]eventRow, 0, len(events))
	for _, e := range events {
		w := place(e.City, e.Country)
		all = append(all, eventRow{Event: e, Where: w, NewPlace: pl.isNew(w, e.TS)})
	}
	filters := slices.Clone(activityFilters)
	for i := range filters {
		for _, e := range all {
			if filters[i].Key == "" || filterMatch(filters[i].Key, e) {
				filters[i].N++
			}
		}
	}
	var rows []eventRow
	for _, e := range all {
		if len(rows) == limit {
			break
		}
		if key == "" || filterMatch(key, e) {
			rows = append(rows, e)
		}
	}

	labels := dayLabels(now, 30)
	rk := "flat"
	if st.Refused > 0 {
		rk = "warn"
	}
	recoveryDays := make([]float64, 30)
	for _, e := range events {
		if e.Kind != evRecoveryUsed {
			continue
		}
		if i := 29 - int(now.Sub(e.TS).Hours()/24); i >= 0 && i < 30 {
			recoveryDays[i]++
		}
	}
	np := tile{Label: "new places · 30d", Value: itoa(len(ov.NewPlaces)), Href: "/activity?f=new", Delta: "every sign in from a known place"}
	if len(ov.NewPlaces) > 0 {
		np.Kind = "warn"
		np.Delta = ov.NewPlaces[0].Where + ", " + ago(ov.NewPlaces[0].TS)
	}
	for i := len(ov.Recent) - 1; i >= 0; i-- {
		np.Ticks = append(np.Ticks, warnIf(ov.Recent[i].NewPlace))
	}
	tiles := []tile{
		{Label: "sign ins · 30d", Value: itoa(st.SignIns), Href: "/activity?f=signin", Delta: plural(st.Places, "place", "places", "from nowhere yet"),
			Spark: suite.SparkT("ok", st.SignInDays, labels)},
		{Label: "codes asked for · 30d", Value: itoa(st.Codes), Href: "/activity?f=signin", Delta: "each one pushed to the phone",
			Spark: suite.SparkT("flat", st.CodeDays, labels)},
		{Label: "refused · 30d", Value: itoa(st.Refused), Kind: warnIf(st.Refused > 0), Href: "/activity?f=refused", Delta: "wrong codes and limits hit",
			Spark: suite.SparkT(rk, st.RefusedDays, labels)},
		{Label: "recovery used · 30d", Value: itoa(st.Recovery), Kind: warnIf(st.Recovery > 0), Href: "/activity?f=recovery", Delta: fmt.Sprintf("%d of %d left", ov.Remaining, recoveryCount),
			Spark: suite.SparkT(warnIf(st.Recovery > 0), recoveryDays, labels)},
		np,
		{Label: "kept here", Value: itoa(len(events)), Delta: "sign ins lit, refusals amber", Ticks: st.Last},
	}
	return rows, filters, tiles
}
