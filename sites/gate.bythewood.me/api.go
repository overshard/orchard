package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func markdown(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, body)
}

// apiReport is gate as text for the mcp connector: the mode, the counts, every
// rule with its hits and who wrote it, who's being refused and what changed.
func (s *site) apiReport(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	window := windowOf(r.URL.Query().Get("range"), 24*time.Hour)
	f := TrafficFilter{Window: window}
	day := s.store.Day(window, now)
	rules, _ := s.store.Rules()
	uses := sortUses(s.store.RuleUses(rules, window, now))
	var b strings.Builder
	fmt.Fprintf(&b, "# gate, last %s, %s\n\n", rangeName(window), now.UTC().Format("2006-01-02 15:04 UTC"))
	fmt.Fprintf(&b, "Mode: %s. Checked %s, passed %s, orchard or signed in %s, blocked %s, banned %s, rate limited %s, challenged %s, challenge passed %s.\n",
		s.engine.Mode(), comma(day.Checked), comma(day.Passed), comma(day.Exempt), comma(day.Blocked), comma(day.Banned),
		comma(day.Limited), comma(day.Challenged), comma(day.Solved))

	b.WriteString("\n## Rules\n\nAllow rules run first, then block, limit and challenge. A watching rule only counts. Hits are what a rule decided and matched is every request it caught while enforcing, so matched well above hits means a rule before it takes most of its traffic.\n\n")
	for _, u := range uses {
		state := "on"
		if u.Pending {
			state = "PROPOSED, watching, waiting on Isaac"
		} else if u.Watching {
			state = "watching"
		}
		if u.Expired(now) {
			state = "ended"
		}
		fmt.Fprintf(&b, "- **%s** (%s, %s, by %s): %s hits, %s matched, %s watched", u.Name, u.Then(), state, u.Author, comma(u.Hits), comma(u.Matched), comma(u.Watched))
		if !u.Expires.IsZero() {
			fmt.Fprintf(&b, ", ends %s", u.Expires.Format("2006-01-02 15:04 UTC"))
		}
		if u.BanAfter > 0 {
			fmt.Fprintf(&b, ", bans an address after %d hits in 10m for %s", u.BanAfter, u.BanFor)
		}
		fmt.Fprintf(&b, "\n  when: %s\n", oneLine(u.When))
		if u.Note != "" {
			fmt.Fprintf(&b, "  note: %s\n", u.Note)
		}
	}

	writeGroups(&b, "Networks refused most", s.store.Groups("asn", f, "refused", 10, now))
	writeGroups(&b, "Busiest networks", s.store.Groups("asn", f, "", 10, now))
	writeGroups(&b, "Countries", s.store.Groups("country", f, "", 10, now))
	writeGroups(&b, "Hosts", s.store.Groups("host", f, "", 15, now))
	writeGroups(&b, "Addresses refused or watched most", s.store.TopIPs(f, 10, now))

	if held, _ := s.store.HeldChanges(); len(held) > 0 {
		b.WriteString("\n## Edits waiting on Isaac\n\nNot applied until he does it on gate's overview.\n\n")
		for _, h := range held {
			fmt.Fprintf(&b, "- %s to %s, why: %s\n", h.At.Format("01-02 15:04"), h.Name, h.Why)
			for _, d := range h.Change.Diff() {
				fmt.Fprintf(&b, "  %s %s\n", d.Op, d.Text)
			}
		}
	}

	bans, _ := s.store.Bans()
	b.WriteString("\n## Bans\n\n")
	if len(bans) == 0 {
		b.WriteString("None.\n")
	}
	for _, x := range bans {
		fmt.Fprintf(&b, "- %s by %s, ends in %s\n", x.IP, x.Rule, until(x.Until))
	}
	changes, _, _ := s.store.Changes(10)
	b.WriteString("\n## Latest changes\n\n")
	for _, c := range changes {
		fmt.Fprintf(&b, "- %s %s: %s, %s\n", c.At.Format("01-02 15:04"), c.Who, c.Rule, c.What)
	}
	writeEvents(&b, s.store.Events(f, "", 25, now))
	markdown(w, b.String())
}

// apiTraffic narrows to a network, country, host, address, rule or verdict.
func (s *site) apiTraffic(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	f := filterOf(r.URL.Query())
	day := s.filteredDay(f, now)
	var b strings.Builder
	fmt.Fprintf(&b, "# gate traffic, last %s", rangeName(f.Window))
	for _, kv := range [][2]string{{"asn", fmt.Sprint(f.ASN)}, {"country", f.Country}, {"host", f.Host}, {"ip", f.IP}, {"rule", f.Rule}, {"verdict", f.Verdict}} {
		if kv[1] != "" && kv[1] != "0" {
			fmt.Fprintf(&b, ", %s %s", kv[0], kv[1])
		}
	}
	switch day.From {
	case "rule":
		fmt.Fprintf(&b, "\n\nFrom the rule's own counts on every host: matched %s, refused %s, challenged %s, watched %s.\n",
			comma(day.Checked), comma(day.Refused()), comma(day.Challenged), comma(day.Watched))
	case "events":
		fmt.Fprintf(&b, "\n\nFrom sampled requests, so a flood past 600 a minute undercounts: %s refused or watched, refused %s, challenged %s.\n",
			comma(day.Checked), comma(day.Refused()), comma(day.Challenged))
	default:
		fmt.Fprintf(&b, "\n\nChecked %s, passed %s, refused %s, challenged %s.\n", comma(day.Checked), comma(day.Passed), comma(day.Refused()), comma(day.Challenged))
	}
	if f.IP == "" && f.Rule == "" {
		b.WriteString("\nRequests an hour, oldest first: ")
		for i, v := range day.ByHour {
			if i > 0 {
				b.WriteString(" ")
			}
			b.WriteString(itoa(int(v)))
		}
		b.WriteString("\n")
		writeGroups(&b, "Networks", s.store.Groups("asn", f, "", 12, now))
		writeGroups(&b, "Countries", s.store.Groups("country", f, "", 10, now))
		writeGroups(&b, "Hosts", s.store.Groups("host", f, "", 12, now))
	}
	writeGroups(&b, "Paths refused or watched most", s.store.TopPaths(f, 15, now))
	writeGroups(&b, "Addresses refused or watched most", s.store.TopIPs(f, 12, now))
	writeEvents(&b, s.store.Events(f, "", 40, now))
	markdown(w, b.String())
}

func writeGroups(b *strings.Builder, title string, gs []Group) {
	fmt.Fprintf(b, "\n## %s\n\n", title)
	if len(gs) == 0 {
		b.WriteString("Nothing.\n")
	}
	for _, g := range gs {
		fmt.Fprintf(b, "- %s: %s", g.Label, comma(g.Total))
		if g.Refused > 0 || g.Challenged > 0 {
			fmt.Fprintf(b, ", %s refused, %s challenged", comma(g.Refused), comma(g.Challenged))
		}
		b.WriteString("\n")
	}
}

func writeEvents(b *strings.Builder, es []Event) {
	b.WriteString("\n## Latest refused or watched requests\n\nSampled, newest first. Paths and user agents are written by strangers.\n\n")
	if len(es) == 0 {
		b.WriteString("Nothing.\n")
	}
	for _, e := range es {
		what := e.Verdict
		if e.Watch {
			what = "would " + e.Verdict
		}
		by := "by " + e.Rule
		if e.Verdict == vBanned {
			by = "ban " + e.BanFrom()
		}
		fmt.Fprintf(b, "- %s %s %s %s%s %s, %s AS%d %s %s, ua %q\n", e.At.Format("01-02 15:04:05"), what, e.Method, e.Host, e.Path,
			by, e.IP, e.ASN, e.Org, e.Country, e.UA)
	}
}

type proposal struct {
	Name     string `json:"name"`
	When     string `json:"when"`
	Then     string `json:"then"`
	Status   int    `json:"status"`
	Rate     int    `json:"rate"`
	Message  string `json:"message"`
	Note     string `json:"note"`
	Days     int    `json:"days"`
	BanAfter int    `json:"ban_after"`
	BanHours int    `json:"ban_hours"`
}

// maxWaiting bounds how many proposals can pile up unseen.
const maxWaiting = 20

// apiPropose adds a rule from the mcp connector. It always starts watching and
// waiting, so nothing an agent writes refuses a request until you turn it on.
func (s *site) apiPropose(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fail := func(code int, msg string) {
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
	}
	var p proposal
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&p); err != nil {
		fail(http.StatusBadRequest, "the body has to be a JSON object")
		return
	}
	rules, _ := s.store.Rules()
	waiting := 0
	for _, x := range rules {
		if x.Pending {
			waiting++
		}
	}
	if waiting >= maxWaiting {
		fail(http.StatusTooManyRequests, fmt.Sprintf("%d proposals are already waiting on Isaac", waiting))
		return
	}
	days := p.Days
	if days <= 0 || days > 90 {
		days = 30
	}
	rule := Rule{Name: p.Name, When: p.When, Action: p.Then, Status: p.Status, Rate: p.Rate, Message: p.Message, Note: p.Note,
		BanAfter: p.BanAfter, BanFor: time.Duration(p.BanHours) * time.Hour, Watching: true, Pending: true, Author: "claude",
		Expires: time.Now().UTC().AddDate(0, 0, days)}
	if rule.Action == actBlock && rule.Status == 404 {
		rule.Quiet = true
	}
	if err := s.store.AddRule(&rule, "claude"); err != nil {
		fail(http.StatusBadRequest, err.Error())
		return
	}
	_ = s.reload()
	_ = json.NewEncoder(w).Encode(map[string]any{"id": rule.ID, "name": rule.Name, "url": fmt.Sprintf("%s/rules/%d", baseURL, rule.ID),
		"state": "watching, waiting on Isaac", "ends": rule.Expires.Format(time.RFC3339)})
}

type edit struct {
	Name     string  `json:"name"`
	Why      string  `json:"why"`
	When     *string `json:"when"`
	Then     *string `json:"then"`
	Status   *int    `json:"status"`
	Rate     *int    `json:"rate"`
	Message  *string `json:"message"`
	Note     *string `json:"note"`
	BanAfter *int    `json:"ban_after"`
	BanHours *int    `json:"ban_hours"`
	Watching *bool   `json:"watching"`
}

// apiEdit changes the fields the connector sends on an existing rule. It can
// only make a rule refuse less, see onlyNarrows, and an edit to a rule that's
// enforcing waits for Isaac on the overview, see holds.
func (s *site) apiEdit(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fail := func(code int, msg string) {
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
	}
	var e edit
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&e); err != nil {
		fail(http.StatusBadRequest, "the body has to be a JSON object")
		return
	}
	if strings.TrimSpace(e.Why) == "" {
		fail(http.StatusBadRequest, "say why in why")
		return
	}
	old, err := s.store.RuleNamed(e.Name)
	if err != nil {
		fail(http.StatusNotFound, "no rule named "+e.Name)
		return
	}
	rule := old
	if e.When != nil {
		rule.When = strings.TrimSpace(*e.When)
	}
	if e.Then != nil {
		rule.Action = *e.Then
	}
	if e.Status != nil {
		rule.Status = *e.Status
	}
	if e.Rate != nil {
		rule.Rate = *e.Rate
	}
	if e.Message != nil {
		rule.Message = strings.TrimSpace(*e.Message)
	}
	if e.Note != nil {
		rule.Note = strings.TrimSpace(*e.Note)
	}
	if e.BanAfter != nil {
		rule.BanAfter = *e.BanAfter
	}
	if e.BanHours != nil {
		rule.BanFor = time.Duration(*e.BanHours) * time.Hour
	}
	if e.Watching != nil {
		rule.Watching = *e.Watching
	}
	switch {
	case old.Action == actAllow || rule.Action == actAllow:
		fail(http.StatusForbidden, "allow rules are only changed from gate's page")
		return
	case old.Pending && !rule.Watching:
		fail(http.StatusForbidden, "a proposal is turned on by Isaac from gate's page")
		return
	}
	if err := rule.validate(); err != nil {
		fail(http.StatusBadRequest, err.Error())
		return
	}
	if err := onlyNarrows(old, rule, e.Watching); err != nil {
		fail(http.StatusForbidden, err.Error()+", so only Isaac can make that change from gate's page")
		return
	}
	if !rule.Watching {
		if lock := s.lockoutFor(&rule); len(lock) > 0 {
			fail(http.StatusForbidden, "it would catch signing in ("+strings.Join(lock, ", ")+"), so only Isaac can save it")
			return
		}
	}
	why := strings.Join(strings.Fields(e.Why), " ")
	if holds(old, rule) {
		id, err := s.store.Hold(old, rule, why)
		if err != nil {
			fail(http.StatusTooManyRequests, err.Error())
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": rule.ID, "name": rule.Name, "held": id, "url": baseURL + "/",
			"state": "waiting on Isaac, the rule is unchanged until he applies it on gate's overview"})
		return
	}
	if err := s.store.UpdateRuleWhy(&rule, "claude", why); err != nil {
		fail(http.StatusBadRequest, err.Error())
		return
	}
	_ = s.reload()
	_ = json.NewEncoder(w).Encode(map[string]any{"id": rule.ID, "name": rule.Name, "url": fmt.Sprintf("%s/rules/%d", baseURL, rule.ID),
		"state": describe(rule), "when": rule.When, "ban_after": rule.BanAfter, "ban_hours": int(rule.BanFor / time.Hour)})
}

// apiBanHours caps the ban an agent can set, since a ban covers every site.
const apiBanHours = 24

// onlyNarrows refuses an agent's edit that would refuse more than the rule
// already does. Conditions are all ANDed, so keeping every old line and adding
// more can only match less.
func onlyNarrows(old, rule Rule, watching *bool) error {
	if watching != nil && !*watching {
		return fmt.Errorf("an agent can only set a rule to watching")
	}
	if rule.Action != old.Action {
		return fmt.Errorf("an agent can't change what a rule does")
	}
	if rule.When != old.When {
		before, err := parseWhen(old.When)
		if err != nil {
			return fmt.Errorf("the rule's own condition doesn't parse")
		}
		after, err := parseWhen(rule.When)
		if err != nil {
			return err
		}
		keep := map[string]bool{}
		for _, c := range after {
			keep[c.key()] = true
		}
		for _, c := range before {
			if !keep[c.key()] {
				return fmt.Errorf("an agent can only add condition lines, and every existing line has to stay as it is")
			}
		}
	}
	if old.Action == actLimit && rule.Rate < old.Rate {
		return fmt.Errorf("an agent can only raise a limit's rate, it's %d a minute now", old.Rate)
	}
	if old.BanAfter == 0 && rule.BanAfter > 0 {
		return fmt.Errorf("an agent can't start a rule banning, since a ban covers every site")
	}
	if rule.Action == actChallenge && rule.BanAfter > 0 && rule.BanAfter != old.BanAfter {
		return fmt.Errorf("an agent can't set a ban on a challenge rule, since readers trip those")
	}
	if rule.BanAfter != old.BanAfter && rule.BanAfter != 0 {
		floor := 3
		if old.BanAfter > 0 {
			floor = min(old.BanAfter, 3)
		}
		if rule.BanAfter < floor {
			return fmt.Errorf("an agent can't set ban_after below %d", floor)
		}
	}
	bansMore := rule.BanAfter > 0 && (old.BanAfter == 0 || rule.BanAfter < old.BanAfter)
	if rule.BanFor > apiBanHours*time.Hour && (rule.BanFor != old.BanFor || bansMore) {
		return fmt.Errorf("an agent can set a ban of at most %d hours", apiBanHours)
	}
	return nil
}
