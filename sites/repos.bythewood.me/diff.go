package main

// The unified diff parser.
//
// This file is the price of shelling out to git for everything. An in-process
// library would hand back hunks as structs; git hands back the text format, so
// the text format gets parsed once here and every template downstream works
// with the same structs a library would have given.
//
// It is worth the fifteen minutes it costs, because the parsed form is what
// makes the diff view better than a coloured <pre>: line numbers on both sides,
// per-file counts, collapsible hunks, and a rename shown as a rename rather
// than as a delete next to an add.

import (
	"bufio"
	"bytes"
	"context"
	"strconv"
	"strings"
)

// maxDiffSize caps what is parsed. Past this the page shows counts and a link
// to the raw patch instead. A generated lockfile or a vendored dependency drop
// is a legitimate multi-megabyte diff, and rendering one as HTML is a page
// nobody can read and a lot of memory to build it with.
const maxDiffSize = 2 << 20

// Change is the kind of thing that happened to one file.
type Change string

const (
	Added    Change = "added"
	Deleted  Change = "deleted"
	Modified Change = "modified"
	Renamed  Change = "renamed"
	Copied   Change = "copied"
)

// FileDiff is one file's worth of a commit.
type FileDiff struct {
	OldPath   string
	NewPath   string
	Status    Change
	Binary    bool
	Additions int
	Deletions int
	Hunks     []Hunk
	// Mode is set only when it changed, because a mode change with no
	// content change is otherwise an entry with nothing in it.
	OldMode string
	NewMode string
}

// Path is what the UI labels the file with: the new name, except for a delete,
// where the new name does not exist.
func (f FileDiff) Path() string {
	if f.Status == Deleted {
		return f.OldPath
	}
	return f.NewPath
}

// Hunk is one @@ block.
type Hunk struct {
	Header   string
	OldStart int
	OldLines int
	NewStart int
	NewLines int
	Lines    []DiffLine
}

// DiffLine carries both line numbers, which is what a side by side or a
// gutter-numbered unified view needs and what the raw text does not have.
// A zero number means the line does not exist on that side.
type DiffLine struct {
	Kind   string // "context", "add", "del"
	OldNum int
	NewNum int
	Text   string
}

// CommitDiff is the whole patch for one commit.
type CommitDiff struct {
	Files     []FileDiff
	Additions int
	Deletions int
	// Truncated is set when the patch was larger than maxDiffSize, so the
	// template can say so rather than silently showing a partial commit as
	// if it were the whole thing.
	Truncated bool
}

// Diff reads and parses one commit's patch.
//
// -r recurses into subdirectories, without which a change inside a directory is
// reported as one opaque tree change. -M detects renames, which is what turns a
// delete and an add of an identical file into a single rename row. --root makes
// the very first commit in a repository produce a diff against the empty tree
// instead of nothing at all.
func (s *Store) Diff(ctx context.Context, repo Repo, sha string) (CommitDiff, error) {
	out, err := run(ctx, repo, "diff-tree", "-p", "-r", "-M", "--root",
		"--no-color", "--patch-with-raw", "--format=", sha)
	if err != nil {
		return CommitDiff{}, err
	}
	return parseDiff(out), nil
}

// DiffRange is the same for two revisions, which is what a compare view uses.
func (s *Store) DiffRange(ctx context.Context, repo Repo, from, to string) (CommitDiff, error) {
	out, err := run(ctx, repo, "diff", "-p", "-M", "--no-color", from, to, "--")
	if err != nil {
		return CommitDiff{}, err
	}
	return parseDiff(out), nil
}

func parseDiff(patch []byte) CommitDiff {
	var d CommitDiff

	if len(patch) > maxDiffSize {
		patch = patch[:maxDiffSize]
		d.Truncated = true
	}

	sc := bufio.NewScanner(bytes.NewReader(patch))
	// A single line of a minified bundle can be megabytes, and the default
	// 64KB token limit would end the scan mid-file with no error the caller
	// can see.
	sc.Buffer(make([]byte, 0, 64<<10), maxDiffSize)

	var cur *FileDiff
	var hunk *Hunk
	oldNum, newNum := 0, 0

	flushHunk := func() {
		if cur != nil && hunk != nil {
			cur.Hunks = append(cur.Hunks, *hunk)
			hunk = nil
		}
	}
	flushFile := func() {
		flushHunk()
		if cur != nil {
			d.Files = append(d.Files, *cur)
			d.Additions += cur.Additions
			d.Deletions += cur.Deletions
			cur = nil
		}
	}

	for sc.Scan() {
		line := sc.Text()

		switch {
		case strings.HasPrefix(line, "diff --git "):
			flushFile()
			old, new := parseDiffGit(line)
			cur = &FileDiff{OldPath: old, NewPath: new, Status: Modified}

		case cur == nil:
			// The raw section --patch-with-raw emits before the first
			// file, and anything else ahead of the first header.
			continue

		case strings.HasPrefix(line, "old mode "):
			cur.OldMode = strings.TrimPrefix(line, "old mode ")
		case strings.HasPrefix(line, "new mode "):
			cur.NewMode = strings.TrimPrefix(line, "new mode ")

		case strings.HasPrefix(line, "new file mode "):
			cur.Status = Added
			cur.OldPath = ""
		case strings.HasPrefix(line, "deleted file mode "):
			cur.Status = Deleted
			cur.NewPath = ""

		case strings.HasPrefix(line, "rename from "):
			cur.Status = Renamed
			cur.OldPath = strings.TrimPrefix(line, "rename from ")
		case strings.HasPrefix(line, "rename to "):
			cur.Status = Renamed
			cur.NewPath = strings.TrimPrefix(line, "rename to ")
		case strings.HasPrefix(line, "copy from "):
			cur.Status = Copied
			cur.OldPath = strings.TrimPrefix(line, "copy from ")
		case strings.HasPrefix(line, "copy to "):
			cur.Status = Copied
			cur.NewPath = strings.TrimPrefix(line, "copy to ")

		case strings.HasPrefix(line, "Binary files "),
			strings.HasPrefix(line, "GIT binary patch"):
			// No hunks follow, and the bytes are not text. The file still
			// belongs in the list so the commit shows that it changed.
			cur.Binary = true

		case strings.HasPrefix(line, "@@"):
			flushHunk()
			h := parseHunkHeader(line)
			hunk = &h
			oldNum, newNum = h.OldStart, h.NewStart

		case hunk == nil:
			// index lines, --- and +++, and the mode-only tail.
			continue

		case strings.HasPrefix(line, "+"):
			hunk.Lines = append(hunk.Lines, DiffLine{
				Kind: "add", NewNum: newNum, Text: line[1:],
			})
			newNum++
			cur.Additions++

		case strings.HasPrefix(line, "-"):
			hunk.Lines = append(hunk.Lines, DiffLine{
				Kind: "del", OldNum: oldNum, Text: line[1:],
			})
			oldNum++
			cur.Deletions++

		case strings.HasPrefix(line, "\\"):
			// "\ No newline at end of file". It annotates the line above
			// rather than being one, and numbering it shifts every line
			// after it by one.
			continue

		case strings.HasPrefix(line, " "), line == "":
			// An empty context line arrives as a bare "" rather than as a
			// single space, because some tools strip trailing whitespace
			// from the patch on the way through.
			text := ""
			if line != "" {
				text = line[1:]
			}
			hunk.Lines = append(hunk.Lines, DiffLine{
				Kind: "context", OldNum: oldNum, NewNum: newNum, Text: text,
			})
			oldNum++
			newNum++
		}
	}
	flushFile()

	return d
}

// parseDiffGit pulls the two paths out of a "diff --git a/x b/y" line.
//
// This is the line that needs core.quotePath=false to be readable at all: with
// quoting on, a path with a non-ASCII byte arrives wrapped in quotes with octal
// escapes inside, and the a/ b/ split below finds the wrong boundary. gitCmd
// sets the flag; this function assumes it.
//
// The split is genuinely ambiguous: a path may itself contain " b/", and the
// header gives nothing to disambiguate on. So rather than guess with one index,
// every candidate separator is tried and the one whose two halves are equal
// wins. That is correct for every case except a rename, and a rename also emits
// "rename from" and "rename to" lines, which the caller parses and which
// overwrite whatever this returned. Falling back to the last candidate keeps
// the old behaviour for anything that matches nothing.
func parseDiffGit(line string) (old, new string) {
	rest := strings.TrimPrefix(line, "diff --git ")

	var lastA, lastB string
	found := false
	for i := 0; i+3 <= len(rest); i++ {
		if rest[i:i+3] != " b/" {
			continue
		}
		a := strings.TrimPrefix(rest[:i], "a/")
		b := strings.TrimPrefix(rest[i+1:], "b/")
		lastA, lastB, found = a, b, true
		if a == b {
			return a, b
		}
	}
	if !found {
		return "", ""
	}
	return lastA, lastB
}

// parseHunkHeader reads "@@ -12,7 +12,9 @@ optional context".
func parseHunkHeader(line string) Hunk {
	h := Hunk{Header: line, OldLines: 1, NewLines: 1}

	// Everything between the two @@ markers.
	body := line
	if i := strings.Index(line[2:], "@@"); i >= 0 {
		body = line[2 : 2+i]
	}

	for _, f := range strings.Fields(body) {
		switch {
		case strings.HasPrefix(f, "-"):
			h.OldStart, h.OldLines = parseRange(f[1:])
		case strings.HasPrefix(f, "+"):
			h.NewStart, h.NewLines = parseRange(f[1:])
		}
	}
	return h
}

// parseRange reads "12,7", or "12" which means a single line.
func parseRange(s string) (start, count int) {
	a, b, ok := strings.Cut(s, ",")
	start, _ = strconv.Atoi(a)
	if !ok {
		return start, 1
	}
	count, _ = strconv.Atoi(b)
	return start, count
}
