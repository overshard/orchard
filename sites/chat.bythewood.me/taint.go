package main

import (
	"slices"

	"chat.bythewood.me/tools"
)

// Text off the web is written by strangers, and a page can tell the model to
// read his history or his logs and put them in the next URL it fetches. Once
// any of these has answered in a turn, nothing private is offered.
var untrustedTools = []string{
	tools.WebSearch.Name, tools.WebFetch.Name, tools.XSearch.Name, tools.News.Name,
}

var privateTools = []string{
	tools.ChatHistory.Name, tools.Remember.Name,
	tools.OrchardLogs.Name, tools.OrchardStatus.Name, tools.OrchardAnalytics.Name,
	tools.OrchardRepos.Name, tools.OrchardCode.Name, tools.OrchardDash.Name,
}

func readUntrusted(used []tools.Result) bool {
	return slices.ContainsFunc(used, func(r tools.Result) bool {
		return r.Err == "" && slices.Contains(untrustedTools, r.Name)
	})
}

func withoutPrivate(offer []map[string]any) []map[string]any {
	for _, name := range privateTools {
		offer = tools.Without(offer, name)
	}
	return offer
}
