package main

import (
	"strings"
	"sync"
)

// The record of what a turn actually did, kept so the page can show it.
//
// All of this already happened and none of it was ever visible, so an answer
// written from the model's memory looked exactly like one read off a tool. A
// step says what went in and what came out, and the page draws them as a list
// under the answer.
//
// It is written next to the message, so it is bounded on purpose: a system
// prompt is several thousand characters and a page of them per turn would grow
// the database faster than the conversation does.

// What one field of one step may store.
const stepMax = 1200

// How many steps a turn may record. Six tool rounds with a gate and a nudge
// each is comfortably inside this, and a runaway turn cannot write a megabyte.
const stepsMax = 60

type Step struct {
	// One of: prompt, memory, wikipedia, model, tool, gate, answer, title,
	// compact. The page styles by this and shows the label as written.
	Kind  string `json:"kind"`
	Label string `json:"label"`
	In    string `json:"in,omitempty"`
	Out   string `json:"out,omitempty"`
	MS    int64  `json:"ms,omitempty"`
	// Something failed or was rejected, which the page colours rather than
	// hides, since a refused tool is part of the work.
	Bad bool `json:"bad,omitempty"`
	// Free text for whatever the step counts, like "1,204 in / 86 out".
	Meta string `json:"meta,omitempty"`
}

// Trace collects the steps of one turn and sends each one out as it happens,
// so the list fills in while the answer is still being written.
type Trace struct {
	mu    sync.Mutex
	steps []Step
	emit  func(Event)
}

func NewTrace(emit func(Event)) *Trace {
	if emit == nil {
		emit = func(Event) {}
	}
	return &Trace{emit: emit}
}

// Add records a step. It is safe from any goroutine, since the memory pass and
// the turn both write here.
func (t *Trace) Add(s Step) {
	if t == nil {
		return
	}
	s.In, s.Out = clip(s.In), clip(s.Out)
	t.mu.Lock()
	if len(t.steps) >= stepsMax {
		t.mu.Unlock()
		return
	}
	t.steps = append(t.steps, s)
	t.mu.Unlock()
	t.emit(Event{Kind: "step", Step: &s})
}

func (t *Trace) Steps() []Step {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]Step(nil), t.steps...)
}

// clip keeps a field inside the budget and says how much it dropped, since a
// prompt cut off with no mark reads as a prompt that was that short.
func clip(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= stepMax {
		return s
	}
	return strings.TrimSpace(s[:stepMax]) + "\n… " + itoa(len(s)-stepMax) + " more characters"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
