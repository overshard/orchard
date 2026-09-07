package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func drain(t *testing.T, ch <-chan json.RawMessage, want int) []json.RawMessage {
	t.Helper()
	var got []json.RawMessage
	for len(got) < want {
		select {
		case b, ok := <-ch:
			if !ok {
				return got
			}
			got = append(got, b)
		case <-time.After(2 * time.Second):
			return got
		}
	}
	return got
}

// The whole point: a reader going away does not stop the turn, and the next
// reader is handed everything it missed.
func TestAReaderLeavingDoesNotStopTheRun(t *testing.T) {
	rs := NewRuns()
	rn := rs.Start("c1")

	_, ch, live := rn.Follow()
	if !live {
		t.Fatal("a fresh run is not live")
	}
	rn.Emit(Event{Kind: "status", Text: "thinking"})
	if len(drain(t, ch, 1)) != 1 {
		t.Fatal("the first reader saw nothing")
	}
	rn.Unfollow(ch)

	// The tab is gone and the turn carries on.
	rn.Emit(Event{Kind: "tool", Tool: "web_search"})
	rn.Emit(Event{Kind: "tail", Text: "an answer"})

	backlog, ch2, live2 := rn.Follow()
	if !live2 {
		t.Fatal("the run ended when its reader left")
	}
	if len(backlog) != 3 {
		t.Fatalf("the second reader was handed %d events, want all 3", len(backlog))
	}
	rn.Finish()
	if _, ok := <-ch2; ok {
		t.Error("finishing did not close the reader's channel")
	}
}

// A tab that comes back after the turn ended gets the whole thing and a closed
// channel, rather than an open stream that will never speak.
func TestAReaderArrivingAfterTheEndGetsEverything(t *testing.T) {
	rs := NewRuns()
	rn := rs.Start("c1")
	rn.Emit(Event{Kind: "status", Text: "thinking"})
	rn.Emit(map[string]any{"kind": "done", "conversation_id": "c1"})
	rn.Finish()

	backlog, ch, live := rn.Follow()
	if live || ch != nil {
		t.Error("a finished run handed back a live channel")
	}
	if len(backlog) != 2 {
		t.Fatalf("backlog had %d events, want 2", len(backlog))
	}
	var last map[string]any
	if err := json.Unmarshal(backlog[1], &last); err != nil {
		t.Fatal(err)
	}
	if last["kind"] != "done" {
		t.Errorf("the done frame did not survive: %v", last)
	}
}

// A new conversation runs under an id the browser made up, and has to be
// findable under the real one once the store hands it over.
func TestARunIsFoundUnderItsRealIdAfterRekey(t *testing.T) {
	rs := NewRuns()
	rn := rs.Start("temp-123")
	rn.Emit(Event{Kind: "status", Text: "thinking"})
	rs.Rekey("temp-123", "conv-abc")

	got, ok := rs.Get("conv-abc")
	if !ok || got != rn {
		t.Fatal("the run was not found under its real id")
	}
	if _, ok := rs.Get("temp-123"); ok {
		t.Error("the run is still under the made up id as well")
	}
}

// Sending again while a turn is running means the first is no longer wanted,
// and two generations writing into one conversation is the thing to avoid.
func TestStartingASecondTurnCancelsTheFirst(t *testing.T) {
	rs := NewRuns()
	first := rs.Start("c1")
	ctx, cancel := context.WithCancel(context.Background())
	first.setCancel(cancel)

	rs.Start("c1")
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Error("the first turn was left running")
	}
}

func TestCancelStopsARun(t *testing.T) {
	rs := NewRuns()
	rn := rs.Start("c1")
	ctx, cancel := context.WithCancel(context.Background())
	rn.setCancel(cancel)

	if !rs.Cancel("c1") {
		t.Fatal("cancel did not find the run")
	}
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Error("cancel did not stop the turn")
	}
	if rs.Cancel("nothing-here") {
		t.Error("cancel claimed to stop a run that does not exist")
	}
}

// A finished run is kept a while so a tab can still read it, and dropped after,
// or the process holds every conversation it ever ran.
func TestFinishedRunsAreSweptButOnlyWhenStale(t *testing.T) {
	rs := NewRuns()
	rn := rs.Start("c1")
	rs.Start("c2")
	rn.Finish()

	rs.Sweep(time.Now())
	if _, ok := rs.Get("c1"); !ok {
		t.Error("a run that just finished was swept")
	}
	rs.Sweep(time.Now().Add(runKept + time.Minute))
	if _, ok := rs.Get("c1"); ok {
		t.Error("a stale run was kept")
	}
	if _, ok := rs.Get("c2"); !ok {
		t.Error("a running turn was swept")
	}
}

// A reader that stops keeping up is skipped rather than waited on, or one slow
// tab holds up the turn and every reader behind it.
func TestASlowReaderDoesNotBlockTheTurn(t *testing.T) {
	rs := NewRuns()
	rn := rs.Start("c1")
	_, _, live := rn.Follow()
	if !live {
		t.Fatal("not live")
	}
	done := make(chan struct{})
	go func() {
		for i := 0; i < 5000; i++ {
			rn.Emit(Event{Kind: "tail", Text: "x"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the turn blocked on a reader that was not reading")
	}
}

// Two turns at once take the card in order rather than interleaving, and the
// one waiting is told where it is rather than sitting on a spinner.
func TestASecondTurnWaitsAndIsToldSo(t *testing.T) {
	q := NewQueue()
	first, ok := q.Enter(context.Background(), func(QueueState) {})
	if !ok {
		t.Fatal("the first turn did not get the card")
	}

	waits := make(chan QueueState, 8)
	got := make(chan struct{})
	go func() {
		release, ok := q.Enter(context.Background(), func(s QueueState) { waits <- s })
		if ok {
			release()
		}
		close(got)
	}()

	select {
	case s := <-waits:
		// Ahead counts the others queued in front, so the first waiter behind a
		// running turn is position 1 with none ahead of it.
		if s.Position < 1 {
			t.Errorf("the waiting turn was given position %d", s.Position)
		}
		if label := waitingLabel(s); label == "" {
			t.Error("no label for a waiting turn")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the second turn was never told it was waiting")
	}

	select {
	case <-got:
		t.Fatal("the second turn ran while the first still held the card")
	case <-time.After(200 * time.Millisecond):
	}

	first()
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("the second turn never got the card after the first let go")
	}
}

func TestWaitingLabelReadsAsWords(t *testing.T) {
	for _, tc := range []struct {
		ahead int
		want  string
	}{
		{0, "waiting for the card"},
		{1, "waiting, one turn ahead"},
		{3, "waiting, 3 turns ahead"},
	} {
		if got := waitingLabel(QueueState{Ahead: tc.ahead}); got != tc.want {
			t.Errorf("ahead=%d gave %q, want %q", tc.ahead, got, tc.want)
		}
	}
}
