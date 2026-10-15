package tools

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type orchardCodeArgs struct {
	Repo   string
	Action string
	Query  string
	Path   string
	From   int
	To     int
	Rev    string
}

func (a *orchardCodeArgs) params() []param {
	return []param{
		strArg(&a.Repo, "repo", "the repository name, as orchard_repos lists it").required(),
		strArg(&a.Action, "action", "find a file by name, search file contents, read a file, or list a directory").
			oneOf("find", "search", "read", "list"),
		strArg(&a.Query, "query", "for find, part of a file name such as earnings.go; for search, the exact text to look for such as a function name"),
		strArg(&a.Path, "path", "for read and list, the full path; for find and search, an optional directory to stay inside"),
		intArg(&a.From, "from", "for read, the first line to return, optional"),
		intArg(&a.To, "to", "for read, the last line to return, optional"),
		strArg(&a.Rev, "rev", "a branch, tag or commit, optional, defaults to the default branch"),
	}
}

// OrchardCode says what's in a repository where orchard_repos says what exists,
// or a question about Isaac's code ends in guessed addresses and 404s. find and
// search let the first call land on the file instead of walking to it.
var OrchardCode = Define(Tool{
	Name: "orchard_code",
	Description: "Read the source of one of Isaac's repositories on repos.bythewood.me, read only, " +
		"and it can never change anything. Four actions: find locates a file by name anywhere in " +
		"the repository, search finds a string inside the files and gives the path and line of each " +
		"hit, read returns one file, list shows one directory. Start with find or search rather " +
		"than walking down from the top, since a name or a symbol gets there in one call. On a long " +
		"file read the lines around a hit with from and to instead of the whole thing. Never guess " +
		"a url for his code and never fetch one, this is the only way in.",
}, func(ctx context.Context, d *Deps, a orchardCodeArgs) (any, error) {
	asked := strings.Trim(a.Repo, "/")
	repo := repoName(asked)
	if repo == "" {
		return nil, fmt.Errorf("repo is required, and orchard_repos lists the names")
	}
	rev := a.Rev
	if rev == "" {
		rev = "HEAD"
	}
	path := strings.Trim(a.Path, "/")
	if path == asked || path == repo {
		path = ""
	}
	query := a.Query
	base := reposBase + "/api/repos/" + url.PathEscape(repo)

	// The action is what the model said it wants, but a call naming one
	// thing and asking for another is common enough that the arguments
	// decide when they disagree. A query with no action is a search.
	switch action := strings.ToLower(a.Action); {
	case action == "find":
		q := url.Values{}
		q.Set("q", query)
		var out map[string]any
		err := estateGet(ctx, d, base+"/find/"+url.PathEscape(rev)+"?"+q.Encode(), &out)
		if err != nil {
			return nil, noRepo(err, repo, rev)
		}
		// find matches file names, and the model reaches for it with the name
		// of a constant or a function, which is in a file and doesn't name one.
		if n, _ := out["count"].(float64); n == 0 && query != "" {
			var hits map[string]any
			if estateGet(ctx, d, base+"/grep/"+url.PathEscape(rev)+"?"+q.Encode(), &hits) == nil {
				hits["note"] = "No file is named that, so these are the places the text appears inside files."
				return hits, nil
			}
		}
		return out, nil

	case action == "search" || (action == "" && query != ""):
		if query == "" {
			return nil, fmt.Errorf("search needs a query, which is the text to look for")
		}
		q := url.Values{}
		q.Set("q", query)
		if path != "" {
			q.Set("path", path)
		}
		var out map[string]any
		err := estateGet(ctx, d, base+"/grep/"+url.PathEscape(rev)+"?"+q.Encode(), &out)
		return out, noRepo(err, repo, rev)
	}

	return orchardWalk(ctx, d, base, repo, rev, path, a.From, a.To)
})

// orchardWalk is read and list in one call, since a path with no dot in its last
// segment is usually a directory and guessing wrong costs a round. The read goes
// first so an extensionless file still reads and a directory still lists.
func orchardWalk(ctx context.Context, d *Deps, base, repo, rev, path string, from, to int) (any, error) {
	if path != "" {
		q := url.Values{}
		if from > 0 {
			q.Set("from", strconv.Itoa(from))
		}
		if to > 0 {
			q.Set("to", strconv.Itoa(to))
		}
		fileURL := base + "/file/" + url.PathEscape(rev) + "/" + escapePath(path)
		if len(q) > 0 {
			fileURL += "?" + q.Encode()
		}
		var file map[string]any
		err := estateGet(ctx, d, fileURL, &file)
		if err == nil {
			return file, nil
		}
		// Only a missing file falls through. A refused session or a binary
		// file is the answer, and listing the directory would bury it.
		if !errors.Is(err, errEstateMissing) {
			return nil, err
		}
	}
	treeURL := base + "/tree/" + url.PathEscape(rev)
	if path != "" {
		treeURL += "/" + escapePath(path)
	}
	var tree map[string]any
	if err := estateGet(ctx, d, treeURL, &tree); err != nil {
		if path == "" {
			return nil, err
		}
		// A wrong path is the commonest mistake here, since the model drops a
		// directory from the front, so the error carries the way out instead of
		// the level above.
		return nil, fmt.Errorf("%s has no file or directory at %q. Call again with "+
			"action find and query %q to get its real path", repo, path, lastSegment(path))
	}
	return tree, nil
}

// noRepo says a 404 from find or search is a repository or revision that isn't
// there, since "no such path" sends the model looking for a path.
func noRepo(err error, repo, rev string) error {
	if errors.Is(err, errEstateMissing) {
		return fmt.Errorf("there is no repository called %q at %q on repos, orchard_repos lists the names", repo, rev)
	}
	return err
}

// repoName is the name repos knows a repository by. The model passes GitHub's
// owner/name, or the whole url, since that is how code is usually named.
func repoName(raw string) string {
	s := strings.TrimSuffix(strings.Trim(strings.TrimSpace(raw), "/"), ".git")
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// lastSegment goes to find when a path was wrong, since the file name is
// the part the model usually has right.
func lastSegment(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 && i+1 < len(p) {
		return p[i+1:]
	}
	return p
}

// escapePath escapes each segment and keeps the separators, since the whole
// path is one wildcard on the other side and escaping it whole would turn every
// slash into %2F.
func escapePath(p string) string {
	if p == "" {
		return ""
	}
	parts := strings.Split(p, "/")
	for i, seg := range parts {
		parts[i] = url.PathEscape(seg)
	}
	return strings.Join(parts, "/")
}
