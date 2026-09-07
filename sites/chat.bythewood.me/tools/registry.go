package tools

// Default is every tool the chat is offered. Adding one here is the only step:
// the schemas handed to the model and the dispatch table are both built from
// this, so a tool cannot exist in one and not the other.
func Default() *Registry {
	r := &Registry{}
	for _, t := range []Tool{
		WebSearch, WebFetch, News, Weather, Markets, SportsScores, Odds, MusicLookup, Convert, Calc, Now,
		Wikipedia,
		OrchardLogs, OrchardStatus, OrchardAnalytics, OrchardRepos, OrchardDash,
		DeepSearch,
	} {
		r.Add(t)
	}
	return r
}
