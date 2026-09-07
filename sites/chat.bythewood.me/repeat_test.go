package main

import "testing"

// A first answer that searched, then the same answer written out again under a
// follow-up that asked something new. That is the shape Isaac hit twice in one
// conversation: the tools ran on the first question and never again, and every
// later reply was the first one reworded. The third is the control, a follow-up
// that went somewhere the first answer does not cover.
const (
	firstAnswer = `Crossings are down rather than up. Between 1 January and 5 September 2026, 16,513 people made the crossing, which is 43% below the same period last year. Almost everyone who arrives claims asylum and is allowed to stay while the claim is decided.

The buses are the dispersal process. Arrivals land in one county and are moved to hotels and shared housing across the country. About 18% of the 89,089 people in asylum accommodation in June 2026 were in hotels, and the government has said it will stop using hotels by 2029.

Applications fell 21% to 85,891 over the year to June 2026, the backlog fell 56%, and returns rose 8% to 40,605. Sources: https://example.org/a and https://example.org/b`

	rehash = `I cannot answer this from the tool results. The sources I read show the opposite of what the question assumes. Crossings are down rather than up, 43% below the same period last year, applications fell 21% and the backlog fell 56%.

The buses are the dispersal process. Arrivals land in one county and are moved to hotels and shared housing across the country. About 18% of the 89,089 people in asylum accommodation in June 2026 were in hotels, and the government has said it will stop using hotels by 2029. Almost everyone who arrives claims asylum and is allowed to stay while the claim is decided.

Sources: https://example.org/a and https://example.org/b`

	movedOn = `Fair, the split is not really between one country and another. Ports on both coasts handle their own arrivals and the counts differ by season more than by policy, so a year on year figure hides most of what you are asking about. The Home Office publishes a quarterly table by port of entry, and Dover and Folkestone sit well above everywhere else in it, which is the number that would settle this. https://example.org/c`
)

func TestRepeatsAnswered(t *testing.T) {
	if !repeatsAnswered(rehash, []string{firstAnswer}) {
		t.Error("a follow-up that rewrote the previous answer was let through")
	}
	if !repeatsAnswered(rehash, []string{"unrelated earlier reply", firstAnswer}) {
		t.Error("the match has to be against every earlier answer, not just the last")
	}
	if repeatsAnswered(movedOn, []string{firstAnswer, rehash}) {
		t.Error("a follow-up that moved on was called a repeat")
	}
	if repeatsAnswered(rehash, nil) {
		t.Error("there was nothing to repeat and it still said yes")
	}
	// A short reply shares its whole vocabulary with what came before, so the
	// overlap says nothing about whether it answered.
	if repeatsAnswered("Yes, that is right, the crossings are down 43% on last year.", []string{firstAnswer}) {
		t.Error("a short reply was measured for overlap")
	}
}

func TestRefusal(t *testing.T) {
	if !refusal.MatchString(rehash) {
		t.Error("missed a refusal to answer from the tool results")
	}
	for _, s := range []string{firstAnswer, movedOn, "I cannot answer that until the market opens."} {
		if refusal.MatchString(s) {
			t.Errorf("ate a real answer: %.60q", s)
		}
	}
}
