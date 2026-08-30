package main

// Every repository read here is a git subprocess. The rules that keep that safe:
// plumbing commands only, NUL delimiters (-z and %x00) because ref names and
// commit messages contain newlines, and "--" before every user supplied ref or path.

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// gitTimeout stays under Cloudflare's 100 second ceiling, so a wedged subprocess
// surfaces as a 500 here rather than a 524 from the edge.
const gitTimeout = 20 * time.Second

// maxBlobSize is the largest file read into memory to render. Anything larger is
// offered as a raw download, which streams.
const maxBlobSize = 25 << 20

// Repo is one bare repository on disk. Name is the URL segment and the display
// name; Path is the --git-dir.
type Repo struct {
	Name string
	Path string
}

// validName gates every name arriving from a URL before it is joined to a path,
// and is the traversal fence. Interior dots are allowed because
// `blog.bythewood.me` is a repository name here; a leading dot is not.
func validName(name string) bool {
	if name == "" || len(name) > 100 {
		return false
	}
	if strings.HasPrefix(name, ".") {
		return false
	}
	// A leading dash reads as an option to any git command that sees it unguarded.
	if strings.HasPrefix(name, "-") {
		return false
	}
	if strings.Contains(name, "..") {
		return false
	}
	if strings.ContainsAny(name, "/\\\x00") {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '.' || r == '-' || r == '_':
		default:
			return false
		}
	}
	return true
}

// Store owns the repository root and the cat-file readers keyed by repository.
type Store struct {
	Root string

	mu      sync.Mutex
	batches map[string]*catFile
}

func NewStore(root string) *Store {
	return &Store{Root: root, batches: make(map[string]*catFile)}
}

// Open resolves a name to a repository. Invalid or missing is (Repo{}, false),
// never a git error that leaks a path.
func (s *Store) Open(name string) (Repo, bool) {
	if !validName(name) {
		return Repo{}, false
	}
	path := filepath.Join(s.Root, name+".git")
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return Repo{}, false
	}
	return Repo{Name: name, Path: path}, true
}

// Discover lists every bare repository under the root; anything not ending in
// .git is ignored, so the root may hold other things without publishing them.
func (s *Store) Discover() ([]Repo, error) {
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		return nil, fmt.Errorf("read repo root: %w", err)
	}

	var repos []Repo
	for _, e := range entries {
		if !e.IsDir() || !strings.HasSuffix(e.Name(), ".git") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".git")
		if !validName(name) {
			continue
		}
		repos = append(repos, Repo{Name: name, Path: filepath.Join(s.Root, e.Name())})
	}
	sort.Slice(repos, func(i, j int) bool { return repos[i].Name < repos[j].Name })
	return repos, nil
}

// gitCmd builds a git invocation against one repository with the safety flags
// every call in this file needs, so no call site has to remember them.
func gitCmd(ctx context.Context, repo Repo, args ...string) *exec.Cmd {
	full := []string{
		// Without this git escapes non-ASCII path bytes into octal in its own output.
		"-c", "core.quotePath=false",
		// A served repository is data, never a place to run a program from.
		"-c", "core.fsmonitor=false",
		"--git-dir", repo.Path,
	}
	cmd := exec.CommandContext(ctx, "git", append(full, args...)...)
	// git refuses some operations without a HOME, the mirror fetch among them.
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
		"HOME="+os.TempDir(),
	)
	return cmd
}

// run executes a plumbing command and returns stdout; stderr is folded into the error.
func run(ctx context.Context, repo Repo, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()

	cmd := gitCmd(ctx, repo, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s",
			strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// Ref is a branch or a tag with its commit already resolved. %(*objectname) is
// the commit an annotated tag points at and is empty for a lightweight one, so
// Target picks whichever of the two is real.
type Ref struct {
	Name      string
	FullName  string
	Target    string
	Subject   string
	Author    string
	When      time.Time
	Annotated bool
	Message   string
}

// refFormat is one line per ref, NUL between fields. A trailing %00 plus -z would
// double-terminate, so for-each-ref keeps the record separator.
const refFormat = "%(refname:short)%00%(refname)%00%(objectname)%00%(*objectname)%00" +
	"%(contents:subject)%00%(authorname)%00%(taggername)%00%(creatordate:iso-strict)%00%(contents:body)"

func (s *Store) refs(ctx context.Context, repo Repo, pattern string) ([]Ref, error) {
	out, err := run(ctx, repo, "for-each-ref",
		"--sort=-creatordate", "--format="+refFormat, pattern)
	if err != nil {
		return nil, err
	}

	var refs []Ref
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\x00")
		if len(f) < 9 {
			continue
		}
		author := f[5]
		if author == "" {
			author = f[6]
		}
		// The starred object name is set only for an annotated tag.
		target, annotated := f[2], false
		if f[3] != "" {
			target, annotated = f[3], true
		}
		refs = append(refs, Ref{
			Name:      f[0],
			FullName:  f[1],
			Target:    target,
			Subject:   f[4],
			Author:    author,
			When:      parseISO(f[7]),
			Annotated: annotated,
			Message:   f[8],
		})
	}
	return refs, nil
}

func (s *Store) Branches(ctx context.Context, repo Repo) ([]Ref, error) {
	return s.refs(ctx, repo, "refs/heads/")
}

func (s *Store) Tags(ctx context.Context, repo Repo) ([]Ref, error) {
	return s.refs(ctx, repo, "refs/tags/")
}

// Head returns the default branch's short name. HEAD is a symbolic ref, so this
// answers on an empty repository too.
func (s *Store) Head(ctx context.Context, repo Repo) string {
	out, err := run(ctx, repo, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return "main"
	}
	return strings.TrimSpace(string(out))
}

// IsEmpty reports a repository with no commits; every other read fails on one.
func (s *Store) IsEmpty(ctx context.Context, repo Repo) bool {
	_, err := run(ctx, repo, "rev-parse", "--verify", "--quiet", "HEAD")
	return err != nil
}

// Commit is one entry in a log.
type Commit struct {
	SHA     string
	Short   string
	Author  string
	Email   string
	When    time.Time
	Commit  time.Time
	Parents []string
	Refs    string
	Subject string
	Body    string
}

// logFormat is ten NUL separated fields. With -z git appends a NUL after each
// record too, so splitting the stream on NUL yields groups of ten.
const logFormat = "%H%x00%h%x00%an%x00%ae%x00%aI%x00%cI%x00%P%x00%D%x00%s%x00%b"

const logFields = 10

// Log reads commits reachable from rev; the caller caps limit.
func (s *Store) Log(ctx context.Context, repo Repo, rev string, skip, limit int) ([]Commit, error) {
	args := []string{"log", "-z", "--format=" + logFormat,
		"--skip=" + strconv.Itoa(skip), "-n", strconv.Itoa(limit), rev, "--"}
	out, err := run(ctx, repo, args...)
	if err != nil {
		return nil, err
	}
	return parseLog(out), nil
}

// LogFile is Log narrowed to one path.
func (s *Store) LogFile(ctx context.Context, repo Repo, rev, path string, skip, limit int) ([]Commit, error) {
	args := []string{"log", "-z", "--format=" + logFormat,
		"--skip=" + strconv.Itoa(skip), "-n", strconv.Itoa(limit), rev, "--", path}
	out, err := run(ctx, repo, args...)
	if err != nil {
		return nil, err
	}
	return parseLog(out), nil
}

func parseLog(out []byte) []Commit {
	fields := strings.Split(string(out), "\x00")
	var commits []Commit
	for i := 0; i+logFields <= len(fields); i += logFields {
		f := fields[i : i+logFields]
		// Some git versions leave a leading newline on the next group's first
		// field, so trim rather than trusting the separator.
		sha := strings.TrimLeft(f[0], "\n")
		if len(sha) < 7 {
			break
		}
		var parents []string
		if f[6] != "" {
			parents = strings.Fields(f[6])
		}
		commits = append(commits, Commit{
			SHA:     sha,
			Short:   f[1],
			Author:  f[2],
			Email:   f[3],
			When:    parseISO(f[4]),
			Commit:  parseISO(f[5]),
			Parents: parents,
			Refs:    f[7],
			Subject: f[8],
			Body:    strings.TrimRight(f[9], "\n"),
		})
	}
	return commits
}

// CommitOne reads a single commit, or errors if rev does not name one.
func (s *Store) CommitOne(ctx context.Context, repo Repo, rev string) (Commit, error) {
	out, err := run(ctx, repo, "log", "-z", "--format="+logFormat, "-n", "1", rev, "--")
	if err != nil {
		return Commit{}, err
	}
	commits := parseLog(out)
	if len(commits) == 0 {
		return Commit{}, fmt.Errorf("no such commit: %s", rev)
	}
	return commits[0], nil
}

// CountCommits walks the graph, and is the one number on a repository page that
// costs real work on a large history.
func (s *Store) CountCommits(ctx context.Context, repo Repo, rev string) int {
	out, err := run(ctx, repo, "rev-list", "--count", rev, "--")
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}

// TreeEntry is one row of a directory listing.
type TreeEntry struct {
	Mode string
	Type string
	SHA  string
	Size int64
	Name string
	Path string
}

// IsDir is true for a subdirectory. A gitlink is neither tree nor blob, so this
// is not the same as "not a blob".
func (e TreeEntry) IsDir() bool { return e.Type == "tree" }

// IsSubmodule reports a gitlink, which ls-tree types as "commit" inside a tree.
func (e TreeEntry) IsSubmodule() bool { return e.Type == "commit" }

// IsSymlink reads the mode, since a symlink is a blob whose content is its target.
func (e TreeEntry) IsSymlink() bool { return e.Mode == "120000" }

// Tree lists one directory, not recursively. --long asks for sizes, which git
// answers from the object header rather than by inflating each blob.
func (s *Store) Tree(ctx context.Context, repo Repo, rev, path string) ([]TreeEntry, error) {
	spec := rev + ":" + path
	if path == "" {
		spec = rev + ":"
	}
	out, err := run(ctx, repo, "ls-tree", "-z", "--long", spec)
	if err != nil {
		return nil, err
	}

	var entries []TreeEntry
	for _, rec := range strings.Split(string(out), "\x00") {
		if rec == "" {
			continue
		}
		// "<mode> SP <type> SP <sha> SP* <size> TAB <name>": the size is space
		// padded and "-" for a tree, so split on the tab first, Fields second.
		tab := strings.IndexByte(rec, '\t')
		if tab < 0 {
			continue
		}
		meta, name := strings.Fields(rec[:tab]), rec[tab+1:]
		if len(meta) < 4 {
			continue
		}
		size, _ := strconv.ParseInt(meta[3], 10, 64)
		full := name
		if path != "" {
			full = path + "/" + name
		}
		entries = append(entries, TreeEntry{
			Mode: meta[0], Type: meta[1], SHA: meta[2],
			Size: size, Name: name, Path: full,
		})
	}

	// git returns tree order, which is byte order with a trailing slash on trees
	// and reads as arbitrary to a person.
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return entries[i].Name < entries[j].Name
	})
	return entries, nil
}

// Blob reads a file at a revision. The size comes from the object header first,
// so an oversized file costs a header read rather than a buffer.
func (s *Store) Blob(ctx context.Context, repo Repo, rev, path string) ([]byte, int64, error) {
	spec := rev + ":" + path

	size, err := s.objectSize(ctx, repo, spec)
	if err != nil {
		return nil, 0, err
	}
	if size > maxBlobSize {
		return nil, size, errTooLarge
	}

	out, err := run(ctx, repo, "cat-file", "blob", spec)
	if err != nil {
		return nil, size, err
	}
	return out, size, nil
}

var errTooLarge = fmt.Errorf("blob exceeds %d bytes", maxBlobSize)

func (s *Store) objectSize(ctx context.Context, repo Repo, spec string) (int64, error) {
	out, err := run(ctx, repo, "cat-file", "-s", spec)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
}

// StreamBlob writes a file's bytes to w without buffering, which is what makes
// /raw work on a file too large to render.
func (s *Store) StreamBlob(ctx context.Context, repo Repo, rev, path string, w io.Writer) error {
	cmd := gitCmd(ctx, repo, "cat-file", "blob", rev+":"+path)
	cmd.Stdout = w
	cmd.Stderr = io.Discard
	return cmd.Run()
}

// Resolve turns a user supplied revision into a commit SHA, and is the validation
// step for anything arriving in a URL. The ^{commit} peel means every caller
// downstream can assume it holds a commit rather than a tag.
func (s *Store) Resolve(ctx context.Context, repo Repo, rev string) (string, error) {
	out, err := run(ctx, repo, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("no such revision: %s", rev)
	}
	sha := strings.TrimSpace(string(out))
	if sha == "" {
		return "", fmt.Errorf("no such revision: %s", rev)
	}
	return sha, nil
}

// Size reports the on-disk size in bytes, the number that decides whether a first
// push fits under cloudflareBodyLimit.
func (s *Store) Size(ctx context.Context, repo Repo) int64 {
	out, err := run(ctx, repo, "count-objects", "-v")
	if err != nil {
		return 0
	}
	var total int64
	for _, line := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		// size and size-pack are both reported in KiB.
		if k == "size" || k == "size-pack" {
			n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			total += n * 1024
		}
	}
	return total
}

// LastCommitTime is the timestamp the index sorts on.
func (s *Store) LastCommitTime(ctx context.Context, repo Repo) time.Time {
	out, err := run(ctx, repo, "for-each-ref", "--sort=-committerdate",
		"--format=%(committerdate:iso-strict)", "--count=1", "refs/heads/")
	if err != nil {
		return time.Time{}
	}
	return parseISO(strings.TrimSpace(string(out)))
}

// Archive streams a tarball or zip of a revision. The prefix gives it a top level
// directory, so extracting does not scatter files into the current one.
func (s *Store) Archive(ctx context.Context, repo Repo, rev, format, prefix string, w io.Writer) error {
	switch format {
	case "tar.gz", "zip":
	default:
		return fmt.Errorf("unsupported archive format: %s", format)
	}
	cmd := gitCmd(ctx, repo, "archive",
		"--format="+format, "--prefix="+prefix+"/", rev)
	cmd.Stdout = w
	cmd.Stderr = io.Discard
	return cmd.Run()
}

// InitBare creates a repository and is the whole of push-to-create.
// receive.autogc=false keeps a repack out of a push, and core.logAllRefUpdates
// gives a bare repository a reflog so a force push leaves the old tip recoverable.
func (s *Store) InitBare(ctx context.Context, name string) (Repo, error) {
	if !validName(name) {
		return Repo{}, fmt.Errorf("invalid repository name: %q", name)
	}
	path := filepath.Join(s.Root, name+".git")
	if _, err := os.Stat(path); err == nil {
		return Repo{Name: name, Path: path}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "init", "--bare", "--initial-branch=main", path)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+os.TempDir())
	if out, err := cmd.CombinedOutput(); err != nil {
		return Repo{}, fmt.Errorf("git init %s: %w: %s", name, err, out)
	}

	repo := Repo{Name: name, Path: path}
	for _, kv := range [][2]string{
		{"receive.autogc", "false"},
		{"core.logAllRefUpdates", "true"},
		{"gc.reflogExpire", "never"},
		{"gc.reflogExpireUnreachable", "never"},
		// Do not set http.receivepack. An explicit false denies the
		// authenticated smart push too, so push-to-create would create the
		// repository and then reject the push that made it with a 403.
	} {
		if _, err := run(ctx, repo, "config", kv[0], kv[1]); err != nil {
			return Repo{}, err
		}
	}
	return repo, nil
}

// GC repacks one repository. Called from a ticker, never from a request.
func (s *Store) GC(ctx context.Context, repo Repo) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	cmd := gitCmd(ctx, repo, "gc", "--auto", "--quiet")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gc %s: %w: %s", repo.Name, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// parseISO reads git's iso-strict form, returning a zero time on failure so a
// malformed date on one ref does not fail a page.
func parseISO(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// catFile is a long lived `git cat-file --batch` per repository, so a listing does
// not fork once per object. Access is serialised because the protocol is a
// conversation, and two callers on one pipe would read each other's bytes.
type catFile struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
}

func newCatFile(repo Repo) (*catFile, error) {
	// Not CommandContext: this process outlives any one request and is closed by
	// Store.Close at shutdown.
	cmd := exec.Command("git", "-c", "core.quotePath=false",
		"--git-dir", repo.Path, "cat-file", "--batch")
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+os.TempDir())

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &catFile{cmd: cmd, stdin: stdin, stdout: bufio.NewReaderSize(stdout, 64<<10)}, nil
}

// object reads one object by revision. The reply is "<sha> <type> <size>" then
// exactly size bytes and a newline, or "<spec> missing".
func (c *catFile) object(spec string) (typ string, data []byte, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, err := io.WriteString(c.stdin, spec+"\n"); err != nil {
		return "", nil, err
	}

	header, err := c.stdout.ReadString('\n')
	if err != nil {
		return "", nil, err
	}
	header = strings.TrimSuffix(header, "\n")

	parts := strings.Fields(header)
	if len(parts) < 3 {
		// "missing" or "ambiguous": what a request for an absent path looks like.
		return "", nil, fmt.Errorf("cat-file: %s", header)
	}
	size, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return "", nil, fmt.Errorf("cat-file: bad size in %q", header)
	}
	if size > maxBlobSize {
		// The payload still has to be drained or the stream desynchronises
		// and every later read on this pipe returns another object's bytes.
		if _, err := io.CopyN(io.Discard, c.stdout, size+1); err != nil {
			return "", nil, err
		}
		return parts[1], nil, errTooLarge
	}

	buf := make([]byte, size)
	if _, err := io.ReadFull(c.stdout, buf); err != nil {
		return "", nil, err
	}
	// The trailing newline git writes after every payload.
	if _, err := c.stdout.Discard(1); err != nil {
		return "", nil, err
	}
	return parts[1], buf, nil
}

func (c *catFile) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.stdin.Close()
	_ = c.cmd.Wait()
}

// batch returns the reader for a repository, starting one on first use.
func (s *Store) batch(repo Repo) (*catFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if c, ok := s.batches[repo.Name]; ok {
		return c, nil
	}
	c, err := newCatFile(repo)
	if err != nil {
		return nil, err
	}
	s.batches[repo.Name] = c
	return c, nil
}

// Object reads one object through the long lived reader.
func (s *Store) Object(repo Repo, spec string) (string, []byte, error) {
	c, err := s.batch(repo)
	if err != nil {
		return "", nil, err
	}
	return c.object(spec)
}

// Close stops every cat-file reader, so a restart leaves no git process per
// repository behind.
func (s *Store) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for name, c := range s.batches {
		c.close()
		delete(s.batches, name)
	}
}
