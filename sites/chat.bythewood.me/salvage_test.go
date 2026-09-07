package main

import (
	"strings"
	"testing"
)

// The exact shape Ornith leaked into an answer: no closing function tag, no
// closing parameter tag, and the whole thing rendered to the reader as markup.
const leaked = "\n<tool_call> <function=web_fetch> <parameter=url> https://github.com/restic/restic/releases </tool_call>\n"

func known(n string) bool { return n == "web_fetch" || n == "web_search" }

func TestSalvageRecoversTheCall(t *testing.T) {
	clean, calls := salvageCalls(leaked, known)
	if len(calls) != 1 {
		t.Fatalf("want 1 call, got %d", len(calls))
	}
	if calls[0].Function.Name != "web_fetch" {
		t.Errorf("name = %q", calls[0].Function.Name)
	}
	if !strings.Contains(calls[0].Function.Arguments, "restic/restic/releases") {
		t.Errorf("args = %q", calls[0].Function.Arguments)
	}
	if strings.Contains(clean, "<") {
		t.Errorf("markup survived into the answer: %q", clean)
	}
}

func TestSalvageStripsMarkupEvenWhenTheToolIsUnknown(t *testing.T) {
	clean, calls := salvageCalls("Here you go.\n"+leaked, func(string) bool { return false })
	if len(calls) != 0 {
		t.Fatalf("want no calls for an unknown tool, got %d", len(calls))
	}
	if strings.Contains(clean, "tool_call") || strings.Contains(clean, "<") {
		t.Errorf("markup survived: %q", clean)
	}
	if !strings.Contains(clean, "Here you go.") {
		t.Errorf("real prose was lost: %q", clean)
	}
}

func TestSalvageJSONShape(t *testing.T) {
	in := `<tool_call>{"name": "web_search", "arguments": {"query": "restic latest release"}}</tool_call>`
	clean, calls := salvageCalls(in, known)
	if len(calls) != 1 || calls[0].Function.Name != "web_search" {
		t.Fatalf("calls = %+v", calls)
	}
	if !strings.Contains(calls[0].Function.Arguments, "restic latest release") {
		t.Errorf("args = %q", calls[0].Function.Arguments)
	}
	if clean != "" {
		t.Errorf("clean = %q, want empty", clean)
	}
}

func TestSalvageLeavesOrdinaryProseAlone(t *testing.T) {
	in := "Use `restic backup` nightly. Compare a < b in your script."
	clean, calls := salvageCalls(in, known)
	if len(calls) != 0 || clean != in {
		t.Errorf("prose was modified: %q, calls %d", clean, len(calls))
	}
}
