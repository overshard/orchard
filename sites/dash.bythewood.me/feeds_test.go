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
