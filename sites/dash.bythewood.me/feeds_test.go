package main

import "testing"

func TestCarriedRead(t *testing.T) {
	was := ThreadRead{URL: "u", Read: "Most like it.", Gist: "A gist.", Comments: 100, At: "09:01 EDT"}
	if got := carriedRead(was, thread{comments: 140}); got != was {
		t.Errorf("a thread that grew by 40%% should keep its read, got %+v", got)
	}
	if got := carriedRead(was, thread{comments: 160}); got.Read != "" || got.Gist != "A gist." {
		t.Errorf("a thread that grew by 60%% should be read again and keep its gist, got %+v", got)
	}
	if got := carriedRead(ThreadRead{URL: "u", Read: "Old.", Gist: "A gist."}, thread{comments: 10}); got.Read != "" || got.Gist != "A gist." {
		t.Errorf("a read with no count should be read again, got %+v", got)
	}
	if got := carriedRead(ThreadRead{}, thread{comments: 10}); got != (ThreadRead{}) {
		t.Errorf("a new thread has nothing to carry, got %+v", got)
	}
}

func TestCarryFeedKeepsOnlyThatFeed(t *testing.T) {
	was := Brief{
		Points: []Point{{Label: "HN", Text: "old hn"}, {Label: "LOBSTERS", Text: "old lobsters"}},
		Threads: []ThreadRead{
			{URL: "https://news.ycombinator.com/item?id=1", Read: "hn thread"},
			{URL: "https://lobste.rs/s/abc", Read: "lobsters thread"},
		},
	}
	var b Brief
	carryFeed(&b, was, "HN", "https://news.ycombinator.com/")
	if len(b.Points) != 1 || b.Points[0].Text != "old hn" {
		t.Fatalf("points = %+v", b.Points)
	}
	if len(b.Threads) != 1 || b.Threads[0].Read != "hn thread" {
		t.Fatalf("threads = %+v", b.Threads)
	}
}
