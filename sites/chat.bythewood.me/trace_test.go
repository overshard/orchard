package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// A step goes out as it happens, so the list fills in while the answer is still
// being written rather than appearing whole at the end.
func TestAStepIsSentAsItHappens(t *testing.T) {
	var sent []Event
	tr := NewTrace(func(e Event) { sent = append(sent, e) })

	tr.Add(Step{Kind: "tool", Label: "wikipedia"})
	tr.Add(Step{Kind: "answer", Label: "wrote the reply"})

	if len(sent) != 2 {
		t.Fatalf("%d events sent, want 2", len(sent))
	}
	if sent[0].Kind != "step" || sent[0].Step == nil || sent[0].Step.Label != "wikipedia" {
		t.Errorf("first event is not the step: %+v", sent[0])
	}
	if len(tr.Steps()) != 2 {
		t.Errorf("%d steps kept, want 2", len(tr.Steps()))
	}
}

// The trace is written next to every message, so a prompt of several thousand
// characters cannot go in whole.
func TestALongFieldIsClippedAndSaysSo(t *testing.T) {
	tr := NewTrace(nil)
	tr.Add(Step{Kind: "prompt", In: strings.Repeat("x", stepMax+500)})

	got := tr.Steps()[0].In
	if len(got) > stepMax+80 {
		t.Errorf("field is %d characters, want it clipped near %d", len(got), stepMax)
	}
	if !strings.Contains(got, "more characters") {
		t.Errorf("a clipped field does not say it was clipped: %q", got[len(got)-40:])
	}
}

// A turn that goes wrong must not write a megabyte of steps.
func TestTheStepCountIsCapped(t *testing.T) {
	tr := NewTrace(nil)
	for i := 0; i < stepsMax+25; i++ {
		tr.Add(Step{Kind: "model", Label: "round"})
	}
	if n := len(tr.Steps()); n != stepsMax {
		t.Errorf("%d steps kept, want the cap of %d", n, stepsMax)
	}
}

// A nil trace is what the tests and any caller that does not want one pass, and
// it has to be usable rather than a panic waiting in the turn loop.
func TestANilTraceIsSafe(t *testing.T) {
	var tr *Trace
	tr.Add(Step{Kind: "model"})
	if tr.Steps() != nil {
		t.Error("a nil trace returned steps")
	}
}

// The steps have to survive a reload, which means the column has to exist and
// be read back. A new column that went into one of the two table creates and
// not the other would lose them silently.
func TestStepsSurviveAStoreRoundTrip(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()

	id, err := store.NewConversation("test")
	if err != nil {
		t.Fatalf("new conversation: %v", err)
	}
	want := []Step{
		{Kind: "wikipedia", Label: "looked the subject up", In: "north korea", Out: "a country", MS: 18},
		{Kind: "gate", Label: "checked the draft", Bad: true},
	}
	if err := store.Append(id, Stored{Role: RoleAssistant, Content: "hi", Steps: want}); err != nil {
		t.Fatalf("append: %v", err)
	}

	msgs, err := store.Messages(id)
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("%d messages, want 1", len(msgs))
	}
	got := msgs[0].Steps
	if len(got) != 2 {
		t.Fatalf("%d steps came back, want 2", len(got))
	}
	if got[0].In != "north korea" || got[0].MS != 18 {
		t.Errorf("the first step came back wrong: %+v", got[0])
	}
	if !got[1].Bad {
		t.Error("the failed step came back as fine")
	}
}
