// A message that is only an instruction to write something down.
//
// "Remember dash isn't showing oracle in earnings block when I think their
// earnings are today" is a note, and the turn it got was six tool calls: the
// time, the whole dashboard, the repository list and three walks into the
// source, which ran the round budget out on a wrong guess and never called
// remember at all. Isaac had to say "Remember this to fix later" to get the one
// call he asked for the first time.
//
// The prompt already says to call remember when he asks. A prompt is a request,
// and the fix for a model that reads an instruction as the start of a job is the
// same one the gate uses: take the other tools off the table so the only thing
// it can do is the thing that was asked.
package main

import (
	"regexp"
	"strings"
)

// The openings that mean write this down. "for me", "for later" and "this then"
// sit between the verb and the note often enough to be part of the pattern.
var noteOpening = regexp.MustCompile(`(?i)^\s*(ok(ay)?|so|and|also|hey)?[\s,-]*` +
	`(please\s+)?(remember|note|jot down|write down|make a note|take a note|keep in mind|` +
	`add to memory|add to your memory|update the memory|update your memory|save to memory|forget)\b`)

// A question is a question even when it opens like a note. "remember when we
// talked about the tunnel? what did we settle on" wants the history, not a new
// fact, and forcing remember there would answer the wrong half.
var carriesAQuestion = regexp.MustCompile(`\?|(?i)\b(can you|could you|what|why|how|when|where|which|who)\b.*\?`)

// isNote reports whether the message asks for something to be written down and
// nothing else. Only then is it safe to take every other tool away.
func isNote(message string) bool {
	m := strings.TrimSpace(message)
	if m == "" || !noteOpening.MatchString(m) {
		return false
	}
	// A note carries its own content. A bare "remember" with nothing after it is
	// somebody mid sentence, and there is nothing yet to store.
	if len(strings.Fields(m)) < 3 {
		return false
	}
	return !carriesAQuestion.MatchString(m)
}
