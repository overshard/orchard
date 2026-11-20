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

	b.WriteString("\n## Rules\n\nAllow rules run first, then block, limit and challenge. A watching rule only counts.\n\n")
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
		fmt.Fprintf(&b, "- **%s** (%s, %s, by %s): %s hits, %s watched", u.Name, u.Then(), state, u.Author, comma(u.Hits), comma(u.Watched))
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
	fmt.Fprintf(&b, "\n\nChecked %s, passed %s, refused %s, challenged %s.\n", comma(day.Checked), comma(day.Passed), comma(day.Refused()), comma(day.Challenged))
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
		fmt.Fprintf(b, "- %s %s %s %s%s by %s, %s AS%d %s %s, ua %q\n", e.At.Format("01-02 15:04:05"), what, e.Method, e.Host, e.Path,
			e.Rule, e.IP, e.ASN, e.Org, e.Country, e.UA)
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
