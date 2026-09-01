package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestValidName is the traversal fence, so it takes the adversarial cases.
func TestValidName(t *testing.T) {
	ok := []string{
		"orchard", "taproot", "blog.bythewood.me", "a", "with-dash",
		"with_underscore", "Mixed123",
	}
	for _, name := range ok {
		if !validName(name) {
			t.Errorf("validName(%q) = false, want true", name)
		}
	}

	bad := []string{
		"", ".", "..", ".hidden", "../etc", "a/b", `a\b`,
		"a..b",   // interior .. is still a traversal component once joined
		"a\x00b", // NUL, which would truncate the path in a syscall
		"has space", "q?x", "semi;colon", "-flag",
		strings.Repeat("x", 101),
	}
	for _, name := range bad {
		if validName(name) {
			t.Errorf("validName(%q) = true, want false", name)
		}
	}
}

func TestRepoNameFromPath(t *testing.T) {
	cases := []struct {
		seg  string
		name string
		ok   bool
	}{
		{"orchard.git", "orchard", true},
		{"blog.bythewood.me.git", "blog.bythewood.me", true},
		{"orchard", "", false},  // no suffix: a browse path, not a wire path
		{".git", "", false},     // empty name
		{"../x.git", "", false}, // traversal survives the suffix strip
	}
	for _, c := range cases {
		name, ok := repoNameFromPath(c.seg)
		if ok != c.ok || name != c.name {
			t.Errorf("repoNameFromPath(%q) = (%q, %v), want (%q, %v)",
				c.seg, name, ok, c.name, c.ok)
		}
	}
}

// TestParseDiff pins the diff parser against every shape the renderer draws.
func TestParseDiff(t *testing.T) {
	patch := `diff --git a/kept.txt b/kept.txt
index 1111111..2222222 100644
--- a/kept.txt
+++ b/kept.txt
@@ -1,3 +1,4 @@
 context one
-removed line
+added line
+second added
 context two
diff --git a/added.txt b/added.txt
new file mode 100644
index 0000000..3333333
--- /dev/null
+++ b/added.txt
@@ -0,0 +1 @@
+brand new
diff --git a/gone.txt b/gone.txt
deleted file mode 100644
index 4444444..0000000
--- a/gone.txt
+++ /dev/null
@@ -1 +0,0 @@
-was here
diff --git a/old/name.txt b/new/name.txt
similarity index 100%
rename from old/name.txt
rename to new/name.txt
diff --git a/image.png b/image.png
index 5555555..6666666 100644
Binary files a/image.png and b/image.png differ
`

	d := parseDiff([]byte(patch))

	if len(d.Files) != 5 {
		t.Fatalf("parsed %d files, want 5", len(d.Files))
	}
	if d.Additions != 3 || d.Deletions != 2 {
		t.Errorf("totals = +%d -%d, want +3 -2", d.Additions, d.Deletions)
	}

	modified := d.Files[0]
	if modified.Status != Modified || modified.Path() != "kept.txt" {
		t.Errorf("file 0 = %s %s, want modified kept.txt", modified.Status, modified.Path())
	}
	if len(modified.Hunks) != 1 {
		t.Fatalf("file 0 has %d hunks, want 1", len(modified.Hunks))
	}

	// The line numbers are why the patch is parsed rather than printed.
	h := modified.Hunks[0]
	want := []struct {
		kind   string
		oldNum int
		newNum int
	}{
		{"context", 1, 1},
		{"del", 2, 0},
		{"add", 0, 2},
		{"add", 0, 3},
		{"context", 3, 4},
	}
	if len(h.Lines) != len(want) {
		t.Fatalf("hunk has %d lines, want %d", len(h.Lines), len(want))
	}
	for i, w := range want {
		got := h.Lines[i]
		if got.Kind != w.kind || got.OldNum != w.oldNum || got.NewNum != w.newNum {
			t.Errorf("line %d = %s(%d,%d), want %s(%d,%d)",
				i, got.Kind, got.OldNum, got.NewNum, w.kind, w.oldNum, w.newNum)
		}
	}

	if d.Files[1].Status != Added {
		t.Errorf("file 1 status = %s, want added", d.Files[1].Status)
	}
	// A delete has no new path, so Path falls back to the old one.
	if d.Files[2].Status != Deleted || d.Files[2].Path() != "gone.txt" {
		t.Errorf("file 2 = %s %s, want deleted gone.txt", d.Files[2].Status, d.Files[2].Path())
	}
	if r := d.Files[3]; r.Status != Renamed || r.OldPath != "old/name.txt" || r.NewPath != "new/name.txt" {
		t.Errorf("file 3 = %s %s -> %s, want renamed old/name.txt -> new/name.txt",
			r.Status, r.OldPath, r.NewPath)
	}
	if !d.Files[4].Binary {
		t.Error("file 4 should be binary")
	}
}

func TestParseHunkHeader(t *testing.T) {
	cases := []struct {
		line                                   string
		oldStart, oldLines, newStart, newLines int
	}{
		{"@@ -1,3 +1,4 @@", 1, 3, 1, 4},
		{"@@ -0,0 +1 @@", 0, 0, 1, 1}, // a count-less range means one line
		{"@@ -12 +12 @@ func main() {", 12, 1, 12, 1},
	}
	for _, c := range cases {
		h := parseHunkHeader(c.line)
		if h.OldStart != c.oldStart || h.OldLines != c.oldLines ||
			h.NewStart != c.newStart || h.NewLines != c.newLines {
			t.Errorf("parseHunkHeader(%q) = -%d,%d +%d,%d, want -%d,%d +%d,%d",
				c.line, h.OldStart, h.OldLines, h.NewStart, h.NewLines,
				c.oldStart, c.oldLines, c.newStart, c.newLines)
		}
	}
}

// core.quotePath=false is what keeps this header split parseable, including a
// path holding the separator it splits on.
func TestParseDiffGit(t *testing.T) {
	cases := []struct{ line, old, new string }{
		{"diff --git a/x.txt b/x.txt", "x.txt", "x.txt"},
		{"diff --git a/dir/a b/file b/dir/a b/file", "dir/a b/file", "dir/a b/file"},
		{"diff --git a/héllo.txt b/héllo.txt", "héllo.txt", "héllo.txt"},
	}
	for _, c := range cases {
		old, new := parseDiffGit(c.line)
		if old != c.old || new != c.new {
			t.Errorf("parseDiffGit(%q) = (%q, %q), want (%q, %q)",
				c.line, old, new, c.old, c.new)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0 B"}, {512, "512 B"}, {1024, "1.0 KB"},
		{1536, "1.5 KB"}, {1 << 20, "1.0 MB"}, {100 << 20, "100.0 MB"},
	}
	for _, c := range cases {
		if got := humanBytes(c.n); got != c.want {
			t.Errorf("humanBytes(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

// TestPercentOf covers the meter that has to warn before a push hits
// Cloudflare's limit rather than after.
func TestPercentOf(t *testing.T) {
	cases := []struct {
		n    int64
		want int
	}{
		{0, 0},
		{50 << 20, 50},
		{cloudflareBodyLimit, 100},
		// Over the limit clamps rather than overflowing the bar.
		{200 << 20, 100},
	}
	for _, c := range cases {
		if got := percentOf(c.n, cloudflareBodyLimit); got != c.want {
			t.Errorf("percentOf(%d) = %d, want %d", c.n, got, c.want)
		}
	}
}

func TestURLPath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"a/b/c.txt", "a/b/c.txt"},
		{"with space.txt", "with%20space.txt"},
		{"has#hash.txt", "has%23hash.txt"},
		{"q?uery.txt", "q%3Fuery.txt"},
		{"dir/sub dir/f.txt", "dir/sub%20dir/f.txt"},
	}
	for _, c := range cases {
		if got := urlPath(c.in); got != c.want {
			t.Errorf("urlPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsDumbPath(t *testing.T) {
	// Serving these as static files hands out loose objects and skips the auth.
	for _, p := range []string{"objects/ab/cdef", "HEAD", "packed-refs", "refs/heads/main"} {
		if !isDumbPath(p) {
			t.Errorf("isDumbPath(%q) = false, want true", p)
		}
	}
	for _, p := range []string{"info/refs", "git-upload-pack", "git-receive-pack"} {
		if isDumbPath(p) {
			t.Errorf("isDumbPath(%q) = true, want false", p)
		}
	}
}

func TestIsBinary(t *testing.T) {
	if IsBinary([]byte("plain text\nwith newlines\n")) {
		t.Error("text detected as binary")
	}
	if !IsBinary([]byte("has\x00nul")) {
		t.Error("NUL-containing data not detected as binary")
	}
}

// A README is arbitrary text out of a mirrored repository nobody reviewed.
func TestMarkdownSanitised(t *testing.T) {
	src := []byte("# Title\n\n<script>alert(1)</script>\n\n" +
		"<img src=x onerror=alert(1)>\n\n[link](https://example.com)\n")

	html, err := RenderMarkdown(src)
	if err != nil {
		t.Fatalf("RenderMarkdown: %v", err)
	}
	got := string(html)

	if strings.Contains(got, "<script") {
		t.Error("script tag survived sanitisation")
	}
	if strings.Contains(got, "onerror") {
		t.Error("event handler attribute survived sanitisation")
	}
	if !strings.Contains(got, "<h1") {
		t.Error("heading did not render")
	}
	if !strings.Contains(got, "example.com") {
		t.Error("link did not render")
	}
}

// TestGitRoundTrip runs the parsers against real git output, not a fixture.
func TestGitRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	root := t.TempDir()
	work := filepath.Join(root, "work")
	bare := filepath.Join(root, "sample.git")

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

	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	git(work, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(work, "README.md"),
		[]byte("# sample\n\nhello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(work, "add", "-A")
	git(work, "commit", "-qm", "first commit")
	if err := os.WriteFile(filepath.Join(work, "second.txt"),
		[]byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(work, "add", "-A")
	git(work, "commit", "-qm", "second commit")
	git(work, "tag", "-a", "v1.0.0", "-m", "release one")
	git(root, "clone", "-q", "--bare", work, bare)

	store := NewStore(root)
	defer store.Close()
	ctx := context.Background()

	repos, err := store.Discover()
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(repos) != 1 || repos[0].Name != "sample" {
		t.Fatalf("Discover = %+v, want one repo named sample", repos)
	}
	repo := repos[0]

	if store.IsEmpty(ctx, repo) {
		t.Error("repository with two commits reported empty")
	}

	commits, err := store.Log(ctx, repo, "main", 0, 10)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("Log returned %d commits, want 2", len(commits))
	}
	if commits[0].Subject != "second commit" {
		t.Errorf("newest subject = %q, want %q", commits[0].Subject, "second commit")
	}
	if len(commits[0].SHA) != 40 {
		t.Errorf("SHA = %q, want 40 characters", commits[0].SHA)
	}
	if commits[0].When.IsZero() {
		t.Error("commit date did not parse")
	}

	branches, err := store.Branches(ctx, repo)
	if err != nil {
		t.Fatalf("Branches: %v", err)
	}
	if len(branches) != 1 || branches[0].Name != "main" {
		t.Errorf("Branches = %+v, want one named main", branches)
	}

	tags, err := store.Tags(ctx, repo)
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	if len(tags) != 1 || tags[0].Name != "v1.0.0" {
		t.Fatalf("Tags = %+v, want one named v1.0.0", tags)
	}
	// An annotated tag is its own object, so Target has to be the commit it
	// points at or every tag link 404s.
	if !tags[0].Annotated {
		t.Error("annotated tag not flagged as annotated")
	}
	if tags[0].Target == "" {
		t.Error("annotated tag did not resolve to a commit")
	}

	entries, err := store.Tree(ctx, repo, "main", "")
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("Tree returned %d entries, want 2", len(entries))
	}

	blob, size, err := store.Blob(ctx, repo, "main", "README.md")
	if err != nil {
		t.Fatalf("Blob: %v", err)
	}
	if !strings.Contains(string(blob), "# sample") {
		t.Errorf("Blob content = %q", blob)
	}
	if size != int64(len(blob)) {
		t.Errorf("Blob size = %d, want %d", size, len(blob))
	}

	// Read twice, since the second read proves the cat-file stream stayed in
	// sync after the first payload.
	for i := range 2 {
		typ, data, err := store.Object(repo, "main:README.md")
		if err != nil {
			t.Fatalf("Object read %d: %v", i, err)
		}
		if typ != "blob" {
			t.Errorf("Object type = %q, want blob", typ)
		}
		if !strings.Contains(string(data), "# sample") {
			t.Errorf("Object content = %q", data)
		}
	}

	sha, err := store.Resolve(ctx, repo, "main")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if _, err := store.Resolve(ctx, repo, "no-such-ref"); err == nil {
		t.Error("Resolve accepted a revision that does not exist")
	}

	diff, err := store.Diff(ctx, repo, sha)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(diff.Files) != 1 || diff.Files[0].Path() != "second.txt" {
		t.Errorf("Diff files = %+v, want one for second.txt", diff.Files)
	}
	if diff.Additions != 1 {
		t.Errorf("Diff additions = %d, want 1", diff.Additions)
	}

	if n := store.CountCommits(ctx, repo, "main"); n != 2 {
		t.Errorf("CountCommits = %d, want 2", n)
	}
	if store.Size(ctx, repo) <= 0 {
		t.Error("Size returned nothing for a repository with content")
	}

	// Overview answers the whole listing card from one ref walk, so it has to
	// agree with the four calls it replaced.
	o := store.Overview(ctx, repo)
	if o.Branches != len(branches) || o.Tags != len(tags) {
		t.Errorf("Overview = %d branches and %d tags, want %d and %d",
			o.Branches, o.Tags, len(branches), len(tags))
	}
	if want := store.Size(ctx, repo); o.Size != want {
		t.Errorf("Overview size = %d, want %d", o.Size, want)
	}
	if o.LastPush.IsZero() {
		t.Error("Overview LastPush is zero")
	}
	if o.Empty {
		t.Error("repository with two commits reported empty by Overview")
	}
}

// TestOverviewCache proves the listing card is cached and that invalidating is
// what makes a push visible, since a stale card is the failure mode the cache
// introduces.
func TestOverviewCache(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	root := t.TempDir()
	bare := filepath.Join(root, "sample.git")

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

	work := filepath.Join(root, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	git(work, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(work, "add", "-A")
	git(work, "commit", "-qm", "first")
	git(root, "clone", "-q", "--bare", work, bare)

	store := NewStore(root)
	defer store.Close()
	ctx := context.Background()

	repo, ok := store.Open("sample")
	if !ok {
		t.Fatal("Open(sample) failed")
	}

	if n := store.Overview(ctx, repo).Branches; n != 1 {
		t.Fatalf("Overview branches = %d, want 1", n)
	}

	// A second branch appears on disk with nothing told to the store.
	git(bare, "branch", "topic", "main")

	if n := store.Overview(ctx, repo).Branches; n != 1 {
		t.Errorf("Overview branches = %d after an uninvalidated write, want the cached 1", n)
	}

	store.InvalidateOverview("sample")

	if n := store.Overview(ctx, repo).Branches; n != 2 {
		t.Errorf("Overview branches = %d after invalidating, want 2", n)
	}
}

// TestInitBareConfig locks in the settings that keep a pushed repository
// recoverable and its pushes fast.
func TestInitBareConfig(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	root := t.TempDir()
	store := NewStore(root)
	defer store.Close()
	ctx := context.Background()

	repo, err := store.InitBare(ctx, "created")
	if err != nil {
		t.Fatalf("InitBare: %v", err)
	}

	get := func(key string) string {
		out, err := run(ctx, repo, "config", "--get", key)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}

	// A gc inside a push is work done while Cloudflare counts to 100.
	if got := get("receive.autogc"); got != "false" {
		t.Errorf("receive.autogc = %q, want false", got)
	}
	// Off by default in a bare repository. With it, a force push leaves the
	// old tip recoverable, so today's mistake is not the only copy left.
	if got := get("core.logAllRefUpdates"); got != "true" {
		t.Errorf("core.logAllRefUpdates = %q, want true", got)
	}
	// Explicitly NOT set. A false here denies the authenticated smart push
	// too, so push-to-create would create a repository and then reject the
	// push that made it.
	if got := get("http.receivepack"); got != "" {
		t.Errorf("http.receivepack = %q, want unset", got)
	}

	// Idempotent: a second push to the same name must not fail.
	if _, err := store.InitBare(ctx, "created"); err != nil {
		t.Errorf("InitBare is not idempotent: %v", err)
	}

	if _, err := store.InitBare(ctx, "../escape"); err == nil {
		t.Error("InitBare accepted a traversing name")
	}
}

func TestTokens(t *testing.T) {
	db, err := OpenDB(t.TempDir())
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	if db.HasTokens() {
		t.Error("fresh database reports tokens")
	}

	token, err := db.CreateToken("laptop")
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	if len(token) < 40 {
		t.Errorf("token is %d characters, want at least 40", len(token))
	}
	if !db.HasTokens() {
		t.Error("HasTokens false after minting one")
	}

	label, err := db.VerifyToken(token)
	if err != nil {
		t.Fatalf("VerifyToken: %v", err)
	}
	if label != "laptop" {
		t.Errorf("label = %q, want laptop", label)
	}

	if _, err := db.VerifyToken(token + "x"); err == nil {
		t.Error("VerifyToken accepted a token with an extra character")
	}
	if _, err := db.VerifyToken("short"); err == nil {
		t.Error("VerifyToken accepted a too-short value")
	}
	// A value sharing the stored prefix must still fail, the prefix is kept in
	// clear and is not a secret.
	if _, err := db.VerifyToken(token[:8] + strings.Repeat("A", 35)); err == nil {
		t.Error("VerifyToken accepted a value matching only the prefix")
	}

	tokens, err := db.Tokens()
	if err != nil {
		t.Fatalf("Tokens: %v", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("Tokens returned %d, want 1", len(tokens))
	}
	// The token itself must never be readable back out.
	if strings.Contains(tokens[0].Prefix, token[8:]) {
		t.Error("stored prefix leaks the token body")
	}

	if err := db.RevokeToken(tokens[0].ID); err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}
	if _, err := db.VerifyToken(token); err == nil {
		t.Error("revoked token still authenticates")
	}
}

// TestRepoMeta covers the metadata git has nowhere to put.
func TestRepoMeta(t *testing.T) {
	db, err := OpenDB(t.TempDir())
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	if err := db.EnsureRepo("orchard"); err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}
	// Called on every push, so it must not fail the second time.
	if err := db.EnsureRepo("orchard"); err != nil {
		t.Fatalf("EnsureRepo is not idempotent: %v", err)
	}

	if err := db.SetDescription("orchard", "the monorepo",
		[]string{"go", "self-hosted"}, "https://example.com"); err != nil {
		t.Fatalf("SetDescription: %v", err)
	}

	meta, err := db.Repo("orchard")
	if err != nil {
		t.Fatalf("Repo: %v", err)
	}
	if meta.Description != "the monorepo" {
		t.Errorf("description = %q", meta.Description)
	}
	if len(meta.Topics) != 2 || meta.Topics[0] != "go" {
		t.Errorf("topics = %v, want [go self-hosted]", meta.Topics)
	}
	if meta.Mirror {
		t.Error("a pushed repository should not be flagged as a mirror")
	}

	// A mirror refuses pushes, so the flag is what the wire checks.
	if err := db.MarkMirror("pinry", "https://github.com/overshard/pinry.git", true); err != nil {
		t.Fatalf("MarkMirror: %v", err)
	}
	mirror, err := db.Repo("pinry")
	if err != nil {
		t.Fatalf("Repo: %v", err)
	}
	if !mirror.Mirror || !mirror.Archived {
		t.Errorf("mirror flags = mirror:%v archived:%v, want both true",
			mirror.Mirror, mirror.Archived)
	}

	if err := db.MarkUpstreamGone("pinry", true); err != nil {
		t.Fatalf("MarkUpstreamGone: %v", err)
	}
	if m, _ := db.Repo("pinry"); !m.UpstreamGone {
		t.Error("upstream_gone did not persist")
	}

	all, err := db.AllRepos()
	if err != nil {
		t.Fatalf("AllRepos: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("AllRepos returned %d, want 2", len(all))
	}
}

// The UI's own authentication, separate from the Basic auth on the git wire.
func TestSessionCookie(t *testing.T) {
	const password = "correct horse"

	rec := &fakeWriter{header: make(map[string][]string)}
	issueSession(rec, password)

	cookie := rec.header["Set-Cookie"]
	if len(cookie) != 1 {
		t.Fatalf("issueSession set %d cookies, want 1", len(cookie))
	}
	raw := cookie[0]
	for _, want := range []string{"HttpOnly", "Secure", "SameSite=Strict"} {
		if !strings.Contains(raw, want) {
			t.Errorf("cookie missing %s: %s", want, raw)
		}
	}

	value := strings.SplitN(strings.TrimPrefix(raw, sessionCookie+"="), ";", 2)[0]

	r := newRequestWithCookie(value)
	if !validSession(r, password) {
		t.Error("freshly issued session did not validate")
	}
	// The signing key comes from the password, so a change must invalidate
	// every outstanding session.
	if validSession(r, "different password") {
		t.Error("session validated under a different password")
	}
	// A tampered payload must not validate, the expiry inside it is a number
	// the client would otherwise choose.
	if validSession(newRequestWithCookie("1:9999999999:forged"), password) {
		t.Error("forged signature validated")
	}
	if validSession(newRequestWithCookie("garbage"), password) {
		t.Error("malformed cookie validated")
	}
}

func TestCheckPassword(t *testing.T) {
	if !checkPassword("secret", "secret") {
		t.Error("matching passwords rejected")
	}
	if checkPassword("secret", "secrez") {
		t.Error("differing passwords accepted")
	}
	if checkPassword("", "secret") {
		t.Error("empty password accepted")
	}
}

func TestTokenBucket(t *testing.T) {
	b := &tokenBucket{tokens: 2, last: time.Now()}
	if !b.take() || !b.take() {
		t.Fatal("bucket refused the tokens it was given")
	}
	if b.take() {
		t.Error("bucket handed out a token it did not have")
	}
}

type fakeWriter struct {
	header map[string][]string
	status int
}

func (f *fakeWriter) Header() http.Header         { return http.Header(f.header) }
func (f *fakeWriter) Write(b []byte) (int, error) { return len(b), nil }
func (f *fakeWriter) WriteHeader(code int)        { f.status = code }

func newRequestWithCookie(value string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: value})
	return r
}

// TestAdopt covers converting a pushed repository into a mirror, the one path
// that rewrites a repository this site may hold the only copy of.
func TestAdopt(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	root := t.TempDir()
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
	commit := func(dir, name string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		git(dir, "add", "-A")
		git(dir, "commit", "-qm", name)
	}

	// The stand-in for GitHub: a work tree with two commits, cloned bare.
	work := filepath.Join(root, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	git(work, "init", "-q", "-b", "main")
	commit(work, "one")
	commit(work, "two")
	upstream := filepath.Join(root, "upstream.git")
	git(root, "clone", "-q", "--bare", work, upstream)

	ctx := context.Background()
	db, err := OpenDB(t.TempDir())
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	// setup builds a pushed repository at rev under a fresh store root.
	setup := func(t *testing.T, name, rev string) (*Mirror, Repo) {
		t.Helper()
		storeRoot := t.TempDir()
		bare := filepath.Join(storeRoot, name+".git")
		git(root, "clone", "-q", "--bare", upstream, bare)
		// A pushed repository has no origin, the clone gave it one.
		git(bare, "remote", "remove", "origin")
		if rev != "" {
			git(bare, "update-ref", "refs/heads/main", rev)
		}
		store := NewStore(storeRoot)
		t.Cleanup(store.Close)
		repo, ok := store.Open(name)
		if !ok {
			t.Fatalf("store.Open(%q) = false", name)
		}
		return NewMirror(store, db), repo
	}

	head := func(dir string) string {
		out, err := exec.Command("git", "--git-dir", dir, "rev-parse", "refs/heads/main").Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}
	tip := head(upstream)
	first := func() string {
		out, err := exec.Command("git", "--git-dir", upstream, "rev-parse", "refs/heads/main~1").Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}()

	gh := GitHubRepo{Name: "sample", CloneURL: upstream}

	t.Run("identical is adopted", func(t *testing.T) {
		m, repo := setup(t, "sample", "")
		adopted, err := m.adopt(ctx, gh, repo)
		if err != nil {
			t.Fatalf("adopt: %v", err)
		}
		if !adopted {
			t.Fatal("adopted = false, want true")
		}
		// The mirror refspec is what makes syncOne rewrite refs, not add to them.
		out, err := run(ctx, repo, "config", "--get", "remote.origin.fetch")
		if err != nil || strings.TrimSpace(string(out)) != "+refs/*:refs/*" {
			t.Errorf("remote.origin.fetch = %q (err %v), want +refs/*:refs/*", out, err)
		}
		if out, err := run(ctx, repo, "config", "--get", "gc.reflogExpire"); err != nil ||
			strings.TrimSpace(string(out)) != "never" {
			t.Errorf("gc.reflogExpire = %q, want never", out)
		}
		// The probe namespace must not survive a successful adoption.
		if out, err := run(ctx, repo, "for-each-ref", "refs/adopt"); err != nil ||
			strings.TrimSpace(string(out)) != "" {
			t.Errorf("refs/adopt left behind: %q", out)
		}
	})

	t.Run("behind upstream is adopted", func(t *testing.T) {
		m, repo := setup(t, "sample", first)
		if head(repo.Path) != first {
			t.Fatalf("setup: head = %s, want %s", head(repo.Path), first)
		}
		adopted, err := m.adopt(ctx, gh, repo)
		if err != nil {
			t.Fatalf("adopt: %v", err)
		}
		if !adopted {
			t.Fatal("adopted = false, want true: behind fast-forwards")
		}
	})

	t.Run("ahead of upstream is refused and left untouched", func(t *testing.T) {
		m, repo := setup(t, "sample", "")
		// A commit only in the pushed copy, the case the skip in Sync protects.
		local := filepath.Join(root, "local")
		if err := os.MkdirAll(local, 0o755); err != nil {
			t.Fatal(err)
		}
		git(root, "clone", "-q", repo.Path, local)
		commit(local, "only-here")
		git(local, "push", "-q", "origin", "main")
		onlyHere := head(repo.Path)
		if onlyHere == tip {
			t.Fatal("setup: local push did not move main")
		}

		adopted, err := m.adopt(ctx, gh, repo)
		if err != nil {
			t.Fatalf("adopt: %v", err)
		}
		if adopted {
			t.Fatal("adopted = true, want false: local holds a commit upstream lacks")
		}
		// Refusal has to leave the repository untouched.
		if got := head(repo.Path); got != onlyHere {
			t.Errorf("refs moved on refusal: head = %s, want %s", got, onlyHere)
		}
		if out, _ := run(ctx, repo, "config", "--get", "remote.origin.url"); strings.TrimSpace(string(out)) != "" {
			t.Errorf("origin left configured after refusal: %q", out)
		}
		if out, err := run(ctx, repo, "for-each-ref", "refs/adopt"); err != nil ||
			strings.TrimSpace(string(out)) != "" {
			t.Errorf("refs/adopt left behind after refusal: %q", out)
		}
	})
}

// The slash is the entire grammar of the one field the settings form has, so
// the cases either side of it are what matter.
func TestParseMirrorSource(t *testing.T) {
	ok := []struct {
		in    string
		kind  string
		owner string
		name  string
	}{
		{"overshard", sourceAccount, "overshard", ""},
		{"  overshard  ", sourceAccount, "overshard", ""},
		{"overshard/newtab", sourceRepo, "overshard", "newtab"},
		{"overshard/blog.bythewood.me", sourceRepo, "overshard", "blog.bythewood.me"},
		// Pasting the URL is the obvious mistake, so it is accepted.
		{"https://github.com/overshard", sourceAccount, "overshard", ""},
		{"https://github.com/overshard/newtab", sourceRepo, "overshard", "newtab"},
		{"https://github.com/overshard/newtab.git", sourceRepo, "overshard", "newtab"},
		{"github.com/overshard/newtab/", sourceRepo, "overshard", "newtab"},
		// A trailing slash is what a copied URL carries, so it is forgiven.
		{"overshard/", sourceAccount, "overshard", ""},
	}
	for _, c := range ok {
		got, err := ParseMirrorSource(c.in)
		if err != nil {
			t.Errorf("ParseMirrorSource(%q) errored: %v", c.in, err)
			continue
		}
		if got.Kind != c.kind || got.Owner != c.owner || got.Name != c.name {
			t.Errorf("ParseMirrorSource(%q) = %+v, want %s %s/%s",
				c.in, got, c.kind, c.owner, c.name)
		}
	}

	bad := []string{
		"", "   ", "/", "/newtab", "-bad", "over shard",
		"a/b/c", "over;shard", "overshard/../etc", strings.Repeat("x", 101),
	}
	for _, in := range bad {
		if got, err := ParseMirrorSource(in); err == nil {
			t.Errorf("ParseMirrorSource(%q) = %+v, want an error", in, got)
		}
	}
}

// TestMirrorSourceLabel checks that what somebody types is what comes back.
func TestMirrorSourceLabel(t *testing.T) {
	for _, in := range []string{"overshard", "overshard/newtab"} {
		src, err := ParseMirrorSource(in)
		if err != nil {
			t.Fatalf("ParseMirrorSource(%q): %v", in, err)
		}
		if src.Label() != in {
			t.Errorf("Label() = %q, want %q", src.Label(), in)
		}
		if want := "https://github.com/" + in; src.URL() != want {
			t.Errorf("URL() = %q, want %q", src.URL(), want)
		}
	}
}

// TestMirrorSources covers the CRUD plus the seed guard, since a setting that
// comes back after being deleted is worse than one that was never editable.
func TestMirrorSources(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(dir)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	if err := db.SeedMirrorSources("overshard"); err != nil {
		t.Fatalf("SeedMirrorSources: %v", err)
	}
	got, err := db.MirrorSources()
	if err != nil {
		t.Fatalf("MirrorSources: %v", err)
	}
	if len(got) != 1 || got[0].Owner != "overshard" || got[0].Kind != sourceAccount {
		t.Fatalf("after seed: %+v, want one account source for overshard", got)
	}

	// Re-seeding is what a restart does, and it must not duplicate.
	if err := db.SeedMirrorSources("overshard"); err != nil {
		t.Fatalf("re-seed: %v", err)
	}
	if got, _ = db.MirrorSources(); len(got) != 1 {
		t.Fatalf("re-seed added a row: %+v", got)
	}

	// Adding the same source twice is a no-op, so a double submit is harmless.
	src := MirrorSource{Kind: sourceRepo, Owner: "other", Name: "thing"}
	for range 2 {
		if err := db.AddMirrorSource(src); err != nil {
			t.Fatalf("AddMirrorSource: %v", err)
		}
	}
	if got, _ = db.MirrorSources(); len(got) != 2 {
		t.Fatalf("after add: %d sources, want 2: %+v", len(got), got)
	}

	// Deleting the last source must stay deleted across a restart.
	for _, s := range got {
		if err := db.DeleteMirrorSource(s.ID); err != nil {
			t.Fatalf("DeleteMirrorSource: %v", err)
		}
	}
	if err := db.SeedMirrorSources("overshard"); err != nil {
		t.Fatalf("seed after delete: %v", err)
	}
	if got, _ = db.MirrorSources(); len(got) != 0 {
		t.Errorf("seed resurrected a deleted source: %+v", got)
	}
}

// TestCoveredBySource scopes the upstream_gone flag. Getting it wrong means
// removing a source claims everything it brought in vanished from GitHub.
func TestCoveredBySource(t *testing.T) {
	sources := []MirrorSource{
		{Kind: sourceAccount, Owner: "overshard"},
		{Kind: sourceRepo, Owner: "other", Name: "thing"},
	}
	covered := []string{
		"https://github.com/overshard/orchard.git",
		"https://github.com/overshard/newtab.git",
		"https://github.com/OverShard/orchard.git", // GitHub logins are case insensitive
		"https://github.com/other/thing.git",
	}
	for _, u := range covered {
		if !coveredBySource(u, sources) {
			t.Errorf("coveredBySource(%q) = false, want true", u)
		}
	}
	notCovered := []string{
		"https://github.com/other/different.git",
		"https://github.com/somebodyelse/orchard.git",
		"", "not-a-url", "https://github.com/overshard",
	}
	for _, u := range notCovered {
		if coveredBySource(u, sources) {
			t.Errorf("coveredBySource(%q) = true, want false", u)
		}
	}
}

// TestAnalyticsWiring guards the four places the collector lives, since they
// are edited separately and any one alone fails silently.
func TestAnalyticsWiring(t *testing.T) {
	// The id is identity and not a credential, and a typo files this site's
	// traffic under nothing.
	const want = "49f89ef6-b0b2-4b47-879e-7e252a067d0c"
	if analyticsID != want {
		t.Errorf("analyticsID = %q, want %q", analyticsID, want)
	}

	base, err := templateFS.ReadFile("templates/base.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, need := range []string{"collectorId", ".AnalyticsID",
		"https://analytics.bythewood.me"} {
		if !strings.Contains(string(base), need) {
			t.Errorf("base.html is missing %q, so nothing is collected", need)
		}
	}

	policy := csp()
	for _, need := range []string{"'unsafe-inline'", "https://analytics.bythewood.me"} {
		if !strings.Contains(policy, need) {
			t.Errorf("CSP is missing %s, so the collector would be blocked: %s",
				need, policy)
		}
	}

	p := (&site{}).page(httptest.NewRequest(http.MethodGet, "/", nil), "t", "d", nil)
	if !p.Analytics {
		t.Error("page.Analytics is false on the real hostname, so the snippet never renders")
	}
	if p.AnalyticsID != analyticsID {
		t.Errorf("page.AnalyticsID = %q, want %q", p.AnalyticsID, analyticsID)
	}
}
