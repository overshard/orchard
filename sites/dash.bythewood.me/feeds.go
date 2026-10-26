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
// read of the whole front page, what runs through it and what the commenters
// make of it. The numbers can say a thread is big but not what the day is like.

const (
	threadsShown = 15
	threadsRead  = 5
	commentsRead = 3
	// A thread's own read wants more of the room than the front page read does.
	commentsThread = 8
	commentRunes   = 280
	hnItemURL      = "https://hacker-news.firebaseio.com/v0/item/%d.json"
	lobstersStoryF = "https://lobste.rs/s/%s.json"
)

type thread struct {
	title, host, link, discuss string
	points, comments           int
	age                        time.Duration
	said                       []string
	// One of the busiest, whose comments the front page read sees.
	busy bool
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
		hits = append(hits, hit{id, thread{title: h.Title, host: hostOf(h.URL), link: h.URL, points: h.Points, comments: h.NumComments,
			age: now.Sub(time.Unix(h.CreatedAtI, 0)), discuss: "https://news.ycombinator.com/item?id=" + h.ObjectID}})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].t.points > hits[j].t.points })
	hits = hits[:min(len(hits), threadsShown)]

	for _, i := range busiest(len(hits), func(i int) int { return hits[i].t.comments }) {
		hits[i].t.busy = true
	}
	for i := range hits {
		if i >= storiesShown && !hits[i].t.busy {
			continue
		}
		said, err := hnComments(ctx, g, hits[i].id, commentsThread)
		if err != nil {
			slog.Warn("brief thread failed", slog.String("component", "brief"), slog.Any("err", err))
		}
		hits[i].t.said = said
	}
	out := make([]thread, len(hits))
	for i, h := range hits {
		out[i] = h.t
	}
	return out, nil
}

// hnComments is a story's top level comments in the order the site ranks them.
// Firebase lists replies in that order and Algolia doesn't, so they come from
// there, at one request for the story and one per comment.
func hnComments(ctx context.Context, g *Guard, id int64, n int) ([]string, error) {
	var story struct {
		Kids []int64 `json:"kids"`
	}
	if err := getJSON(ctx, g, "hnitem", fmt.Sprintf(hnItemURL, id), &story); err != nil {
		return nil, err
	}
	var said []string
	for _, kid := range story.Kids {
		if len(said) == n {
			break
		}
		var c struct {
			Text    string `json:"text"`
			Deleted bool   `json:"deleted"`
			Dead    bool   `json:"dead"`
		}
		if err := getJSON(ctx, g, "hnitem", fmt.Sprintf(hnItemURL, kid), &c); err != nil {
			return said, err
		}
		if s := commentText(c.Text); s != "" && !c.Deleted && !c.Dead {
			said = append(said, s)
		}
	}
	return said, nil
}

func lobstersThreads(ctx context.Context, g *Guard, now time.Time) ([]thread, error) {
	var payload []lobstersStory
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
		t := thread{title: s.Title, host: hostOf(s.URL), link: s.URL, points: s.Score, comments: s.CommentCount, discuss: s.CommentsURL, age: 24 * time.Hour}
		if at, err := time.Parse(time.RFC3339, s.CreatedAt); err == nil {
			t.age = now.Sub(at)
		}
		hits = append(hits, hit{s.ShortID, t})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].t.points > hits[j].t.points })
	hits = hits[:min(len(hits), threadsShown)]

	for _, i := range busiest(len(hits), func(i int) int { return hits[i].t.comments }) {
		hits[i].t.busy = true
	}
	for i := range hits {
		if i >= storiesShown && !hits[i].t.busy {
			continue
		}
		said, err := lobstersComments(ctx, g, hits[i].id, commentsThread)
		if err != nil {
			slog.Warn("brief thread failed", slog.String("component", "brief"), slog.Any("err", err))
		}
		hits[i].t.said = said
	}
	out := make([]thread, len(hits))
	for i, h := range hits {
		out[i] = h.t
	}
	return out, nil
}

// lobstersComments is a story's top level comments by score, which one request
// carries all of.
func lobstersComments(ctx context.Context, g *Guard, shortID string, n int) ([]string, error) {
	var story struct {
		Comments []struct {
			Depth        int    `json:"depth"`
			Score        int    `json:"score"`
			CommentPlain string `json:"comment_plain"`
			IsDeleted    bool   `json:"is_deleted"`
		} `json:"comments"`
	}
	if err := getJSON(ctx, g, "lobsters", fmt.Sprintf(lobstersStoryF, shortID), &story); err != nil {
		return nil, err
	}
	top := story.Comments[:0]
	for _, c := range story.Comments {
		if c.Depth == 0 && !c.IsDeleted {
			top = append(top, c)
		}
	}
	sort.SliceStable(top, func(a, b int) bool { return top[a].Score > top[b].Score })
	var said []string
	for _, c := range top {
		if len(said) == n {
			break
		}
		if s := commentText(c.CommentPlain); s != "" {
			said = append(said, s)
		}
	}
	return said, nil
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
		if !t.busy || len(t.said) == 0 {
			continue
		}
		fmt.Fprintf(&b, "[%d] %s\n", i+1, t.title)
		for _, s := range t.said[:min(len(t.said), commentsRead)] {
			b.WriteString("- " + s + "\n")
		}
	}
	return b.String()
}

// Room past the asked 30 words so the model finishes the sentence on its own
// instead of running into the cap mid clause.
const feedReadMax = 240

func (b *Briefer) compileFeeds(ctx context.Context, slot briefSlot, at time.Time) (Brief, error) {
	now := time.Now()
	brief := Brief{Kind: slot.kind, Title: slot.title(), Slot: at.Unix(),
		Compiled: time.Now().In(easternTime()).Format("15:04 MST")}
	prev := map[string]ThreadRead{}
	for _, t := range b.store.Snapshot().Briefs.Feeds.Threads {
		prev[t.URL] = t
	}
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
			for _, t := range ts[:min(len(ts), storiesShown)] {
				tr := carriedRead(prev[t.discuss], t)
				if tr.Read == "" {
					tr = ThreadRead{URL: t.discuss, Read: b.threadRead(ctx, site.name, t),
						Comments: t.comments, At: brief.Compiled, Gist: tr.Gist}
				}
				if tr.Gist == "" {
					tr.Gist = b.articleTLDR(ctx, t.title, fetchArticle(ctx, b.guard, t.link))
				}
				if tr.Read != "" || tr.Gist != "" {
					brief.Threads = append(brief.Threads, tr)
				}
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
	system := `You write a short read for one reader of what a tech community's front page is like today, from its titles and the top comments on its busiest threads.
- "read" is one or two sentences, at most 30 words in all. Name the two or three subjects that run through more than one thread, then say what the commenters broadly make of them.
- If nothing runs through more than one thread, say the page is all over the place and give the argument on the busiest thread instead.
- Don't list titles back, since the reader sees them. Describe the page as a whole.
- Use only the titles and the listed comments. No outside knowledge, no opinion of your own, no hype words like buzzing, hot, or exploding.
- Do not put story numbers or point counts in the text. Cite the numbers of up to three threads the read draws on in "sources", the most important first.`
	user := fmt.Sprintf("The %s front page, most points first:\n%s", name, threadList(ts))
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"read":    map[string]any{"type": "string", "maxLength": feedReadMax},
			"sources": map[string]any{"type": "array", "minItems": 1, "maxItems": 3, "items": map[string]any{"type": "integer"}},
		},
		"required":             []string{"read", "sources"},
		"additionalProperties": false,
	}
	var out struct {
		Read    string `json:"read"`
		Sources []int  `json:"sources"`
	}
	// One retry, since the 9B now and then writes a single sentence straight
	// into the maxLength with no comma late enough for whole to keep.
	var read string
	ask := user
	for try := 0; try < 2 && read == ""; try++ {
		if err := b.model.Structured(ctx, system, ask, schema, 300, &out); err != nil {
			return Point{}, err
		}
		read = whole(out.Read, feedReadMax)
		if out.Read != "" && read == "" {
			slog.Info("brief line cut", slog.String("component", "brief"), slog.String("feed", name), slog.Int("try", try+1), slog.String("raw", out.Read))
			ask = user + "\n\nYour last read ran past the length limit and was cut off mid sentence. Write two short sentences, at most 30 words in all."
		}
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

// carriedRead keeps the last slot's read of a thread until it gains half again
// its comments, since the top comments rarely change a slot later. The article's
// TL;DR is kept whatever the comments did.
func carriedRead(was ThreadRead, t thread) ThreadRead {
	if was.URL == "" {
		return ThreadRead{}
	}
	if was.Comments == 0 || t.comments*2 > was.Comments*3 {
		return ThreadRead{URL: was.URL, Gist: was.Gist}
	}
	return was
}

// Room past the asked 25 words, for the same reason as feedReadMax.
const threadReadMax = 200

// threadRead is what one thread's commenters make of it, for the card its row
// opens. A thread with too little said to read gets nothing, and so does one
// the model fails on, since the card still has its top comments.
func (b *Briefer) threadRead(ctx context.Context, name string, t thread) string {
	if len(t.said) < commentsRead {
		return ""
	}
	system := `You write a short read for one reader of what the commenters on one thread make of it, from its title and its top comments.
- "read" is one or two sentences, at most 25 words in all. Say what most of the commenters think, then where they split if they do.
- Don't restate the title, since the reader sees it.
- Use only the listed comments. No outside knowledge, no opinion of your own, no hype words.`
	var c strings.Builder
	fmt.Fprintf(&c, "A %s thread, %d points and %d comments: %s", name, t.points, t.comments, t.title)
	if t.host != "" {
		c.WriteString(" (" + t.host + ")")
	}
	c.WriteString("\n\nTop comments, most upvoted first:\n")
	for _, s := range t.said {
		c.WriteString("- " + s + "\n")
	}
	user := c.String()
	schema := map[string]any{
		"type":                 "object",
		"properties":           map[string]any{"read": map[string]any{"type": "string", "maxLength": threadReadMax}},
		"required":             []string{"read"},
		"additionalProperties": false,
	}
	var out struct {
		Read string `json:"read"`
	}
	// A read that stops without a full stop was cut at the cap, and whole can
	// only guess where its last clause ended, so it gets one more try first.
	ask := user
	for try := 0; try < 2; try++ {
		if err := b.model.Structured(ctx, system, ask, schema, 200, &out); err != nil {
			slog.Warn("brief thread read failed", slog.String("component", "brief"), slog.String("feed", name), slog.Any("err", err))
			return ""
		}
		if read := tidy(out.Read); read == "" || strings.ContainsAny(read[len(read)-1:], ".!?") {
			return read
		}
		slog.Info("brief line cut", slog.String("component", "brief"), slog.String("feed", name), slog.Int("try", try+1), slog.String("raw", out.Read))
		ask = user + "\n\nYour last read ran past the length limit and was cut off mid sentence. Write two short sentences, at most 25 words in all."
	}
	return whole(out.Read, threadReadMax)
}
