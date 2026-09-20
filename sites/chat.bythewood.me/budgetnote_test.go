package main

import (
	"strings"
	"testing"

	"chat.bythewood.me/tools"
)

// The answer step used to be told only that the budget was gone, so a turn
// whose sums never ran still produced a table of figures in a confident voice.
func TestBudgetNoteNamesAToolThatNeverWorked(t *testing.T) {
	used := []tools.Result{
		{Name: "web_search", Content: "fine"},
		{Name: "calc", Err: "only arithmetic is supported"},
		{Name: "calc", Err: "that expression is too long"},
	}
	note := budgetNote(used)
	if !strings.Contains(note, "calc") {
		t.Errorf("calc failed every time and the note does not say so: %s", note)
	}
	if !strings.Contains(note, "Do not write down a number you could not work out") {
		t.Errorf("the note does not forbid guessing: %s", note)
	}
	if strings.Contains(note, "web_search") {
		t.Errorf("a tool that worked was named as a gap: %s", note)
	}
}

// A fetch that failed once and worked on the retry is not a gap, and naming it
// would teach the model to hedge an answer it actually has.
func TestBudgetNoteIgnoresAToolThatRecovered(t *testing.T) {
	used := []tools.Result{
		{Name: "web_fetch", Err: "timeout"},
		{Name: "web_fetch", Content: "the page"},
	}
	note := budgetNote(used)
	if strings.Contains(note, "web_fetch") {
		t.Errorf("a recovered tool was reported as a gap: %s", note)
	}
}

func TestBudgetNoteIsPlainWhenNothingFailed(t *testing.T) {
	note := budgetNote([]tools.Result{{Name: "calc", Content: 12}})
	if strings.Contains(note, "never worked") {
		t.Errorf("a clean turn got a failure note: %s", note)
	}
	if !strings.Contains(note, "tool budget") {
		t.Errorf("the base instruction went missing: %s", note)
	}
}

func TestBudgetNoteNamesTheReason(t *testing.T) {
	note := budgetNote([]tools.Result{{Name: "calc", Err: "only arithmetic is supported"}})
	if !strings.Contains(note, "only arithmetic is supported") {
		t.Errorf("the reason is what tells the reader what broke: %s", note)
	}
}
