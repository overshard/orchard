package main

import (
	"slices"

	"chat.bythewood.me/tools"
)

// Text off the web is written by strangers, and a page can tell the model to
// read his history or his logs and put them in the next URL it fetches. own is
// strangers' text that came out of his own sites, like a request path in the logs.
func readUntrusted(used []tools.Result) (outside, own bool) {
	for _, r := range used {
		t := tools.Of(r.Name)
		if r.Err != "" || !t.Is(tools.Untrusted) {
			continue
		}
		if t.Is(tools.Estate) {
			own = true
		} else {
			outside = true
		}
	}
	return outside, own
}

// The other way round, a request path in his logs can tell the model to put
// what it just read into a URL. Once anything private has answered in a turn,
// nothing that sends text off the machine is offered.
func readPrivate(used []tools.Result) bool {
	return slices.ContainsFunc(used, func(r tools.Result) bool {
		return r.Err == "" && tools.Of(r.Name).Is(tools.Private)
	})
}

// After the web nothing private is offered. After his own sites the others stay
// open, since reading one already sealed everything Outbound, but remember still
// closes, because a note is how an instruction outlives the turn.
func shut(t tools.Tool, outside, own bool) bool {
	if outside {
		return t.Is(tools.Private)
	}
	return own && t.Is(tools.Private) && !t.Is(tools.Estate)
}

// A memory pass after strangers' text would write their instructions down as
// something he said, and a recalled memory is never closed off like a result.
func learnsFrom(used []tools.Result) bool {
	outside, own := readUntrusted(used)
	return !outside && !own
}
