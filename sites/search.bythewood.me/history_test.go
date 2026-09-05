package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testHistory(t *testing.T) *History {
	t.Helper()
	h, _ := testHistoryDir(t)
	return h
}

func testHistoryDir(t *testing.T) (*History, string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "search-hist")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	h, err := OpenHistory(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close() })
	return h, dir
}

func sampleAnswer() *Answer {
	return &Answer{
		Query: "when is the next liverpool game", Standalone: "when is the next liverpool game",
		Shape: ShapeUpcoming, Text: "They play Fulham on **12 September 2026** [1].",
		Queries: []string{"liverpool fixtures september 2026"},
		Sources: []Source{
			{N: 1, URL: "https://sports-calendar.com/en/soccer/liverpool", Site: "sports-calendar.com"},
			{N: 2, URL: "https://www.thisisanfield.com/2026/09/fixtures", Site: "This Is Anfield"},
		},
		Warnings: []string{"a source names 9 September"},
		Support:  1, Elapsed: "10.2s",
		Citations: []Citation{{Checked: true, Supported: true, PassageID: 1}},
	}
}

func TestHistoryLogAndList(t *testing.T) {
	h := testHistory(t)
	id, err := h.Log(sampleAnswer(), Stamp{Model: "unsloth/Qwen3.5-4B-GGUF:Q4_K_M", Prompts: "abc123", Sampling: "s"})
	if err != nil || id == 0 {
		t.Fatalf("log: id=%d err=%v", id, err)
	}
	got, err := h.List(10, 0, "")
	if err != nil || len(got) != 1 {
		t.Fatalf("list: %d rows, err=%v", len(got), err)
	}
	e := got[0]
	if e.Question != "when is the next liverpool game" || string(ShapeUpcoming) != e.Shape {
		t.Errorf("round trip lost the question or shape: %+v", e)
	}
	if len(e.Sources) != 2 || len(e.Queries) != 1 || len(e.Warnings) != 1 {
		t.Errorf("json columns did not round trip: %+v", e)
	}
	if e.Model == "" || e.Prompts == "" {
		t.Error("an answer with no stamp cannot be read back next month")
	}
	if e.Rated() {
		t.Error("a fresh answer is unrated")
	}
	if !strings.Contains(string(e.Body()), "<strong>") {
		t.Errorf("the stored markdown should render, got %q", e.Body())
	}
}

// A thumb moves the domains behind the answer, and changing your mind has to
// move them back rather than counting both.
func TestRateMovesDomainsAndReverses(t *testing.T) {
	h := testHistory(t)
	id, _ := h.Log(sampleAnswer(), Stamp{})

	if err := h.Rate(id, 1, "", ""); err != nil {
		t.Fatal(err)
	}
	rep := h.Reputation()
	if len(rep) != 2 {
		t.Fatalf("want both domains scored, got %v", rep)
	}
	// Laplace smoothing: one good out of one is 2/3, not 1.
	if got := rep["sports-calendar.com"]; got < 0.66 || got > 0.67 {
		t.Errorf("one good answer should score 2/3, got %.3f", got)
	}

	if err := h.Rate(id, -1, "sources", ""); err != nil {
		t.Fatal(err)
	}
	if got := h.Reputation()["sports-calendar.com"]; got < 0.33 || got > 0.34 {
		t.Errorf("changing the verdict should not leave both counted, got %.3f", got)
	}

	only, err := h.List(10, 0, "down")
	if err != nil || len(only) != 1 || only[0].Reason != "sources" {
		t.Fatalf("filtering to down thumbs: %d rows err=%v", len(only), err)
	}
	if !only[0].Bad() {
		t.Error("that one is thumbed down")
	}
	if got, _ := h.List(10, 0, "up"); len(got) != 0 {
		t.Errorf("it is no longer an up thumb, got %d", len(got))
	}
}

// Deleting a question has to leave the reputation standing, since that holds no
// question text and is what the site learned rather than what was asked.
func TestDeleteKeepsWhatWasLearned(t *testing.T) {
	h := testHistory(t)
	id, _ := h.Log(sampleAnswer(), Stamp{})
	h.Rate(id, 1, "", "")

	if err := h.Delete(id); err != nil {
		t.Fatal(err)
	}
	if got, _ := h.List(10, 0, ""); len(got) != 0 {
		t.Fatalf("the question should be gone, got %d rows", len(got))
	}
	if total, rated := h.Count(); total != 0 || rated != 0 {
		t.Errorf("counts should be zero, got %d and %d", total, rated)
	}
	if len(h.Reputation()) != 2 {
		t.Error("deleting a question should not delete what it taught")
	}

	id2, _ := h.Log(sampleAnswer(), Stamp{})
	h.Rate(id2, -1, "wrong", "")
	if err := h.DeleteAll(); err != nil {
		t.Fatal(err)
	}
	if got, _ := h.List(10, 0, ""); len(got) != 0 {
		t.Errorf("delete everything should leave nothing, got %d", len(got))
	}
}

// The version stamp has to move when a prompt does, or a month of thumbs
// cannot be told apart from the month before it.
func TestPromptVersionMovesWithTheContracts(t *testing.T) {
	before := promptVersion()
	if len(before) != 12 {
		t.Fatalf("want a short hash, got %q", before)
	}
	c := contracts[ShapeUpcoming]
	original := c.Instruction
	c.Instruction += " One more rule."
	contracts[ShapeUpcoming] = c
	t.Cleanup(func() {
		c.Instruction = original
		contracts[ShapeUpcoming] = c
	})
	if promptVersion() == before {
		t.Error("editing a contract has to change the prompt version")
	}
}

// Deleting a row is not deleting the text. secure_delete zeroes it in place and
// VACUUM rewrites the file, but both write through the WAL, so without the
// checkpoint the question is still there in plain bytes beside the database.
// The only honest test of that is to read the files.
func TestDeleteLeavesNothingOnDisk(t *testing.T) {
	h, dir := testHistoryDir(t)
	a := sampleAnswer()
	a.Query = "a question nobody else would ask zzqqxx"
	a.Text = "an answer nobody else would write zzqqxx."
	id, err := h.Log(a, Stamp{})
	if err != nil {
		t.Fatal(err)
	}
	if !onDisk(t, dir, "zzqqxx") {
		t.Fatal("the question should be on disk before it is deleted, or this proves nothing")
	}
	if err := h.Delete(id); err != nil {
		t.Fatal(err)
	}
	if onDisk(t, dir, "zzqqxx") {
		t.Error("a deleted question is still readable in the database directory")
	}
}

func onDisk(t *testing.T, dir, needle string) bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		if bytes.Contains(b, []byte(needle)) {
			return true
		}
	}
	return false
}
