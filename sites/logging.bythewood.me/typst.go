package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// PDF generation, by handing Typst markup to the typst CLI on stdin and
// reading a PDF back on stdout.
//
// The blog and the portfolio compile Typst at build time. A log report cannot,
// for the same reason an analytics property report cannot: it covers an
// arbitrary date range chosen in a query string, over a table that grows every
// second, so there is no finite set to precompile. That is what puts a
// subprocess on a request path here and why this runtime image is Alpine rather
// than FROM scratch.

// typstTimeout bounds one compile, since a subprocess can hang a request in a
// way that outlives it.
const typstTimeout = 30 * time.Second

// Typst runs the CLI, or reports once that it cannot.
type Typst struct {
	once sync.Once
	bin  string
	err  error
}

func NewTypst() *Typst { return &Typst{} }

// ErrTypstMissing means no typst binary is on PATH, which is the normal state
// of a local checkout. The PDF route answers 503 and every other route works.
// In the image the binary is always there.
var ErrTypstMissing = errors.New("typst binary not found on PATH")

func (t *Typst) resolve() (string, error) {
	t.once.Do(func() {
		bin, err := exec.LookPath("typst")
		if err != nil {
			t.err = ErrTypstMissing
			return
		}
		t.bin = bin
	})
	return t.bin, t.err
}

// Render compiles Typst markup to PDF bytes.
//
// root is what Typst resolves absolute paths in the source against, which
// bounds what the compiler can read to that directory. It matters because the
// source is assembled from a template with log messages and request paths in it:
// --root is the fence that keeps a crafted string from reaching outside the
// site directory, and it replaces the path_within check the Rust World
// implementation did by hand.
func (t *Typst) Render(ctx context.Context, root, source string) ([]byte, error) {
	bin, err := t.resolve()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, typstTimeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, "compile", "--root", root, "-", "-")
	cmd.Stdin = strings.NewReader(source)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Dir = root
	// Typst reads fonts through fontconfig, which wants somewhere writable for
	// its cache. Without this it warns on every single compile in a
	// read-only container.
	cmd.Env = append(os.Environ(), "XDG_CACHE_HOME=/tmp")

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("typst compile timed out after %s", typstTimeout)
		}
		return nil, fmt.Errorf("typst compile: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if stdout.Len() == 0 {
		return nil, fmt.Errorf("typst produced no output: %s", strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// typstMD escapes a string for Typst's markup mode.
//
// "/" is in the set because a page URL is user data and "//" starts a Typst
// line comment, so a URL containing one would swallow the rest of that line of
// the report.
func typstMD(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '\\', '[', ']', '*', '_', '`', '#', '$', '<', '@', '~', '/':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// typstStr escapes a string for a Typst string literal, where the markup rules
// above do not apply and only the quote and the backslash are dangerous.
func typstStr(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
