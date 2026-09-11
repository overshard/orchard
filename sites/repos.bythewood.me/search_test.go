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

// A repository with the shape that broke the walk: the file worth finding is
// three levels down and its directory name is the one the model dropped.
func searchFixture(t *testing.T) *site {
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
	chat := filepath.Join(work, "sites", "chat.bythewood.me")
	for _, d := range []string{dash, chat} {
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
	write(filepath.Join(chat, "ground.go"), "package main\n\nfunc subjectOf() {}\n")
	write(filepath.Join(work, "README.md"), "orchard\n")

	git(work, "init", "-q", "-b", "main")
	git(work, "add", "-A")
	git(work, "commit", "-qm", "first commit")
	git(root, "clone", "-q", "--bare", work, filepath.Join(root, "orchard.git"))

	s := &site{store: NewStore(root)}
	t.Cleanup(func() { s.store.Close() })
	return s
}

func callJSON(t *testing.T, h http.HandlerFunc, pattern, target string) (int, map[string]any) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(pattern, h)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

// The one call that replaces the walk. "earnings.go" has to come back as its
// full path without anything having listed a directory.
func TestFindLocatesAFileByName(t *testing.T) {
	s := searchFixture(t)
	code, out := callJSON(t, s.apiFind, "GET /api/repos/{name}/find/{rev}",
		"/api/repos/orchard/find/HEAD?q=earnings.go")
	if code != http.StatusOK {
		t.Fatalf("find answered %d: %v", code, out)
	}
	paths, _ := out["paths"].([]any)
	if len(paths) != 1 || paths[0] != "sites/dash.bythewood.me/earnings.go" {
		t.Fatalf("find returned %v", out["paths"])
	}

	// No query is the whole tree, which is how the shape of a repository is
	// seen in one call rather than one directory at a time.
	_, all := callJSON(t, s.apiFind, "GET /api/repos/{name}/find/{rev}",
		"/api/repos/orchard/find/HEAD")
	if n, _ := all["count"].(float64); n != 3 {
		t.Errorf("the whole tree listed %v paths, want 3", all["count"])
	}
}

// Case insensitive and fixed string, because the caller is a language model and
// a regex it wrote by accident finds nothing and says nothing about why.
func TestGrepFindsASymbol(t *testing.T) {
	s := searchFixture(t)
	code, out := callJSON(t, s.apiGrep, "GET /api/repos/{name}/grep/{rev}",
		"/api/repos/orchard/grep/HEAD?q=earningsShown")
	if code != http.StatusOK {
		t.Fatalf("grep answered %d: %v", code, out)
	}
	hits, _ := out["hits"].([]any)
	if len(hits) != 2 {
		t.Fatalf("grep found %d hits, want the declaration and the comment: %v", len(hits), out)
	}
	first, _ := hits[0].(map[string]any)
	if first["path"] != "sites/dash.bythewood.me/earnings.go" {
		t.Errorf("first hit is %v", first["path"])
	}
	if first["line"] != float64(3) {
		t.Errorf("first hit is on line %v, want 3", first["line"])
	}
}

// A question about one site must not read the other ten, which is the whole
// reason the pathspec is passed to git rather than filtered afterwards.
func TestGrepStaysInsideThePathspec(t *testing.T) {
	s := searchFixture(t)
	_, out := callJSON(t, s.apiGrep, "GET /api/repos/{name}/grep/{rev}",
		"/api/repos/orchard/grep/HEAD?q=package&path=sites/chat.bythewood.me")
	hits, _ := out["hits"].([]any)
	if len(hits) != 1 {
		t.Fatalf("the pathspec let %d hits through: %v", len(hits), out)
	}
	if m, _ := hits[0].(map[string]any); m["path"] != "sites/chat.bythewood.me/ground.go" {
		t.Errorf("hit is %v", hits[0])
	}
}

// Nothing found is an answer. git grep exits 1 on no match, which would
// otherwise be reported as the search having failed.
func TestGrepFindsNothingWithoutFailing(t *testing.T) {
	s := searchFixture(t)
	code, out := callJSON(t, s.apiGrep, "GET /api/repos/{name}/grep/{rev}",
		"/api/repos/orchard/grep/HEAD?q=thisisnotinanyfile")
	if code != http.StatusOK {
		t.Fatalf("an empty search answered %d: %v", code, out)
	}
	if n, _ := out["count"].(float64); n != 0 {
		t.Errorf("count = %v, want 0", out["count"])
	}
	if _, ok := out["hits"].([]any); !ok {
		t.Errorf("hits came back as %#v, want an empty array", out["hits"])
	}
}

// A long file read whole is context the window pays for until the conversation
// ends, so a reader that wants one function asks for its lines.
func TestFileTakesALineRange(t *testing.T) {
	s := searchFixture(t)
	code, out := callJSON(t, s.apiFile, "GET /api/repos/{name}/file/{rev}/{path...}",
		"/api/repos/orchard/file/HEAD/sites/dash.bythewood.me/earnings.go?from=3&to=3")
	if code != http.StatusOK {
		t.Fatalf("the range read answered %d: %v", code, out)
	}
	if out["text"] != "const earningsShown = 3" {
		t.Errorf("text = %q", out["text"])
	}
	if out["partial"] != true || out["from"] != float64(3) {
		t.Errorf("a slice did not say it was one: %v", out)
	}

	// The whole file is the shape it always was, with no range fields on it.
	_, whole := callJSON(t, s.apiFile, "GET /api/repos/{name}/file/{rev}/{path...}",
		"/api/repos/orchard/file/HEAD/sites/dash.bythewood.me/earnings.go")
	if whole["partial"] != nil {
		t.Errorf("a whole file called itself partial: %v", whole)
	}
	if !strings.Contains(whole["text"].(string), "walkEarnings") {
		t.Error("the whole file is missing its last lines")
	}
}

// A range past the end of the file is a clamp, not an error, since the caller
// is guessing at where a function ends.
func TestLineRangeClampsRatherThanFailing(t *testing.T) {
	s := searchFixture(t)
	code, out := callJSON(t, s.apiFile, "GET /api/repos/{name}/file/{rev}/{path...}",
		"/api/repos/orchard/file/HEAD/README.md?from=900&to=1000")
	if code != http.StatusOK {
		t.Fatalf("an out of range read answered %d: %v", code, out)
	}
	if strings.TrimSpace(out["text"].(string)) != "orchard" {
		t.Errorf("text = %q", out["text"])
	}
}
