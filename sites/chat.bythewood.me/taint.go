package main

import (
	"slices"

	"chat.bythewood.me/tools"
)

// Text off the web is written by strangers, and a page can tell the model to
// read his history or his logs and put them in the next URL it fetches. Once
// any untrusted tool has answered in a turn, nothing private is offered.
func readUntrusted(used []tools.Result) bool {
	return slices.ContainsFunc(used, func(r tools.Result) bool {
		return r.Err == "" && tools.Of(r.Name).Is(tools.Untrusted)
	})
}
