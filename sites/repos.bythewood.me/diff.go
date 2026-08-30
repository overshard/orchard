package main

// The unified diff parser: git hands back the text format, and this turns it into
// the structs every template downstream works with.

import (
	"bufio"
	"bytes"
	"context"
	"strconv"
	"strings"
)

// maxDiffSize caps what is parsed; past it the page shows counts and a link to
// the raw patch.
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
	// Mode is set only when it changed.
	OldMode string
	NewMode string
}

// Path is the new name, or the old one for a delete.
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

// DiffLine carries both line numbers; a zero means the line does not exist on
// that side.
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
	// Truncated is set when the patch was larger than maxDiffSize.
	Truncated bool
}

// Diff reads and parses one commit's patch. -r recurses, -M detects renames, and
// --root makes the first commit in a repository diff against the empty tree
// rather than produce nothing.
func (s *Store) Diff(ctx context.Context, repo Repo, sha string) (CommitDiff, error) {
	out, err := run(ctx, repo, "diff-tree", "-p", "-r", "-M", "--root",
		"--no-color", "--patch-with-raw", "--format=", sha)
	if err != nil {
		return CommitDiff{}, err
	}
	return parseDiff(out), nil
}

// DiffRange is Diff between two revisions.
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
	// A minified bundle can hold a megabyte-long line, and the default 64KB token
	// limit would end the scan mid-file with no error the caller can see.
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
			// The raw section --patch-with-raw emits ahead of the first header.
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
			// No hunks follow, but the file still belongs in the list.
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
			// "\ No newline at end of file" annotates the line above rather
			// than being one; numbering it shifts everything after it.
			continue

		case strings.HasPrefix(line, " "), line == "":
			// An empty context line can arrive as a bare "" rather than a single
			// space, since some tools strip trailing whitespace from a patch.
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

// parseDiffGit pulls the two paths out of "diff --git a/x b/y". The split is
// ambiguous because a path may contain " b/", so every candidate is tried and the
// one whose halves match wins; a rename is corrected by its own header lines.
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
