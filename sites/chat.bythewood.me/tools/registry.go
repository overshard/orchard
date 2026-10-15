package tools

// Default is every tool the chat is offered, and the schemas and the dispatch
// table are both built from it so a tool can't exist in one and not the other.
func Default() *Registry {
	r := &Registry{}
	for _, t := range []Tool{
		WebSearch, WebFetch, News, Remember, Weather, Markets, SportsScores, Odds, MusicLookup, Convert, Calc, Now,
		Wikipedia, Dictionary,
		OrchardLogs, OrchardStatus, OrchardAnalytics, OrchardRepos, OrchardCode, OrchardDash,
		ChatHistory, XSearch,
		PropertyTool, Mortgage,
		Image,
	} {
		r.Add(t)
	}
	return r
}
