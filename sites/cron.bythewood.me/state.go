package main

import (
	"net/http"
	"strconv"

	"cron.bythewood.me/suite"
)

// suiteState is cron's entry in the merged index, every job and whether its
// last run failed.
func (s *site) suiteState(r *http.Request) suite.State {
	jobs, err := s.store.Jobs()
	if err != nil {
		return suite.State{Tool: "cron", Note: "couldn't read the jobs"}
	}
	sortForAttention(jobs)
	base := "https://cron.bythewood.me"
	st := suite.State{Tool: "cron", Level: "ok", Items: []suite.Item{
		{Kind: "page", Name: "jobs", Href: base + "/", Note: "every reminder and check"},
		{Kind: "page", Name: "runs", Href: base + "/runs", Note: "every run, newest first"},
		{Kind: "page", Name: "new reminder", Href: base + "/jobs/new", Note: "push fixed text at a time"},
		{Kind: "page", Name: "new check", Href: base + "/jobs/new?kind=prompt", Note: "the model looks something up"},
		{Kind: "page", Name: "keys", Href: base + "/keys", Note: "tokens for the api"},
	}}
	for _, j := range jobs {
		it := suite.Item{Kind: kindName(j.Kind), Name: j.Name, Href: base + "/jobs/" + strconv.FormatInt(j.ID, 10)}
		switch {
		case !j.Enabled:
			it.Note = "paused"
		case j.LastErr != "":
			it.Level, it.Note = "bad", "failed · "+clip(j.LastErr, 80)
			st.Count++
		default:
			it.Note = s.describe(j) + " · next " + until(j.NextRun)
		}
		st.Items = append(st.Items, it)
	}
	sum := summarize(jobs)
	st.Note = plural(sum.Active, "job", "jobs") + " on"
	if st.Count > 0 {
		st.Level = "bad"
		st.Note = plural(st.Count, "job", "jobs") + " failing"
	}
	return st
}
