package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// usd is a dollar amount with the places it needs, so a call's fraction of a
// cent still shows.
func usd(v float64) string {
	switch {
	case v == 0:
		return "$0"
	case math.Abs(v) < 0.1:
		return fmt.Sprintf("$%.4f", v)
	}
	return fmt.Sprintf("$%.2f", v)
}

// clock is when a call happened on the budget's clock, the time alone for
// today.
func clock(t time.Time) string {
	l, n := t.In(budgetZone), time.Now().In(budgetZone)
	if l.YearDay() == n.YearDay() && l.Year() == n.Year() {
		return l.Format("15:04:05")
	}
	return l.Format("Jan 2 15:04")
}

// change is a delta against the 24 hours before, or nothing when there was
// nothing then.
func change(now, then float64, unit string) string {
	if then == 0 {
		return ""
	}
	if unit == "%" {
		d := 100 * (now - then) / then
		return fmt.Sprintf("%+.0f%% on yesterday", d)
	}
	d := (now - then) / 1000
	return strings.Replace(fmt.Sprintf("%+.1fs on yesterday", d), "-", "−", 1)
}

type tick struct {
	Kind, Tip, Label, Href string
	Height                 float64
}

func (s *site) overview(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	month, _ := s.month(now)
	day := s.dayStats(now)
	recent, _, _ := s.store.FindCalls(callFilter{Limit: 8})
	days := s.spendByDay(month, now)
	var top float64
	for _, d := range days {
		top = max(top, d.Spent)
	}
	ticks := make([]tick, len(days))
	for i, d := range days {
		t := tick{Tip: d.Day.Format("Mon Jan 2") + "\n" + usd(d.Spent), Label: d.Day.Format("Mon Jan 2") + ", " + usd(d.Spent), Href: "/calls?day=" + d.Day.Format("2006-01-02"), Height: 4}
		if top > 0 && d.Spent > 0 {
			t.Height = max(8, 100*d.Spent/top)
		} else {
			t.Kind = "none"
		}
		ticks[i] = t
	}
	mix, unsplit := s.tokenMix(month)
	var mixTop float64
	for _, p := range mix {
		mixTop = max(mixTop, p.Spent)
	}
	alerts, sent := s.alertLog(month)
	var spentPct, projPct float64
	if month.Budget > 0 {
		spentPct = min(100, 100*month.Spent/month.Budget)
		projPct = min(100, 100*month.Projected/month.Budget)
	}
	keys := s.spendByKey(month)
	caps := 0
	for _, k := range keys {
		if k.Cap > 0 {
			caps++
		}
	}
	s.render(w, "overview.html", map[string]any{
		"Title": "Overview", "Recent": recent, "Month": month, "Claude": s.claude != nil, "Day": day,
		"Ticks": ticks, "Keys": keys, "Caps": caps, "Mix": mix, "MixTop": mixTop, "Unsplit": unsplit,
		"Alerts": alerts, "Sent": sent, "SpentPct": spentPct, "ProjPct": projPct,
		"CallsDelta": change(float64(day.Calls), float64(day.PrevCalls), "%"),
		"P50Delta":   change(float64(day.P50), float64(day.PrevP50), "s"),
		"DayOfMonth": now.In(budgetZone).Day(), "DaysInMonth": month.Start.AddDate(0, 1, -1).Day(),
		"Headline": s.headline(day, month), "Upstream": s.upstream, "Suite": s.chrome("overview", day, month),
	})
}

// callRanges are the calls page's range chips.
var callRanges = []struct {
	Name string
	D    time.Duration
}{{"1h", time.Hour}, {"24h", 24 * time.Hour}, {"7d", 7 * 24 * time.Hour}, {"30d", 30 * 24 * time.Hour}}

type chip struct {
	Name, Href, Kind string
}

// callsQuery is the calls page's filters read from the query string, keeping
// the raw values so a link can change one and keep the rest.
type callsQuery struct {
	callFilter
	Range, DayText string
}

func readCallsQuery(q url.Values, now time.Time) callsQuery {
	c := callsQuery{callFilter: callFilter{Caller: q.Get("caller"), Failed: q.Get("failed") == "1", Q: strings.TrimSpace(q.Get("q")), Limit: 100}}
	if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 && n <= 500 {
		c.Limit = n
	}
	c.ID, _ = strconv.ParseInt(q.Get("id"), 10, 64)
	for _, r := range callRanges {
		if q.Get("range") == r.Name {
			c.Range, c.Since = r.Name, now.Add(-r.D)
		}
	}
	if d, err := time.ParseInLocation("2006-01-02", q.Get("day"), budgetZone); err == nil {
		c.DayText, c.Day = q.Get("day"), d
	}
	return c
}

// url is this query with the given parameters changed, an empty value
// dropping one.
func (c callsQuery) url(set ...string) string {
	v := url.Values{}
	put := func(k, val string) {
		if val != "" {
			v.Set(k, val)
		}
	}
	put("caller", c.Caller)
	if c.Failed {
		v.Set("failed", "1")
	}
	put("range", c.Range)
	put("day", c.DayText)
	put("q", c.Q)
	if c.Limit != 100 {
		v.Set("limit", strconv.Itoa(c.Limit))
	}
	for i := 0; i+1 < len(set); i += 2 {
		v.Del(set[i])
		put(set[i], set[i+1])
	}
	if len(v) == 0 {
		return "/calls"
	}
	return "/calls?" + v.Encode()
}

func (s *site) callsPage(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	f := readCallsQuery(r.URL.Query(), now)
	calls, total, err := s.store.FindCalls(f.callFilter)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	usage, _ := s.store.Usage(now.Add(-30 * 24 * time.Hour))
	day := s.dayStats(now)
	month, _ := s.month(now)
	if r.URL.Query().Get("format") == "json" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"day":   map[string]any{"calls": day.Calls, "errors": day.Errors, "p50_ms": day.P50, "p95_ms": day.P95},
			"month": map[string]any{"spent": month.Spent, "budget": month.Budget, "projected": month.Projected, "next_30_days": month.Next30, "per_day": month.PerDay},
			"usage": usage, "calls": calls, "matched": total,
		})
		return
	}

	failedHref := f.url("failed", "1")
	if f.Failed {
		failedHref = f.url("failed", "")
	}
	chips := []chip{{Name: "failed only", Href: failedHref, Kind: map[bool]string{true: "bad"}[f.Failed]}}
	ranges := []chip{{Name: "any time", Href: f.url("range", ""), Kind: map[bool]string{true: "brass"}[f.Range == ""]}}
	for _, rg := range callRanges {
		ranges = append(ranges, chip{Name: rg.Name, Href: f.url("range", rg.Name), Kind: map[bool]string{true: "brass"}[f.Range == rg.Name]})
	}
	callers := []chip{{Name: "every caller", Href: f.url("caller", ""), Kind: map[bool]string{true: "brass"}[f.Caller == ""]}}
	for _, u := range usage {
		name := u.Caller
		if name == "" {
			name = "unknown"
		}
		callers = append(callers, chip{Name: name, Href: f.url("caller", u.Caller), Kind: map[bool]string{true: "brass"}[f.Caller == u.Caller && f.Caller != ""]})
	}

	var cost float64
	var ms []int64
	for _, c := range calls {
		cost += float64(c.Cost) / 1e6
		ms = append(ms, c.MS)
	}
	slices.Sort(ms)

	lab := "LLM · calls"
	for _, part := range []string{f.Caller, map[bool]string{true: "failed"}[f.Failed], f.Range, f.DayText} {
		if part != "" {
			lab += " · " + part
		}
	}
	head := template.HTML(template.HTMLEscapeString(count(day.Calls, "call", "calls")) + " today")
	if day.Errors > 0 {
		head += template.HTML(`, <em class="bad">` + count(day.Errors, "failed", "failed") + `</em>.`)
	} else {
		head += "."
	}
	filtered := f.Caller != "" || f.Failed || f.Range != "" || f.DayText != "" || f.Q != "" || f.ID > 0
	s.render(w, "calls.html", map[string]any{"Calls": calls, "F": f, "Total": total, "Filtered": filtered,
		"Chips": chips, "Ranges": ranges, "Callers": callers, "Cost": cost, "P95": pct(ms, 0.95),
		"Lab": lab, "Title": "Calls", "Headline": head, "Suite": s.chrome("calls", day, month)})
}

func (s *site) keysPage(w http.ResponseWriter, r *http.Request) {
	keys, err := s.store.Keys()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	// A key is shown once, on the redirect straight after it's made, because
	// nothing here can produce it again.
	fresh := ""
	if c, err := r.Cookie(newKeyCookie); err == nil {
		fresh = c.Value
		http.SetCookie(w, &http.Cookie{
			Name: newKeyCookie, Value: "", Path: "/",
			Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1,
		})
	}
	live := 0
	for _, k := range keys {
		if !k.Revoked {
			live++
		}
	}
	month, _ := s.month(time.Now())
	head := template.HTML(template.HTMLEscapeString(count(live, "live key", "live keys")) + ", <em>one per caller</em>.")
	s.render(w, "keys.html", map[string]any{"Title": "Keys", "Keys": keys, "Fresh": fresh, "Headline": head, "Use": s.keyUses(month, time.Now()),
		"Suite": s.chrome("keys", s.dayStats(time.Now()), month)})
}

// keyCap sets a key's monthly cap in dollars, and an empty or zero one takes
// the cap off.
func (s *site) keyCap(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	v := strings.TrimPrefix(strings.TrimSpace(r.FormValue("cap")), "$")
	dollars := 0.0
	if v != "" {
		var err error
		dollars, err = strconv.ParseFloat(v, 64)
		if err != nil || dollars < 0 || math.IsInf(dollars, 0) || dollars > 1e6 {
			http.Error(w, "a cap is a dollar amount, or empty for none", http.StatusBadRequest)
			return
		}
	}
	if err := s.store.SetCap(id, int64(math.Round(dollars*1e6))); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/keys", http.StatusSeeOther)
}

// compact is a token count the way a bar's label has room for, like 41.2M.
func compact(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 10_000:
		return fmt.Sprintf("%.0fk", float64(n)/1e3)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return strconv.FormatInt(n, 10)
}

// asked is the last thing the user said in a call, in full, which is the
// question a person reading the log is after.
func asked(messages string) string {
	var msgs []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal([]byte(messages), &msgs) != nil {
		return messages
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != "user" {
			continue
		}
		var text string
		if json.Unmarshal(msgs[i].Content, &text) == nil {
			return text
		}
		var parts []struct {
			Type, Text string
		}
		if json.Unmarshal(msgs[i].Content, &parts) == nil {
			var b strings.Builder
			for _, p := range parts {
				if p.Text != "" {
					b.WriteString(p.Text + "\n")
				}
			}
			return strings.TrimSpace(b.String())
		}
		return string(msgs[i].Content)
	}
	return ""
}
