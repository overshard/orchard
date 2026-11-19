package main

import (
	"fmt"
	"net/http"
	"net/url"

	"repos.bythewood.me/suite"
)

// suiteState is repos' entry in the merged index, every repository with a
// failed mirror sync called out.
func (s *site) suiteState(r *http.Request) suite.State {
	repos, err := s.store.Discover()
	if err != nil {
		return suite.State{Tool: "repos", Note: "couldn't list the repositories"}
	}
	meta, _ := s.db.AllRepos()
	base := "https://repos.bythewood.me"
	st := suite.State{Tool: "repos", Level: "ok", Items: []suite.Item{
		{Kind: "page", Name: "repositories", Href: base + "/", Note: "last pushed first"},
		{Kind: "page", Name: "settings", Href: base + "/settings", Note: "push tokens and mirrors"},
	}}
	shown := 0
	for _, repo := range repos {
		m := meta[repo.Name]
		if m.Hidden {
			continue
		}
		shown++
		it := suite.Item{Kind: "repo", Name: repo.Name, Href: base + "/" + url.PathEscape(repo.Name), Note: m.Description}
		if m.Mirror && it.Note == "" {
			it.Note = "mirror"
		}
		if m.LastSyncErr != "" {
			it.Level, it.Note = "bad", "sync failed · "+m.LastSyncErr
			st.Count++
		}
		st.Items = append(st.Items, it)
	}
	st.Note = fmt.Sprintf("%d %s", shown, pluralize(shown, "repository", "repositories"))
	if st.Count > 0 {
		st.Level = "bad"
		st.Note = fmt.Sprintf("%d %s failing to sync", st.Count, pluralize(st.Count, "mirror", "mirrors"))
	}
	return st
}
