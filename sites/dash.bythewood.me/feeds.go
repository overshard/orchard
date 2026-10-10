package main

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"time"
)

// The feeds desk reads Hacker News and Lobsters at each brief slot and writes a
// sentence on what the top comments on the busiest threads argue. The titles are
// on the panel already, and the numbers can say a thread is big but not what
// anyone thinks of it.

const (
	threadsShown   = 15
	threadsRead    = 3
	commentsRead   = 3
	commentRunes   = 280
	hnItemURL      = "https://hacker-news.firebaseio.com/v0/item/%d.json"
	lobstersStoryF = "https://lobste.rs/s/%s.json"
)

type thread struct {
	title, host, discuss string
	points, comments     int
	age                  time.Duration
	said                 []string
}

func hnThreads(ctx context.Context, g *Guard, now time.Time) ([]thread, error) {
	var payload algoliaPayload
	if err := getJSON(ctx, g, "algolia", hackerNewsURL, &payload); err != nil {
		return nil, err
	}
	type hit struct {
		id int64
		t  thread
	}
	var hits []hit
	for _, h := range payload.Hits {
		var id int64
		if _, err := fmt.Sscan(h.ObjectID, &id); err != nil || h.Title == "" {
			continue
		}
		hits = append(hits, hit{id, thread{title: h.Title, host: hostOf(h.URL), points: h.Points, comments: h.NumComments,
			age: now.Sub(time.Unix(h.CreatedAtI, 0)), discuss: "https://news.ycombinator.com/item?id=" + h.ObjectID}})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].t.points > hits[j].t.points })
	hits = hits[:min(len(hits), threadsShown)]

	// Firebase lists a story's replies in the order the site ranks them, which
	// Algolia doesn't, so the comments come from there.
	for _, i := range busiest(len(hits), func(i int) int { return hits[i].t.comments }) {
		var story struct {
			Kids []int64 `json:"kids"`
		}
		if err := getJSON(ctx, g, "hnitem", fmt.Sprintf(hnItemURL, hits[i].id), &story); err != nil {
			slog.Warn("brief thread failed", slog.String("component", "brief"), slog.Any("err", err))
			continue
		}
		for _, kid := range story.Kids[:min(len(story.Kids), commentsRead)] {
			var c struct {
				Text    string `json:"text"`
				Deleted bool   `json:"deleted"`
				Dead    bool   `json:"dead"`
			}
			if getJSON(ctx, g, "hnitem", fmt.Sprintf(hnItemURL, kid), &c) == nil && !c.Deleted && !c.Dead {
				if s := commentText(c.Text); s != "" {
					hits[i].t.said = append(hits[i].t.said, s)
				}
			}
		}
	}
	out := make([]thread, len(hits))
	for i, h := range hits {
		out[i] = h.t
	}
	return out, nil
}

func lobstersThreads(ctx context.Context, g *Guard, now time.Time) ([]thread, error) {
	var payload []struct {
		lobstersStory
		ShortID string `json:"short_id"`
	}
	if err := getJSON(ctx, g, "lobsters", lobstersURL, &payload); err != nil {
		return nil, err
	}
	type hit struct {
		id string
		t  thread
	}
	var hits []hit
	for _, s := range payload {
		if s.Title == "" || s.ShortID == "" {
			continue
		}
		t := thread{title: s.Title, host: hostOf(s.URL), points: s.Score, comments: s.CommentCount, discuss: s.CommentsURL, age: 24 * time.Hour}
		if at, err := time.Parse(time.RFC3339, s.CreatedAt); err == nil {
			t.age = now.Sub(at)
		}
		hits = append(hits, hit{s.ShortID, t})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].t.points > hits[j].t.points })
	hits = hits[:min(len(hits), threadsShown)]

	for _, i := range busiest(len(hits), func(i int) int { return hits[i].t.comments }) {
		var story struct {
			Comments []struct {
				Depth        int    `json:"depth"`
				Score        int    `json:"score"`
				CommentPlain string `json:"comment_plain"`
				IsDeleted    bool   `json:"is_deleted"`
			} `json:"comments"`
		}
		if err := getJSON(ctx, g, "lobsters", fmt.Sprintf(lobstersStoryF, hits[i].id), &story); err != nil {
			slog.Warn("brief thread failed", slog.String("component", "brief"), slog.Any("err", err))
			continue
		}
		top := story.Comments[:0]
		for _, c := range story.Comments {
			if c.Depth == 0 && !c.IsDeleted {
				top = append(top, c)
			}
		}
		sort.SliceStable(top, func(a, b int) bool { return top[a].Score > top[b].Score })
		for _, c := range top[:min(len(top), commentsRead)] {
			if s := commentText(c.CommentPlain); s != "" {
				hits[i].t.said = append(hits[i].t.said, s)
			}
		}
	}
	out := make([]thread, len(hits))
	for i, h := range hits {
		out[i] = h.t
	}
	return out, nil
}

// busiest is the indexes of the threads with the most comments, which is where
// a front page's argument is.
func busiest(n int, comments func(int) int) []int {
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return comments(idx[a]) > comments(idx[b]) })
	return idx[:min(n, threadsRead)]
}

func commentText(raw string) string {
	s := html.UnescapeString(tags.ReplaceAllString(strings.ReplaceAll(raw, "<p>", " "), " "))
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > commentRunes {
		s = string(r[:commentRunes]) + "..."
	}
	return s
}

func threadList(ts []thread) string {
	var b strings.Builder
	for i, t := range ts {
		fmt.Fprintf(&b, "[%d] %d points, %d comments, %s old: %s", i+1, t.points, t.comments, ago(t.age), t.title)
		if t.host != "" {
			b.WriteString(" (" + t.host + ")")
		}
		b.WriteString("\n")
	}
	b.WriteString("\nTop comments on the busiest threads:\n")
	for i, t := range ts {
		if len(t.said) == 0 {
			continue
		}
		fmt.Fprintf(&b, "[%d] %s\n", i+1, t.title)
		for _, s := range t.said {
			b.WriteString("- " + s + "\n")
		}
	}
	return b.String()
}

// Room past the asked 15 words so the model finishes the sentence on its own,
// since at a cap near the target it ran into it and left half a clause.
const feedReadMax = 160

func (b *Briefer) compileFeeds(ctx context.Context, slot briefSlot, at time.Time) (Brief, error) {
	now := time.Now()
	brief := Brief{Kind: slot.kind, Title: slot.title(), Slot: at.Unix(),
		Compiled: time.Now().In(easternTime()).Format("15:04 MST")}
	for _, site := range []struct {
		label, name string
		fetch       func(context.Context, *Guard, time.Time) ([]thread, error)
	}{
		{"HN", "Hacker News", hnThreads},
		{"LOBSTERS", "Lobsters", lobstersThreads},
	} {
		ts, err := site.fetch(ctx, b.guard, now)
		if err == nil && len(ts) < 5 {
			err = fmt.Errorf("only %d threads", len(ts))
		}
		if err == nil {
			var pt Point
			pt, err = b.feedRead(ctx, site.name, ts)
			pt.Label = site.label
			if err == nil {
				brief.Points = append(brief.Points, pt)
				brief.Stories += len(ts)
			}
		}
		if err != nil {
			slog.Warn("brief feed read failed", slog.String("component", "brief"), slog.String("feed", site.label), slog.Any("err", err))
		}
	}
	if len(brief.Points) == 0 {
		return Brief{}, fmt.Errorf("no feed reads")
	}
	return brief, nil
}

func (b *Briefer) feedRead(ctx context.Context, name string, ts []thread) (Point, error) {
	system := `You write one sentence for one reader on what a tech community is arguing about right now, from its front page and the top comments on its busiest threads. The titles are shown to the reader already, so the sentence is about what the commenters think.
- "read" is one sentence of at most 15 words. Pick the thread whose comments say the most, open with "On" and its subject in a few words, then say what its top commenters argue, in your own words, like "On <subject>, commenters <what they argue>."
- Use only what the listed comments say. No outside knowledge, no opinion of your own, no hype words like buzzing, hot, or exploding.
- Do not put story numbers or point counts in the text. Cite the number of the thread you wrote about in "sources".`
	user := fmt.Sprintf("The %s front page, most points first:\n%s", name, threadList(ts))
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"read":    map[string]any{"type": "string", "maxLength": feedReadMax},
			"sources": map[string]any{"type": "array", "minItems": 1, "maxItems": 1, "items": map[string]any{"type": "integer"}},
		},
		"required":             []string{"read", "sources"},
		"additionalProperties": false,
	}
	var out struct {
		Read    string `json:"read"`
		Sources []int  `json:"sources"`
	}
	if err := b.model.Structured(ctx, system, user, schema, 200, &out); err != nil {
		return Point{}, err
	}
	read := whole(out.Read, feedReadMax)
	if out.Read != "" && read == "" {
		slog.Info("brief line cut", slog.String("component", "brief"), slog.String("feed", name), slog.String("raw", out.Read))
	}
	pt := Point{Text: read}
	for _, n := range out.Sources {
		if n < 1 || n > len(ts) || slices.ContainsFunc(pt.Links, func(l Link) bool { return l.URL == ts[n-1].discuss }) {
			continue
		}
		// Named by its row on the panel, which is the same points order.
		src := "THREAD"
		if n <= storiesShown {
			src = fmt.Sprintf("NO. %d", n)
		}
		pt.Links = append(pt.Links, Link{Source: src, URL: ts[n-1].discuss, Title: ts[n-1].title})
	}
	if pt.Text == "" {
		return Point{}, fmt.Errorf("no read")
	}
	return pt, nil
}
