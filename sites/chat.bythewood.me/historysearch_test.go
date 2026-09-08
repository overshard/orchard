package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// A store with two conversations that share a word, so ranking has something to
// get wrong.
func searchStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStore(filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	rifles, _ := s.NewConversation("Cheap bolt action rimfire rifles")
	_ = s.Append(rifles, Stored{Role: RoleUser, Content: "what are the most common cheap bolt action 22 rifles for plinking"})
	_ = s.Append(rifles, Stored{Role: RoleAssistant, Content: "The Savage Mark II and the Ruger American Rimfire are the two that show up everywhere."})

	food, _ := s.NewConversation("Bojangles nutrition")
	_ = s.Append(food, Stored{Role: RoleUser, Content: "is the dirty rice better than the pinto beans"})
	_ = s.Append(food, Stored{Role: RoleAssistant, Content: "The pinto beans have 7g of protein and no saturated fat, so they beat the dirty rice."})
	return s
}

func TestSearchHistoryFindsTheRightExchange(t *testing.T) {
	s := searchStore(t)

	hits, err := s.SearchHistory("pinto beans protein", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("a question that was answered found nothing")
	}
	if !strings.Contains(hits[0].Answer, "pinto beans") {
		t.Errorf("the top hit is %q, want the beans exchange", hits[0].Answer)
	}
	// A hit carries what was asked as well as what was said, since an answer on
	// its own reads as an assertion from nowhere.
	if !strings.Contains(hits[0].Question, "dirty rice") {
		t.Errorf("the hit has no question on it: %q", hits[0].Question)
	}
	if hits[0].Title == "" {
		t.Error("the hit does not say which conversation it came from")
	}
}

// The stemmer is what makes a real question reach a stored one. Without it
// "rifle" never finds "rifles", which is the same failure memory hit first.
func TestSearchHistoryStems(t *testing.T) {
	s := searchStore(t)
	hits, err := s.SearchHistory("rifle", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal(`"rifle" did not reach "rifles"`)
	}
	if !strings.Contains(hits[0].Title, "rimfire") {
		t.Errorf("top hit is %q, want the rifles conversation", hits[0].Title)
	}
}

// Ten rows off one long thread is the same answer ten times, and it crowds out
// every other conversation that matched.
func TestSearchHistoryReturnsOneHitPerConversation(t *testing.T) {
	s := searchStore(t)
	id, _ := s.NewConversation("Rifles again")
	for i := 0; i < 6; i++ {
		_ = s.Append(id, Stored{Role: RoleUser, Content: "more about rifles and rifles"})
		_ = s.Append(id, Stored{Role: RoleAssistant, Content: "still about rifles"})
	}
	hits, err := s.SearchHistory("rifles", 10)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, h := range hits {
		seen[h.ConvID]++
	}
	for id, n := range seen {
		if n > 1 {
			t.Errorf("conversation %s came back %d times", id, n)
		}
	}
}

// A query of nothing but stopwords has no subject in it, and searching on that
// would return whatever is newest and present it as a match.
func TestSearchHistoryRefusesAQueryWithNoSubject(t *testing.T) {
	s := searchStore(t)
	if _, err := s.SearchHistory("what is the it", 5); err == nil {
		t.Error("a query with no searchable word was accepted")
	}
}
