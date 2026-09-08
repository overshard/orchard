package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// chat.bythewood.me could list these repositories and read nothing inside them,
// so every question about Isaac's own code ended with the model guessing raw
// addresses and collecting 404s. These two endpoints are what it reads instead,
// so the shape they answer in is a contract.
func TestAPITreeAndFile(t *testing.T) {
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
	if err := os.MkdirAll(filepath.Join(work, "tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "tools", "web.go"),
		[]byte("package tools\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(work, "init", "-q", "-b", "main")
	git(work, "add", "-A")
	git(work, "commit", "-qm", "first commit")
	git(root, "clone", "-q", "--bare", work, filepath.Join(root, "demo.git"))

	s := &site{store: NewStore(root)}
	defer s.store.Close()

	call := func(h http.HandlerFunc, pattern, target string) (int, map[string]any) {
		t.Helper()
		mux := http.NewServeMux()
		mux.HandleFunc(pattern, h)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}

	code, out := call(s.apiTree, "GET /api/repos/{name}/tree/{rev}", "/api/repos/demo/tree/HEAD")
	if code != http.StatusOK {
		t.Fatalf("listing the top of the repository answered %d: %v", code, out)
	}
	entries, _ := out["entries"].([]any)
	if len(entries) == 0 {
		t.Fatalf("the top level listed nothing: %v", out)
	}

	code, out = call(s.apiTree, "GET /api/repos/{name}/tree/{rev}/{path...}", "/api/repos/demo/tree/HEAD/tools")
	if code != http.StatusOK {
		t.Fatalf("listing a directory answered %d: %v", code, out)
	}
	if entries, _ := out["entries"].([]any); len(entries) != 1 {
		t.Errorf("the tools directory listed %v", out["entries"])
	}

	code, out = call(s.apiFile, "GET /api/repos/{name}/file/{rev}/{path...}", "/api/repos/demo/file/HEAD/tools/web.go")
	if code != http.StatusOK {
		t.Fatalf("reading a file answered %d: %v", code, out)
	}
	if out["text"] != "package tools\n" {
		t.Errorf("the file came back as %q", out["text"])
	}

	// A missing path is a 404, which is what orchard_code falls through on when
	// it tries a file first and the path turns out to be a directory.
	code, _ = call(s.apiFile, "GET /api/repos/{name}/file/{rev}/{path...}", "/api/repos/demo/file/HEAD/tools")
	if code != http.StatusNotFound {
		t.Errorf("a directory read as a file answered %d, want 404", code)
	}
	code, _ = call(s.apiTree, "GET /api/repos/{name}/tree/{rev}", "/api/repos/nope/tree/HEAD")
	if code != http.StatusNotFound {
		t.Errorf("an unknown repository answered %d, want 404", code)
	}
}
