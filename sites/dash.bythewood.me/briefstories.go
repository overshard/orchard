package main

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

// lean is AllSides' rating of each outlet, L, C or R. It's never shown to the
// model, only counted per story, so a story one side alone is covering gets
// marked as that instead of left out.
type briefFeed struct{ name, lean, endpoint, url string }

// AP and Reuters have no public feeds of their own, so they come through Google
// News searches restricted to their domains.
func googleNews(site string) string {
	return "https://news.google.com/rss/search?q=when:1d+site:" + site + "&hl=en-US&gl=US&ceid=US:en"
}

// story is one item off a feed.
type story struct {
	title, desc, url, source, lean string
	at                             time.Time
	words                          []string
}

// cluster is one event as every outlet that carried it told it.
type cluster struct {
	stories []story
	members []int
	newest  time.Time
	session time.Time
}

// distinctive drops the words a big share of the pile has in its titles. On
// a day the administration is in every headline, trump, admin and feder are
// in half of them, and two unrelated stories share four words without trying.
func distinctive(all []story) [][]string {
	df := map[string]int{}
	for _, s := range all {
		for _, w := range s.words {
			df[w]++
		}
	}
	common := max(4, len(all)/15)
	out := make([][]string, len(all))
	for i, s := range all {
		for _, w := range s.words {
			if df[w] <= common {
				out[i] = append(out[i], w)
			}
		}
	}
	return out
}

// Three rare words in common is one event. The common ones are already gone,
// so three of what is left is a lot.
func sameEvent(a, b []string) bool {
	shared := 0
	for _, w := range a {
		if slices.Contains(b, w) {
			shared++
		}
	}
	return shared >= 3
}

// A Google News search hands back a hundred items, so each feed is held to
// its newest few or AP and Reuters would be most of the pile.
const perFeed = 25

// gather reads every feed and keeps what landed between since and now.
func gather(ctx context.Context, g *Guard, feeds []briefFeed, since, now time.Time) []story {
	seen := map[string]bool{}
	var all []story
	for _, feed := range feeds {
		items, err := fetchRSS(ctx, g, feed.endpoint, feed.url)
		if err != nil {
			slog.Warn("brief feed failed", slog.String("component", "brief"), slog.String("feed", feed.url), slog.Any("err", err))
			continue
		}
		var mine []story
		for _, it := range items.Channel.Items {
			title := strings.TrimSpace(html.UnescapeString(it.Title))
			desc := blurb(it.Description)
			if feed.endpoint == "googlenews" {
				// Its titles end in " - AP News" and its description is the
				// title again wrapped in a link.
				if i := strings.LastIndex(title, " - "); i > 0 {
					title = title[:i]
				}
				desc = ""
			}
			if title == "" || it.Link == "" || promotional(title) || sidebar(title) || clip(it.Link) {
				continue
			}
			key := it.GUID
			if key == "" {
				key = it.Link
			}
			if seen[key] || seen[titleKey(title)] {
				continue
			}
			seen[key], seen[titleKey(title)] = true, true

			at, err := parseRSSTime(it.PubDate)
			if err != nil || at.Before(since) || at.After(now.Add(10*time.Minute)) {
				continue
			}
			if strings.HasPrefix(desc, title) {
				desc = ""
			}
			mine = append(mine, story{title: title, desc: desc, url: it.Link, source: feed.name,
				lean: feed.lean, at: at, words: significant(title)})
		}
		sort.SliceStable(mine, func(i, j int) bool { return mine[i].at.After(mine[j].at) })
		all = append(all, mine[:min(len(mine), perFeed)]...)
	}
	return all
}

// clusterStories groups the same event across outlets and ranks the groups by
// how many outlets carried them, which is the closest thing to "this
// mattered" that does not depend on any one newsroom's judgement.
func clusterStories(all []story, limit int) []cluster {
	return clusterBy(all, limit, func(story) time.Time { return time.Time{} })
}

// clusterBy never groups two stories from different sessions, since Reuters
// runs "S&P 500 flat as chipmakers fall" two days running and one cluster
// would then explain both closes.
func clusterBy(all []story, limit int, session func(story) time.Time) []cluster {
	sort.SliceStable(all, func(i, j int) bool { return all[i].at.After(all[j].at) })
	keys := distinctive(all)
	var out []cluster
	for n, s := range all {
		day := session(s)
		i := slices.IndexFunc(out, func(c cluster) bool {
			return c.session.Equal(day) && slices.ContainsFunc(c.members, func(m int) bool { return sameEvent(keys[m], keys[n]) })
		})
		if i < 0 {
			out = append(out, cluster{newest: s.at, session: day})
			i = len(out) - 1
		}
		out[i].stories = append(out[i].stories, s)
		out[i].members = append(out[i].members, n)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := len(out[i].outlets()), len(out[j].outlets())
		if a != b {
			return a > b
		}
		return out[i].newest.After(out[j].newest)
	})
	return out[:min(len(out), limit)]
}

func (c cluster) outlets() []string {
	var names []string
	for _, s := range c.stories {
		if !slices.Contains(names, s.source) {
			names = append(names, s.source)
		}
	}
	return names
}

// lead is the telling the model reads, the title sharing the most words with
// the rest, so one outlet folding in a second story doesn't speak for all. A
// center outlet's is favoured as the least framed.
func (c cluster) lead() story {
	score := func(s story) int {
		n := 0
		for _, w := range s.words {
			for _, o := range c.stories {
				if o.url != s.url && slices.Contains(o.words, w) {
					n++
				}
			}
		}
		if s.lean == "C" {
			n += 2
		}
		if s.desc != "" {
			n++
		}
		return n
	}
	best := c.stories[0]
	for _, s := range c.stories[1:] {
		if score(s) > score(best) {
			best = s
		}
	}
	return best
}

var tags = regexp.MustCompile(`<[^>]*>`)

// blurb is the feed's own description cut to a sentence or two. CBS and PBS
// wrap theirs in markup.
func blurb(desc string) string {
	s := html.UnescapeString(tags.ReplaceAllString(desc, " "))
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 240 {
		s = string(r[:240])
		if i := strings.LastIndex(s, " "); i > 160 {
			s = s[:i]
		}
		s += "..."
	}
	return s
}

func outletNames(cs []cluster) string {
	var names []string
	for _, c := range cs {
		for _, n := range c.outlets() {
			if !slices.Contains(names, n) {
				names = append(names, n)
			}
		}
	}
	sort.Strings(names)
	return strings.Join(names, " / ")
}

func storyCount(cs []cluster) int {
	n := 0
	for _, c := range cs {
		n += len(c.stories)
	}
	return n
}

// clusterList numbers the events from 1 for the model to cite by. The lean of
// each outlet stays out of it, since a model told which side a story came from
// starts writing about the sides.
func clusterList(cs []cluster, now time.Time) string {
	var b strings.Builder
	for i, c := range cs {
		l := c.lead()
		names := c.outlets()
		fmt.Fprintf(&b, "[%d] %d outlet", i+1, len(names))
		if len(names) > 1 {
			b.WriteString("s")
		}
		fmt.Fprintf(&b, " (%s), %s ago", strings.Join(names, ", "), ago(now.Sub(c.newest)))
		if !c.session.IsZero() {
			fmt.Fprintf(&b, ", %s's session", c.session.Format("Monday"))
		}
		b.WriteString(": " + l.title)
		if l.desc != "" {
			b.WriteString(" | " + l.desc)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func ago(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%dm", max(int(d.Minutes()), 1))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}

func newBrief(slot briefSlot, at time.Time, events []cluster) Brief {
	return Brief{Kind: slot.kind, Title: slot.title(), Slot: at.Unix(), Stories: storyCount(events),
		Outlets: outletNames(events), Compiled: time.Now().In(easternTime()).Format("15:04 MST")}
}

// cite turns the model's event numbers into links, one per outlet with the
// center ones first, and counts who carried the point on each side. A number
// that is not an event is dropped.
func cite(events []cluster, nums []int) Point {
	var pt Point
	var picked []story
	for _, n := range nums {
		if n < 1 || n > len(events) {
			continue
		}
		for _, s := range events[n-1].stories {
			if !slices.ContainsFunc(picked, func(p story) bool { return p.source == s.source }) {
				picked = append(picked, s)
			}
		}
	}
	sort.SliceStable(picked, func(i, j int) bool { return picked[i].lean == "C" && picked[j].lean != "C" })

	sides := map[string]int{}
	for _, s := range picked {
		sides[s.lean]++
	}
	for _, s := range picked[:min(len(picked), 4)] {
		pt.Links = append(pt.Links, Link{Source: s.source, URL: s.url, Title: s.title})
	}
	if len(picked) > 0 {
		pt.Coverage = fmt.Sprintf("L%d C%d R%d", sides["L"], sides["C"], sides["R"])
	}
	// Ground News' blindspot: a story only one side's outlets are running is
	// marked as that and kept, since dropping it would be its own kind of spin.
	if len(picked) >= 2 && sides["C"] == 0 {
		if sides["R"] == 0 {
			pt.Note = "LEFT-LEANING OUTLETS ONLY"
		} else if sides["L"] == 0 {
			pt.Note = "RIGHT-LEANING OUTLETS ONLY"
		}
	}
	return pt
}
