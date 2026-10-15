package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCommentBookDueAndPrune(t *testing.T) {
	now := time.Now()
	b := newCommentBook()
	b.put("https://news.ycombinator.com/item?id=1", []string{"fresh"}, now)
	b.put("https://news.ycombinator.com/item?id=2", []string{"old"}, now.Add(-time.Hour))
	b.put("https://lobste.rs/s/gone", []string{"gone"}, now)
	hn := []Story{
		{Comments: "https://news.ycombinator.com/item?id=1", Count: 5, id: "1"},
		{Comments: "https://news.ycombinator.com/item?id=2", Count: 5, id: "2"},
		{Comments: "https://news.ycombinator.com/item?id=3", Count: 5, id: "3"},
		{Comments: "https://news.ycombinator.com/item?id=4", Count: 0, id: "4"},
	}
	due, feeds := b.stale(hn, nil, now)
	if len(due) != 2 || due[0].id != "2" || due[1].id != "3" || feeds[0] != "hn" {
		t.Fatalf("due %+v %v", due, feeds)
	}
	if _, ok := b.m["https://lobste.rs/s/gone"]; ok {
		t.Error("a story off both pages was kept")
	}
	b.attach(hn)
	if len(hn[0].Said) != 1 || hn[0].Said[0] != "fresh" || hn[2].Said != nil {
		t.Errorf("%+v", hn)
	}
}

func TestAttachThreadReads(t *testing.T) {
	st := State{HN: []Story{{Comments: "a"}, {Comments: "b", Read: "stale"}}}
	st.Briefs.Feeds.Compiled = "16:05 EDT"
	st.Briefs.Feeds.Threads = []ThreadRead{{URL: "a", Read: "Most think it's overdue."}}
	st.attachFeedReads()
	if st.HN[0].Read != "Most think it's overdue." || st.HN[0].ReadAt != "16:05 EDT" || st.HN[1].Read != "" || st.HN[1].ReadAt != "" {
		t.Errorf("%+v", st.HN)
	}
}

func TestThreadReadNeedsComments(t *testing.T) {
	m := &scriptedModel{}
	b := &Briefer{model: m}
	if got := b.threadRead(context.Background(), "Hacker News", thread{title: "Quiet", said: []string{"one", "two"}}); got != "" || len(m.asks) != 0 {
		t.Errorf("read %q with %d asks", got, len(m.asks))
	}
	m.answers = []string{`{"read": "Most like it, but a few say the benchmark is cherry picked."}`}
	th := thread{title: "Fast thing", host: "example.com", points: 300, comments: 120, said: []string{"Nice.", "Cherry picked.", "Finally."}}
	if got := b.threadRead(context.Background(), "Hacker News", th); got != "Most like it, but a few say the benchmark is cherry picked." {
		t.Errorf("%q", got)
	}
	if !strings.Contains(m.asks[0], "- Cherry picked.") || !strings.Contains(m.asks[0], "Fast thing (example.com)") {
		t.Errorf("ask: %s", m.asks[0])
	}
}

// The front page read still sees three comments from the busiest threads only,
// though every shown thread now carries more for its own read.
func TestThreadListOnlyBusy(t *testing.T) {
	ts := []thread{
		{title: "Busy", busy: true, said: []string{"a", "b", "c", "d"}},
		{title: "Shown", said: []string{"x", "y", "z"}},
	}
	got := threadList(ts)
	if !strings.Contains(got, "- c\n") || strings.Contains(got, "- d\n") || strings.Contains(got, "- x\n") {
		t.Errorf("%s", got)
	}
}

// The 14 Oct passkeys read, cut at a comma with its last clause left as two
// words, which whole kept as a sentence.
func TestThreadReadRetriesACutRead(t *testing.T) {
	m := &scriptedModel{answers: []string{
		`{"read": "Most think passkeys are a mess, but some say the issue is attestation being extended to restrict devices, while others,"}`,
		`{"read": "Most think passkeys are a mess. Some blame attestation for locking devices down."}`,
	}}
	b := &Briefer{model: m}
	th := thread{title: "Passkeys are not a trap", said: []string{"a", "b", "c"}}
	if got := b.threadRead(context.Background(), "Hacker News", th); got != "Most think passkeys are a mess. Some blame attestation for locking devices down." || len(m.asks) != 2 {
		t.Errorf("%q asks=%d", got, len(m.asks))
	}
}
