package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Tick is one bar in an o-ticks row.
type Tick struct {
	H   int
	Cls string
	Tip string
}

// repoStrip is the row of tiles at the top of a repository. Everything from
// the commit history is filled only signed in, since it's where SHAs live.
type repoStrip struct {
	History bool
	Count   int
	Weeks   []Tick
	Recent  int
	Before  int
	Spark   []float64
	Labels  []string

	LastPush    time.Time
	LastBranch  string
	LastSubject string
	PushTicks   []Tick

	Files    int
	Top      []Tick
	TopCount int

	Branches, Tags int
	RefTicks       []Tick

	Size    int64
	Percent int
	Over    bool
	Pack    []Tick

	Release       string
	ReleaseWhen   time.Time
	ReleaseTicks  []Tick
	Mirror        bool
	SyncErr       string
	LastSync      time.Time
	OnlyCopy      bool
	ArchivedUpstr bool
}

// weekLabel names the week that ends i weeks before the last of n.
func weekLabel(now time.Time, n, i int) string {
	end := now.Add(-time.Duration(n-1-i) * 7 * 24 * time.Hour)
	return "week to " + end.UTC().Format("Jan 2")
}

// heights scales counts to tick heights, keeping a zero visible as a stub.
func heights(vals []int, tip func(i, v int) string) []Tick {
	hi := 0
	for _, v := range vals {
		hi = max(hi, v)
	}
	out := make([]Tick, len(vals))
	for i, v := range vals {
		h := 6
		cls := "q"
		if v > 0 && hi > 0 {
			h = 12 + int(88*float64(v)/float64(hi))
			cls = ""
		}
		out[i] = Tick{H: h, Cls: cls, Tip: tip(i, v)}
	}
	return out
}

func plural(n int, one, many string) string {
	return fmt.Sprintf("%d %s", n, pluralize(n, one, many))
}

func (s *site) buildStrip(ctx context.Context, rc *repoContext, paths []string) repoStrip {
	now := time.Now()
	st := repoStrip{
		Branches: len(rc.Branches), Tags: len(rc.Tags),
		Size: rc.Size, Percent: rc.PushPercent, Over: rc.OverLimit,
		Mirror: rc.Meta.Mirror, SyncErr: rc.Meta.LastSyncErr, LastSync: rc.Meta.LastSync,
		OnlyCopy: rc.Meta.UpstreamGone, ArchivedUpstr: rc.Meta.Archived,
		Files: len(paths),
	}

	if rc.SignedIn && !rc.Empty {
		st.History = true
		st.Count = s.store.Count(ctx, rc.Repo, rc.Rev)
		weeks := s.store.Weekly(ctx, rc.Repo, rc.Rev)
		st.Weeks = heights(weeks, func(i, v int) string {
			return weekLabel(now, len(weeks), i) + "\n" + plural(v, "commit", "commits")
		})
		n := len(weeks)
		for i := n - 4; i < n; i++ {
			st.Recent += weeks[i]
		}
		for i := n - 8; i < n-4; i++ {
			st.Before += weeks[i]
		}
		for i := n - 12; i < n; i++ {
			st.Spark = append(st.Spark, float64(weeks[i]))
			st.Labels = append(st.Labels, weekLabel(now, n, i))
		}
	}

	// Branches come newest first.
	pushed := make([]int, 12)
	for _, b := range rc.Branches {
		if st.LastPush.IsZero() || b.When.After(st.LastPush) {
			st.LastPush, st.LastBranch, st.LastSubject = b.When, b.Name, b.Subject
		}
		if i := 11 - int(now.Sub(b.When)/(7*24*time.Hour)); i >= 0 && i < 12 {
			pushed[i]++
		}
	}
	st.PushTicks = heights(pushed, func(i, v int) string {
		return weekLabel(now, 12, i) + "\n" + plural(v, "branch", "branches") + " moved"
	})

	top := map[string]int{}
	for _, p := range paths {
		dir, _, nested := strings.Cut(p, "/")
		if !nested {
			dir = "."
		}
		top[dir]++
	}
	names := make([]string, 0, len(top))
	for k := range top {
		names = append(names, k)
	}
	sort.Slice(names, func(i, j int) bool { return top[names[i]] > top[names[j]] })
	st.TopCount = len(names)
	if len(names) > 16 {
		names = names[:16]
	}
	counts := make([]int, len(names))
	for i, n := range names {
		counts[i] = top[n]
	}
	st.Top = heights(counts, func(i, v int) string {
		if names[i] == "." {
			return "top level\n" + plural(v, "file", "files")
		}
		return names[i] + "/\n" + plural(v, "file", "files")
	})

	for _, b := range rc.Branches {
		st.RefTicks = append(st.RefTicks, Tick{H: 100, Tip: "branch " + b.Name})
	}
	for i, t := range rc.Tags {
		if i == 24 {
			break
		}
		st.RefTicks = append(st.RefTicks, Tick{H: 50, Cls: "q", Tip: "tag " + t.Name})
	}

	const packTicks = 20
	filled := int(math.Round(float64(rc.PushPercent) / 100 * packTicks))
	for i := range packTicks {
		t := Tick{H: 100, Cls: "none"}
		if i < filled {
			t.Cls = ""
			if rc.OverLimit {
				t.Cls = "bad"
			} else if rc.PushPercent >= 75 {
				t.Cls = "warn"
			}
		}
		t.Tip = fmt.Sprintf("%d%% of one push", (i+1)*100/packTicks)
		st.Pack = append(st.Pack, t)
	}

	// Tags come newest first too.
	if len(rc.Tags) > 0 {
		st.Release, st.ReleaseWhen = rc.Tags[0].Name, rc.Tags[0].When
		months := make([]int, 12)
		var tagged [12][]string
		for _, t := range rc.Tags {
			i := 11 - monthsBetween(t.When, now)
			if i >= 0 && i < 12 {
				months[i]++
				tagged[i] = append(tagged[i], t.Name)
			}
		}
		st.ReleaseTicks = heights(months, func(i, v int) string {
			label := now.AddDate(0, i-11, 0).UTC().Format("Jan 2006")
			if v == 0 {
				return label + "\nno tags"
			}
			return label + "\n" + strings.Join(tagged[i], ", ")
		})
	}
	return st
}

func monthsBetween(a, b time.Time) int {
	a, b = a.UTC(), b.UTC()
	return (b.Year()-a.Year())*12 + int(b.Month()) - int(a.Month())
}

// files is every path at a branch or tag, for go to file. It's only names, so
// it's public wherever the tree is.
func (s *site) files(w http.ResponseWriter, r *http.Request) {
	rc, ok := s.resolveRepo(w, r)
	if !ok {
		return
	}
	if rc.Empty || !rc.public(rc.Rev) {
		http.NotFound(w, r)
		return
	}
	paths, err := s.store.Paths(r.Context(), rc.Repo, rc.Rev)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, map[string]any{"paths": paths, "capped": len(paths) == maxPaths})
}

// lastCommits answers the file table's last commit column after the tree has
// painted, since walking history for it is the slow part of a listing.
func (s *site) lastCommits(w http.ResponseWriter, r *http.Request) {
	rc, ok := s.resolveRepo(w, r)
	if !ok {
		return
	}
	if !rc.SignedIn || rc.Empty {
		w.Header().Set("Cache-Control", "no-store")
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	dir := strings.Trim(r.PathValue("path"), "/")
	entries, err := s.store.Tree(ctx, rc.Repo, rc.Rev, dir)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name)
	}
	found, err := s.store.LastCommits(ctx, rc.Repo, rc.Rev, dir, names)
	if err != nil {
		slog.Info("last commits failed", slog.String("repo", rc.Repo.Name), slog.Any("err", err))
		found = map[string]LastCommit{}
	}
	type row struct {
		LastCommit
		Ago string `json:"ago"`
	}
	out := make(map[string]row, len(found))
	for k, c := range found {
		out[k] = row{c, shortAgo(c.When)}
	}
	writeJSON(w, map[string]any{"entries": out})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Info("write json", slog.Any("err", err))
	}
}

// shortAgo is humanTime cut down for a narrow column, like 3d or 5mo.
func shortAgo(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/24/7))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/24/30))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/24/365))
	}
}

// dayGroup is one day's commits on the log.
type dayGroup struct {
	Day     string
	Commits []Commit
}

// byDay groups a log by the UTC day each commit was made, newest first as git
// gave them.
func byDay(commits []Commit) []dayGroup {
	var out []dayGroup
	for _, c := range commits {
		day := c.Commit.UTC().Format("Mon, Jan 2 2006")
		if len(out) == 0 || out[len(out)-1].Day != day {
			out = append(out, dayGroup{Day: day})
		}
		out[len(out)-1].Commits = append(out[len(out)-1].Commits, c)
	}
	return out
}
