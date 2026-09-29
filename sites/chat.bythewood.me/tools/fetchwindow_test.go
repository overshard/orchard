package tools

import (
	"strings"
	"testing"
)

// A README's settings sit far below where a fetch used to stop, and the first
// mention of a heading is its line in the table of contents.
func TestAFetchCanStartAtASection(t *testing.T) {
	page := "Index\nKeybindings\nConfigurability\n" + strings.Repeat("filler line about installing\n", 400) +
		"Configurability\nproc_left = false\n" + strings.Repeat("more\n", 100)
	part, cut, note := window(page, "configurability", 200)
	if !strings.Contains(part, "proc_left") || !cut || note != "" {
		t.Errorf("did not land on the section: cut=%v note=%q part=%q", cut, note, part)
	}
	part, _, note = window(page, "no such heading", 200)
	if !strings.HasPrefix(part, "Index") || note == "" {
		t.Errorf("a missing phrase should say so and start at the top: %q %q", note, part)
	}
	if part, cut, _ := window("short page", "", 200); part != "short page" || cut {
		t.Errorf("a short page came back as %q cut=%v", part, cut)
	}
	if part, _, _ := window(strings.Repeat("é", 300), "", 101); !strings.HasSuffix(part, "é") {
		t.Error("cut inside a rune")
	}
}
