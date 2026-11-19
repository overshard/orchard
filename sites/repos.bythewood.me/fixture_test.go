package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// repoFixture is a bare orchard repository with a few files a few levels down.
func repoFixture(t *testing.T) *site {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := t.TempDir()
	work := filepath.Join(root, "work")
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=t@example.com",
			"GIT_CONFIG_NOSYSTEM=1", "HOME="+root)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	dash := filepath.Join(work, "sites", "dash.bythewood.me")
	pond := filepath.Join(work, "sites", "pond.bythewood.me")
	for _, d := range []string{dash, pond} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dash, "earnings.go"),
		"package main\n\nconst earningsShown = 3\n\nfunc walkEarnings() {\n\t// EarningsShown again\n}\n")
	write(filepath.Join(pond, "game.go"), "package main\n\nfunc hatch() {}\n")
	write(filepath.Join(work, "README.md"), "orchard\n")

	git(work, "init", "-q", "-b", "main")
	git(work, "add", "-A")
	git(work, "commit", "-qm", "first commit")
	git(root, "clone", "-q", "--bare", work, filepath.Join(root, "orchard.git"))

	s := &site{store: NewStore(root)}
	t.Cleanup(func() { s.store.Close() })
	return s
}
