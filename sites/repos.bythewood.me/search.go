// Finding a file and finding a string, the half of reading a repository that
// walking a tree one directory at a time can't do. A caller that can only list
// and read spends its budget guessing down from the root, and one grep lands on it.
package main

import (
	"context"
	"strconv"
	"strings"
)

// GrepHit is one matching line, already trimmed to something a model can read
// without a screen of minified javascript arriving with it.
type GrepHit struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

const (
	// A search is meant to point at a file, and a hundred lines is already more
	// than a reader needs to decide where to look.
	grepMaxHits = 100

	// Without a per file cap a common identifier returns one file over and over.
	grepMaxPerFile = 20

	// A line longer than this is minified or generated, and the part past here
	// is never the part that matters.
	grepMaxLineLen = 300
)

// Grep searches file contents at a revision, fixed string and case insensitive
// since a regex a language model wrote by accident is worse than a literal that
// finds nothing. The pathspec goes to git so a search of one site skips the rest.
func (s *Store) Grep(ctx context.Context, repo Repo, rev, query, pathspec string, limit int) ([]GrepHit, bool, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, false, nil
	}
	if limit <= 0 || limit > grepMaxHits {
		limit = grepMaxHits
	}

	args := []string{
		"grep",
		// Skip binaries instead of reporting that one matched, which is all git
		// will say about them anyway.
		"-I",
		"-n", "-z", "--no-color",
		"--fixed-strings", "--ignore-case",
		"--max-count", strconv.Itoa(grepMaxPerFile),
		"-e", query,
		rev,
	}
	if p := strings.Trim(strings.TrimSpace(pathspec), "/"); p != "" {
		// A bare directory matches nothing on its own, so it's widened to
		// everything under it.
		if !strings.ContainsAny(p, "*?[") {
			p += "/*"
		}
		args = append(args, "--", p)
	}

	// git grep exits 1 with no output when nothing matched, which run reports as
	// an error, but nothing found is an answer.
	out, err := run(ctx, repo, args...)
	if err != nil {
		if len(out) == 0 {
			return nil, false, nil
		}
		return nil, false, err
	}

	prefix := rev + ":"
	var hits []GrepHit
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		// "<rev>:<path>\x00<lineno>\x00<text>", and the NULs are why the path
		// is safe to read off even when it has a colon in it.
		rest := strings.TrimPrefix(line, prefix)
		path, after, ok := strings.Cut(rest, "\x00")
		if !ok {
			continue
		}
		num, text, ok := strings.Cut(after, "\x00")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(num)
		if err != nil {
			continue
		}
		text = strings.TrimRight(text, "\r")
		if len(text) > grepMaxLineLen {
			text = text[:grepMaxLineLen] + "..."
		}
		hits = append(hits, GrepHit{Path: path, Line: n, Text: text})
		if len(hits) == limit {
			// A full page says it was cut short, so a caller doesn't read it as
			// the whole answer.
			return hits, true, nil
		}
	}
	return hits, false, nil
}

const findMaxPaths = 200

// FindPaths is every path at a revision whose name contains the query, so
// "earnings.go" turns into its full path without walking anything. An empty
// query lists the whole tree, capped, for the shape of a repository in one go.
func (s *Store) FindPaths(ctx context.Context, repo Repo, rev, query string, limit int) ([]string, bool, error) {
	if limit <= 0 || limit > findMaxPaths {
		limit = findMaxPaths
	}
	out, err := run(ctx, repo, "ls-tree", "-r", "--name-only", "-z", rev)
	if err != nil {
		return nil, false, err
	}

	needle := strings.ToLower(strings.TrimSpace(query))
	var found []string
	for _, path := range strings.Split(string(out), "\x00") {
		if path == "" {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(path), needle) {
			continue
		}
		found = append(found, path)
		if len(found) == limit {
			return found, true, nil
		}
	}
	return found, false, nil
}
