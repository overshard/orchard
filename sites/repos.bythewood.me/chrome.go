package main

import (
	"net/http"
	"strings"
	"time"

	"repos.bythewood.me/suite"
)

// chrome is the header and status line. Inside a repository the second row is
// that repository's own pages, since that's where a reader moves around.
func chrome(r *http.Request, signedIn bool, repo string) suite.Page {
	p := suite.Page{Tool: "repos", SignedIn: signedIn, Source: sourceURL, Author: authorName, Year: time.Now().Year()}
	path := r.URL.Path
	if repo == "" {
		p.Tabs = []suite.Tab{{Name: "repositories", Href: "/", On: path == "/"}}
		if signedIn {
			p.Tabs = append(p.Tabs, suite.Tab{Name: "settings", Href: "/settings", On: path == "/settings"})
		}
		p.Tabs = append(p.Tabs, suite.Tab{Name: "source", Href: sourceURL})
		p.Meta = "clone over https · push with a token"
	} else {
		base := "/" + repo
		rest := strings.TrimPrefix(path, base)
		p.Tabs = []suite.Tab{
			{Name: "repositories", Href: "/"},
			{Name: "files", Href: base, On: rest == "" || strings.HasPrefix(rest, "/tree") || strings.HasPrefix(rest, "/blob")},
		}
		if signedIn {
			p.Tabs = append(p.Tabs, suite.Tab{Name: "history", Href: base + "/log", On: strings.HasPrefix(rest, "/log") || strings.HasPrefix(rest, "/commit")})
		}
		p.Tabs = append(p.Tabs,
			suite.Tab{Name: "branches", Href: base + "/branches", On: rest == "/branches"},
			suite.Tab{Name: "tags", Href: base + "/tags", On: rest == "/tags"})
	}
	p.Status = []suite.Seg{{Text: "git over https"}}
	if repo != "" {
		p.Status = append(p.Status, suite.Seg{Text: repo})
	}
	p.Right = []suite.Seg{{Text: "times are UTC"}}
	return p
}

// footerFor is the four column footer on the signed out index.
func footerFor(p page) suite.Footer {
	return suite.Footer{Tool: "repos", About: "Hosts my git repositories, mirrors the archived ones from GitHub and puts a browser on all of it.",
		Source: sourceURL, Author: authorName, Year: p.Year, Pages: []suite.Tab{{Name: "Repositories", Href: "/"}}}
}
