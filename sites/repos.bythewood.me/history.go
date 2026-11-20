package main

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
)

// memo holds the reads a repository page repeats on every view, keyed by
// repository name first so a push can drop everything it made stale.
type memo struct {
	mu sync.Mutex
	m  map[string]memoEntry
}

type memoEntry struct {
	value any
	at    time.Time
}

const (
	memoTTL = 10 * time.Minute
	memoMax = 512
)

func newMemo() *memo { return &memo{m: make(map[string]memoEntry)} }

func (c *memo) get(key string, ttl time.Duration) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok || (ttl > 0 && time.Since(e.at) > ttl) {
		return nil, false
	}
	return e.value, true
}

func (c *memo) put(key string, v any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) >= memoMax {
		// Map order is random, so this throws out an arbitrary half.
		n := 0
		for k := range c.m {
			delete(c.m, k)
			if n++; n >= memoMax/2 {
				break
			}
		}
	}
	c.m[key] = memoEntry{value: v, at: time.Now()}
}

func (c *memo) drop(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.m {
		if strings.HasPrefix(k, name+"\x00") {
			delete(c.m, k)
		}
	}
}

func memoKey(repo Repo, parts ...string) string {
	return repo.Name + "\x00" + strings.Join(parts, "\x00")
}

// historyWeeks is how far back the commit ticks go.
const historyWeeks = 52

// Weekly counts commits reachable from rev in each of the last 52 weeks, oldest
// first, with the last bucket ending now.
func (s *Store) Weekly(ctx context.Context, repo Repo, rev string) []int {
	key := memoKey(repo, "weekly", rev)
	if v, ok := s.memo.get(key, memoTTL); ok {
		return v.([]int)
	}
	now := time.Now()
	since := now.Add(-historyWeeks * 7 * 24 * time.Hour)
	// The -n cap is a backstop for a repository with a busy machine committer.
	out, err := run(ctx, repo, "log", "--format=%ct", "-n", "50000",
		"--since="+strconv.FormatInt(since.Unix(), 10), rev, "--")
	weeks := make([]int, historyWeeks)
	if err != nil {
		return weeks
	}
	weeks = bucketWeeks(out, now)
	s.memo.put(key, weeks)
	return weeks
}

func bucketWeeks(out []byte, now time.Time) []int {
	weeks := make([]int, historyWeeks)
	for line := range strings.SplitSeq(string(out), "\n") {
		ts, err := strconv.ParseInt(strings.TrimSpace(line), 10, 64)
		if err != nil {
			continue
		}
		age := now.Sub(time.Unix(ts, 0))
		i := historyWeeks - 1 - int(age/(7*24*time.Hour))
		if age < 0 {
			i = historyWeeks - 1
		}
		if i >= 0 && i < historyWeeks {
			weeks[i]++
		}
	}
	return weeks
}

// Count is CountCommits behind the memo, since it walks the whole graph.
func (s *Store) Count(ctx context.Context, repo Repo, rev string) int {
	key := memoKey(repo, "count", rev)
	if v, ok := s.memo.get(key, memoTTL); ok {
		return v.(int)
	}
	n := s.CountCommits(ctx, repo, rev)
	s.memo.put(key, n)
	return n
}

// maxPaths caps the go to file list, which is held in memory and sent whole.
const maxPaths = 50000

// Paths is every file at rev, for go to file. The tree's id keys the memo, so
// a push that leaves the tree alone still hits it.
func (s *Store) Paths(ctx context.Context, repo Repo, rev string) ([]string, error) {
	tree, err := s.TreeID(ctx, repo, rev, "")
	if err != nil {
		return nil, err
	}
	key := memoKey(repo, "paths", tree)
	if v, ok := s.memo.get(key, 0); ok {
		return v.([]string), nil
	}
	out, err := run(ctx, repo, "ls-tree", "-r", "-z", "--name-only", tree)
	if err != nil {
		return nil, err
	}
	var paths []string
	for p := range strings.SplitSeq(string(out), "\x00") {
		if p == "" {
			continue
		}
		if len(paths) == maxPaths {
			break
		}
		paths = append(paths, p)
	}
	s.memo.put(key, paths)
	return paths, nil
}

// TreeID is the id of the directory at path in rev.
func (s *Store) TreeID(ctx context.Context, repo Repo, rev, path string) (string, error) {
	out, err := run(ctx, repo, "rev-parse", "--verify", "--quiet", rev+"^{tree}:"+path)
	if path == "" {
		out, err = run(ctx, repo, "rev-parse", "--verify", "--quiet", rev+"^{tree}")
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// LastCommit is the newest commit to touch one entry of a directory.
type LastCommit struct {
	SHA     string    `json:"sha"`
	Short   string    `json:"short"`
	Subject string    `json:"subject"`
	When    time.Time `json:"when"`
}

// How far back LastCommits looks. An entry untouched in that many commits, or
// in that long, is left blank instead of walking the rest of the history.
const (
	lastCommitsMax     = 2000
	lastCommitsTimeout = 5 * time.Second
)

// lastSem keeps two of these walks at most on a two core box.
var lastSem = make(chan struct{}, 2)

// LastCommits finds the newest commit touching each entry under dir, in one
// streamed git log that stops as soon as every entry has one. Keyed by the
// directory's tree id, which only changes when something under it does.
func (s *Store) LastCommits(ctx context.Context, repo Repo, rev, dir string, names []string) (map[string]LastCommit, error) {
	tree, err := s.TreeID(ctx, repo, rev, dir)
	if err != nil {
		return nil, err
	}
	key := memoKey(repo, "last", tree)
	if v, ok := s.memo.get(key, 0); ok {
		return v.(map[string]LastCommit), nil
	}

	select {
	case lastSem <- struct{}{}:
		defer func() { <-lastSem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	ctx, cancel := context.WithTimeout(ctx, lastCommitsTimeout)
	defer cancel()
	args := []string{"log", "-z", "--no-renames", "--name-only",
		"--format=%x01%H%x00%h%x00%ct%x00%s",
		"-n", strconv.Itoa(lastCommitsMax), rev, "--"}
	if dir != "" {
		args = append(args, dir)
	}
	cmd := gitCmd(ctx, repo, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	found := scanLastCommits(stdout, dir, names)
	cancel()
	_ = cmd.Wait()

	// A walk cut short by the timeout is still worth showing, but not keeping.
	if len(found) == len(names) || ctx.Err() == nil {
		s.memo.put(key, found)
	}
	return found, nil
}

// scanLastCommits reads log -z --name-only records, each "\x01sha\0short\0ct\0subject\0\n"
// then the paths it changed, and gives each wanted name the first commit naming it.
func scanLastCommits(r io.Reader, dir string, names []string) map[string]LastCommit {
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	found := make(map[string]LastCommit, len(names))
	prefix := ""
	if dir != "" {
		prefix = dir + "/"
	}
	br := bufio.NewReaderSize(r, 64<<10)
	for len(found) < len(want) {
		rec, err := br.ReadBytes('\x01')
		rec = bytes.TrimSuffix(rec, []byte{'\x01'})
		if len(rec) > 0 {
			f := strings.Split(string(rec), "\x00")
			if len(f) >= 4 {
				ts, _ := strconv.ParseInt(f[2], 10, 64)
				c := LastCommit{SHA: f[0], Short: f[1], When: time.Unix(ts, 0).UTC(), Subject: f[3]}
				for _, p := range f[4:] {
					p = strings.TrimLeft(p, "\n")
					rest, ok := strings.CutPrefix(p, prefix)
					if !ok || rest == "" {
						continue
					}
					name, _, _ := strings.Cut(rest, "/")
					if want[name] {
						if _, seen := found[name]; !seen {
							found[name] = c
						}
					}
				}
			}
		}
		if err != nil {
			break
		}
	}
	return found
}
