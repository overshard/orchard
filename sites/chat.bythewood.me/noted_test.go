package main

import "testing"

// The five Isaac actually sent. Every one of them is a note and nothing else,
// and the one that went wrong is the Oracle line, which spent six tool calls
// and the whole round budget before answering with a guess.
func TestIsNoteOnTheRealOnes(t *testing.T) {
	for _, m := range []string{
		"Remember dash isn't showing oracle in earnings block when I think their earnings are today",
		"remember for me that i want to build in xcancel into chat tooling in some way here",
		"remember for later -- need better system when swapping between chats with active live chat",
		"okay -- remember this then -- i want to maybe try doing a small android app for you",
		"Remember I need to fix on the chat ui here the top bar on mobile, it always says new conversation",
		"Remember tonight to look into why chat here flagged no search",
		"add to memory that i want to add a new tool to bythewood chat",
		"Remember this to fix later",
	} {
		if !isNote(m) {
			t.Errorf("isNote(%q) = false, want true", m)
		}
	}
}

// The note that asked for a lookup and was offered remember alone, so it wrote
// down three films from memory until the third call ran into the token cap.
// "look into why" is a job for later and not a lookup now.
func TestANoteThatAsksForALookupGetsTheLookups(t *testing.T) {
	for _, m := range []string{
		"remember that my friends and I want to watch heretic, alien romulus, and terrifier this weekend, look up basic details and name of each and add them to memory",
		"remember the three campsites near boone, look them up first",
		"note that i want a new tent, search for one under 300 dollars",
		"remember to find out when the yadkin valley fair is",
	} {
		if !isNote(m) || !wantsLookup(m) {
			t.Errorf("isNote(%q) = %v, wantsLookup = %v, want both", m, isNote(m), wantsLookup(m))
		}
	}
	for _, m := range []string{
		"Remember tonight to look into why chat here flagged no search",
		"Remember dash isn't showing oracle in earnings block when I think their earnings are today",
		"add to memory that i want to add a new tool to bythewood chat",
	} {
		if !isNote(m) || wantsLookup(m) {
			t.Errorf("isNote(%q) = %v, wantsLookup = %v, want a plain note", m, isNote(m), wantsLookup(m))
		}
	}
}

// A question that opens like a note still wants an answer, and forcing remember
// there would write a fact down instead of answering.
func TestIsNoteLeavesAQuestionAlone(t *testing.T) {
	for _, m := range []string{
		"remember when we talked about the tunnel? what did we settle on",
		"do you remember what i said about the rifle?",
		"what do you remember about me",
		"can you remember things between chats?",
		"remember",
		"why is dash not showing oracle",
		"check the logs for errors",
	} {
		if isNote(m) {
			t.Errorf("isNote(%q) = true, want false", m)
		}
	}
}
