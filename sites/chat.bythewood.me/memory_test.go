package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func memStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStore(filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestAFactIsStoredOnceHoweverOftenProposed(t *testing.T) {
	s := memStore(t)
	for i := 0; i < 3; i++ {
		if _, err := s.AddFact("Isaac drinks his coffee black"); err != nil {
			t.Fatal(err)
		}
	}
	facts, err := s.Facts()
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 {
		t.Fatalf("got %d facts, want 1", len(facts))
	}
}

// Retrieval is the whole value of this. A fact that does not surface when the
// question touches it may as well not be stored.
func TestRelevantFindsWhatTheQuestionTouches(t *testing.T) {
	s := memStore(t)
	for _, f := range []string{
		"Isaac drinks his coffee black",
		"Isaac prefers a hammock to a tent",
		"Isaac runs every site he owns from a desktop behind a Cloudflare tunnel",
		"Isaac camps most weekends through the autumn",
	} {
		if _, err := s.AddFact(f); err != nil {
			t.Fatal(err)
		}
	}

	got := s.Relevant("what tent should I take camping this year", 3, true)
	if len(got) == 0 {
		t.Fatal("nothing matched a question about camping")
	}
	joined := ""
	for _, f := range got {
		joined += f.Text + " | "
	}
	if !strings.Contains(joined, "hammock") || !strings.Contains(joined, "camps") {
		t.Errorf("the two facts that matter did not come back:\n%s", joined)
	}
	if strings.Contains(joined, "coffee") {
		t.Errorf("an unrelated fact was pulled in:\n%s", joined)
	}
}

// Without stopwords "my" and "the" match everything, so a question about
// anything drags in the whole table.
func TestRelevantIgnoresWordsThatMatchEverything(t *testing.T) {
	s := memStore(t)
	if _, err := s.AddFact("Isaac prefers the aisle seat"); err != nil {
		t.Fatal(err)
	}
	if got := s.Relevant("what is the weather", 5, true); len(got) != 0 {
		t.Errorf("matched on filler words: %+v", got)
	}
	if _, err := s.AddFact("Isaac is researching whether a tiny home on wheels can be lived in full-time"); err != nil {
		t.Fatal(err)
	}
	if got := s.Relevant("what time is it right now", 5, true); len(got) != 0 {
		t.Errorf("matched on filler words: %+v", got)
	}
}

// A long message shares a word with almost anything, so one in common is chance.
// These are real questions and the facts each one pulled in by a single word.
func TestALongQuestionNeedsTwoWordsInCommon(t *testing.T) {
	s := memStore(t)
	for _, f := range []string{
		"Isaac dislikes super-processed, pre-cooked food and prefers simple, whole ingredients seasoned with spices.",
		"Isaac packs a warm layer, a mid-40s-rated sleeping bag, and dry sleepwear for cool camping nights.",
		"Isaac's chat application is fronted by a Cloudflare Tunnel and Caddy, the same stack that fronts his other sites.",
		"Isaac runs his own AI assistant, Ornith 1.5 9B, on his RTX 3070 GPU, which he also uses for gaming.",
	} {
		if _, err := s.AddFact(f); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{
		`what do people currently use for TUI to view process and system monitoring on linux as the cool thing -- i used to use top then htop for years -- is htop still on "top" as it were`,
		"why has wardogs been losing rating % on steam? it started low due to server issues/ddos, then went up to like 81%, now it's at like 76%",
		"why has arctic vibe celcius packets and alani nu packets been just sold out on amazon and other places for like a few weeks / month now?",
	} {
		if got := s.Relevant(q, 6, true); len(got) != 0 {
			t.Errorf("%q pulled in %q", q[:40], got[0].Text)
		}
	}
	if got := s.Relevant("is my 3070 still good enough for gaming at 1440p with newer titles coming out this year", 6, true); len(got) != 1 {
		t.Errorf("a long question sharing two words with a fact got %d facts", len(got))
	}
}

func TestRelevantCountsUses(t *testing.T) {
	s := memStore(t)
	if _, err := s.AddFact("Isaac uses bun rather than npm"); err != nil {
		t.Fatal(err)
	}
	s.Relevant("should I use bun here", 3, true)
	facts, _ := s.Facts()
	if facts[0].Used != 1 {
		t.Errorf("used = %d, want 1", facts[0].Used)
	}
}

func TestAnIncognitoRecallCountsNothing(t *testing.T) {
	s := memStore(t)
	if _, err := s.AddFact("Isaac uses bun rather than npm"); err != nil {
		t.Fatal(err)
	}
	if got := s.Relevant("should I use bun here", 3, false); len(got) != 1 {
		t.Fatalf("recalled %d facts, want 1", len(got))
	}
	facts, _ := s.Facts()
	if facts[0].Used != 0 {
		t.Errorf("used = %d after an incognito recall, want 0", facts[0].Used)
	}
}

// A model that ignores the rules once must not be able to write a credential
// into a file that is read into every future turn.
func TestCredentialsAreRefusedWhateverTheModelSays(t *testing.T) {
	for _, bad := range []string{
		"Isaac's password is hunter2",
		"The api key for the gateway is orch-EXAMPLEEXAMPLEEXAMPLEEXAMPLEexample00",
		"Isaac's token is eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
		"His ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQ key",
	} {
		if !looksSecret(bad) {
			t.Errorf("allowed through: %q", bad)
		}
	}
	for _, fine := range []string{
		"Isaac drinks his coffee black",
		"Isaac prefers a hammock to a tent",
		"Isaac runs Debian 13 trixie in his development container",
	} {
		if looksSecret(fine) {
			t.Errorf("refused an ordinary fact: %q", fine)
		}
	}
}

func TestApplyChangesRefusesAnIdThatIsNotThere(t *testing.T) {
	s := memStore(t)
	id, err := s.AddFact("Isaac drinks his coffee black")
	if err != nil {
		t.Fatal(err)
	}
	existing, _ := s.Facts()
	site := &site{store: s}

	applied := site.applyChanges([]memChange{
		{Op: "replace", ID: id + 999, Fact: "something else"},
		{Op: "delete", ID: id + 998},
	}, existing)
	if len(applied) != 0 {
		t.Errorf("acted on ids that do not exist: %+v", applied)
	}
	if facts, _ := s.Facts(); len(facts) != 1 {
		t.Errorf("the real fact was disturbed: %+v", facts)
	}
}

func TestApplyChangesReplacesRatherThanDuplicating(t *testing.T) {
	s := memStore(t)
	id, _ := s.AddFact("Isaac's car is a Subaru")
	existing, _ := s.Facts()
	site := &site{store: s}

	site.applyChanges([]memChange{{Op: "replace", ID: id, Fact: "Isaac's car is a Toyota"}}, existing)

	facts, _ := s.Facts()
	if len(facts) != 1 {
		t.Fatalf("got %d facts, want the one replaced", len(facts))
	}
	if facts[0].Text != "Isaac's car is a Toyota" {
		t.Errorf("fact = %q", facts[0].Text)
	}
}

// A small model wraps JSON in a fence or writes a sentence in front of it, and
// neither is a reason to lose the edit.
func TestParseChangesSurvivesTheUsualWrapping(t *testing.T) {
	raw := "Sure, here is what should change:\n```json\n" +
		`{"changes":[{"op":"add","fact":"Isaac drinks his coffee black"},{"op":"delete","id":4}]}` +
		"\n```"
	got := parseChanges(raw)
	if len(got) != 2 {
		t.Fatalf("got %d changes: %+v", len(got), got)
	}
	if got[0].Op != "add" || got[0].Fact != "Isaac drinks his coffee black" {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].Op != "delete" || got[1].ID != 4 {
		t.Errorf("second = %+v", got[1])
	}
}

func TestParseChangesDropsTheMalformed(t *testing.T) {
	got := parseChanges(`{"changes":[{"op":"add"},{"op":"replace","fact":"no id"},{"op":"burn","id":1},{"op":"delete","id":0}]}`)
	if len(got) != 0 {
		t.Errorf("kept malformed changes: %+v", got)
	}
}

// An empty block is left out entirely rather than saying it knows nothing,
// which a model reads as an invitation to talk about not knowing things.
func TestMemoryBlockIsEmptyWhenNothingMatched(t *testing.T) {
	if got := memoryBlock(nil); got != "" {
		t.Errorf("got %q, want nothing", got)
	}
	got := memoryBlock([]Fact{{Text: "Isaac drinks his coffee black"}})
	if !strings.Contains(got, "coffee black") {
		t.Errorf("the fact is missing:\n%s", got)
	}
	if !strings.Contains(got, "Do not list them back") {
		t.Errorf("the instruction not to recite them is missing:\n%s", got)
	}
}

func TestTidyFactCapsLength(t *testing.T) {
	long := "Isaac " + strings.Repeat("x", maxFactChars+50)
	if got := tidyFact(long); len(got) > maxFactChars {
		t.Errorf("length = %d, want at most %d", len(got), maxFactChars)
	}
	if got := tidyFact("  - Isaac   drinks  coffee "); got != "Isaac drinks coffee" {
		t.Errorf("got %q", got)
	}
}

// The unique index only ever caught a fact proposed back word for word. On
// 2026-09-08 one turn wrote two nearly identical facts about X post search and
// left a third standing that contradicted both, because the model is asked to
// replace rather than add and did not.
func TestAddFactMergesARewording(t *testing.T) {
	s := memStore(t)

	first, err := s.AddFact("Isaac wants to build XCancel into the chat tooling in some way.")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.AddFact("Isaac is still looking for a way to integrate X post search into the chat tooling, " +
		"but xcancel and Nitter are not the solutions since xcancel has no API.")
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Errorf("a rewording was stored as a new fact (%d then %d)", first, second)
	}
	facts, _ := s.Facts()
	if len(facts) != 1 {
		t.Fatalf("the table holds %d facts, want the one", len(facts))
	}
	// The newer wording is the one that stands, since it is the correction.
	if !strings.Contains(facts[0].Text, "not the solutions") {
		t.Errorf("the stored fact is the old wording: %q", facts[0].Text)
	}
}

// A fact wholly contained in one already stored adds nothing, and replacing the
// longer with the shorter would throw away what it knew.
func TestAddFactKeepsTheFullerWording(t *testing.T) {
	s := memStore(t)
	full, _ := s.AddFact("Isaac camps and hikes in Yadkin Valley, North Carolina.")
	again, err := s.AddFact("Isaac camps and hikes.")
	if err != nil {
		t.Fatal(err)
	}
	if again != full {
		t.Errorf("the shorter fact was stored separately (%d then %d)", full, again)
	}
	facts, _ := s.Facts()
	if len(facts) != 1 || !strings.Contains(facts[0].Text, "Yadkin") {
		t.Errorf("the fuller wording was lost: %#v", facts)
	}
}

// Two facts sharing only a name are not the same fact, and merging them would
// lose one of them for good.
func TestAddFactKeepsUnrelatedFactsApart(t *testing.T) {
	s := memStore(t)
	_, _ = s.AddFact("Isaac likes buttered chicken pizza.")
	_, _ = s.AddFact("Isaac eats sausages but avoids ones containing nitrates or nitrites.")
	_, _ = s.AddFact("Isaac regularly cooks sheet-pan meals of broccoli and potatoes with a single roasted protein.")
	facts, _ := s.Facts()
	if len(facts) != 3 {
		t.Errorf("three unrelated facts collapsed to %d: %#v", len(facts), facts)
	}
}

// Real misses from a day of use, each a fact recalled by one word it shared
// with a question about something else.
func TestOneSharedWordIsChanceOnAnOrdinaryQuestion(t *testing.T) {
	s := memStore(t)
	for _, f := range []string{
		"Isaac eats sausages but avoids ones containing nitrates or nitrites.",
		"Isaac likes the Vogue article's idea of lived-in, well-loved interiors over freshly new.",
		"Isaac is researching whether a tiny home on wheels can legally be lived in full-time in Yadkin County, NC.",
		"Isaac works on a project called orchard-dash, a dashboard he is developing.",
	} {
		if _, err := s.AddFact(f); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{
		"how do i backup my aegis one time passwords",
		"any big news over night",
		"can i have multiple tailscale networks on the same system like for work and home?",
		"make a cyberpunk esque living room scene",
	} {
		if got := s.Relevant(q, 6, true); len(got) != 0 {
			t.Errorf("%q pulled in %q", q, got[0].Text)
		}
	}
}

func TestAFilmIsAMovie(t *testing.T) {
	s := memStore(t)
	if _, err := s.AddFact("Isaac and his friends want to watch the films Heretic and Terrifier this weekend."); err != nil {
		t.Fatal(err)
	}
	if got := s.Relevant("what movies do my friends and I want to watch", 6, true); len(got) != 1 {
		t.Errorf("a question about movies missed the fact about films: %d", len(got))
	}
}

// The id a recalled fact is shown with was copied into the fact itself.
func TestAFactLosesTheIDItWasShownWith(t *testing.T) {
	for in, want := range map[string]string{
		"[1] Isaac's friends movie night watch list: Heretic.": "Isaac's friends movie night watch list: Heretic.",
		"Isaac likes buttered chicken pizza. (id 19)":          "Isaac likes buttered chicken pizza.",
	} {
		if got := tidyFact(in); got != want {
			t.Errorf("tidyFact(%q) = %q", in, got)
		}
	}
	if got := memoryBlock([]Fact{{ID: 7, Text: "Isaac drinks his coffee black"}}); !strings.Contains(got, "(id 7)") {
		t.Errorf("the block does not carry the id: %q", got)
	}
}

func TestAnOverlongFactEndsOnAClause(t *testing.T) {
	long := "Isaac wants to build an autonomous AI WAF: run orchard-llm on orchard-logging at midnight to find bad actors, " +
		"generate an orchard Caddy rule to block them, verify accuracy, live-reload Caddy, test it works and nothing is broken, " +
		"and possibly route traffic through a firewall (UFW) first before Caddy and update a firewall rule instead."
	got := tidyFact(long)
	if len(got) > maxFactChars || !strings.HasSuffix(got, ".") || strings.HasSuffix(got, "Caddy.") {
		t.Errorf("tidyFact cut to %q", got)
	}
	if !strings.HasSuffix(got, "is broken.") {
		t.Errorf("tidyFact should end on the last whole clause, got %q", got)
	}
}
