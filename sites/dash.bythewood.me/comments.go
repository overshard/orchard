package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// The top few comments on every row of both feeds, for the card a row opens.
// They're fetched on the news poll, but a thread's comments only get another
// look once they're this old, since every HN comment is its own request.
const (
	commentsShown = 3
	commentsStale = 45 * time.Minute
)

type saidEntry struct {
	said []string
	at   time.Time
}

// commentBook is keyed by a story's discussion URL, which both feeds carry and
// which doesn't change while the story moves around the page.
type commentBook struct {
	mu sync.Mutex
	m  map[string]saidEntry
}

func newCommentBook() *commentBook { return &commentBook{m: map[string]saidEntry{}} }

func (b *commentBook) attach(stories []Story) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range stories {
		stories[i].Said = b.m[stories[i].Comments].said
	}
}

// stale is the stories due a fetch, and it forgets any story that's left both
// pages so the book doesn't grow all day.
func (b *commentBook) stale(hn, lobsters []Story, now time.Time) (due []Story, feeds []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	keep := map[string]bool{}
	for _, list := range []struct {
		feed    string
		stories []Story
	}{{"hn", hn}, {"lobsters", lobsters}} {
		for _, s := range list.stories {
			keep[s.Comments] = true
			if s.id == "" || s.Count == 0 {
				continue
			}
			if e, ok := b.m[s.Comments]; !ok || now.Sub(e.at) >= commentsStale {
				due = append(due, s)
				feeds = append(feeds, list.feed)
			}
		}
	}
	for k := range b.m {
		if !keep[k] {
			delete(b.m, k)
		}
	}
	return due, feeds
}

func (b *commentBook) put(key string, said []string, at time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.m[key] = saidEntry{said, at}
}

// refreshComments fetches what's due and patches it onto the rows. A failed
// fetch is left out of the book, so the next poll tries it again.
func (s *Store) refreshComments(ctx context.Context, g *Guard) {
	if s.comments == nil {
		return
	}
	snap := s.Snapshot()
	now := time.Now()
	due, feeds := s.comments.stale(snap.HN, snap.Lobsters, now)
	got := 0
	for i, st := range due {
		var said []string
		var err error
		if feeds[i] == "hn" {
			var id int64
			if _, err = fmt.Sscan(st.id, &id); err == nil {
				said, err = hnComments(ctx, g, id, commentsShown)
			}
		} else {
			said, err = lobstersComments(ctx, g, st.id, commentsShown)
		}
		if err != nil {
			slog.Warn("comments fetch failed", slog.String("component", "news"), slog.String("feed", feeds[i]), slog.Any("err", err))
			continue
		}
		s.comments.put(st.Comments, said, now)
		got++
	}
	if got == 0 {
		return
	}
	s.update(func(st *State) {
		s.comments.attach(st.HN)
		s.comments.attach(st.Lobsters)
	})
}
