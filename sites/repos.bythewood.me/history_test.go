package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBucketWeeks(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	day := int64(24 * 60 * 60)
	out := []byte(strings.Join([]string{
		"1800000000",
		"1799990000",
		itoa(1_800_000_000 - 8*day),
		itoa(1_800_000_000 - 400*day),
		"junk",
	}, "\n"))
	w := bucketWeeks(out, now)
	if w[historyWeeks-1] != 2 || w[historyWeeks-2] != 1 || sum(w) != 3 {
		t.Errorf("buckets came out %v", w[historyWeeks-3:])
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func sum(v []int) (t int) {
	for _, x := range v {
		t += x
	}
	return t
}

func TestScanLastCommits(t *testing.T) {
	log := "\x01aaa\x00aa\x001800000000\x00Touch b\x00\nsrc/b/x.go\x00\x01bbb\x00bb\x001790000000\x00Touch a and b\x00\nsrc/a.go\x00src/b/y.go\x00src/c.go\x00"
	got := scanLastCommits(strings.NewReader(log), "src", []string{"a.go", "b", "c.go", "d.go"})
	if got["b"].SHA != "aaa" || got["a.go"].SHA != "bbb" || got["c.go"].Subject != "Touch a and b" {
		t.Errorf("got %+v", got)
	}
	if _, ok := got["d.go"]; ok {
		t.Error("an untouched entry got a commit")
	}
}

func TestLastCommitsWalk(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := t.TempDir()
	work := filepath.Join(root, "work")
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = work
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@example.com", "GIT_CONFIG_NOSYSTEM=1", "HOME="+root)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	write := func(p, body string) {
		t.Helper()
		full := filepath.Join(work, p)
		os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	os.MkdirAll(work, 0o755)
	git("init", "-q", "-b", "main")
	write("src/a.go", "a")
	write("src/b/x.go", "x")
	write("top.md", "t")
	git("add", "-A")
	git("commit", "-qm", "Start")
	write("src/b/x.go", "x2")
	git("commit", "-qam", "Change x")
	write("top.md", "t2")
	git("commit", "-qam", "Change top")
	git("clone", "-q", "--bare", work, filepath.Join(root, "r.git"))

	store := NewStore(root)
	defer store.Close()
	repo, _ := store.Open("r")
	ctx := context.Background()

	got, err := store.LastCommits(ctx, repo, "main", "src", []string{"a.go", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if got["a.go"].Subject != "Start" || got["b"].Subject != "Change x" {
		t.Errorf("src came out %+v", got)
	}
	top, _ := store.LastCommits(ctx, repo, "main", "", []string{"src", "top.md"})
	if top["src"].Subject != "Change x" || top["top.md"].Subject != "Change top" {
		t.Errorf("the top came out %+v", top)
	}

	paths, err := store.Paths(ctx, repo, "main")
	if err != nil || !slices.Equal(paths, []string{"src/a.go", "src/b/x.go", "top.md"}) {
		t.Errorf("paths came out %v, %v", paths, err)
	}
	if w := store.Weekly(ctx, repo, "main"); sum(w) != 3 {
		t.Errorf("weekly counted %d commits, want 3", sum(w))
	}
}
