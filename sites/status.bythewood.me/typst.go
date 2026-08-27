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
// decisions/0008 chose the CLI over a Go PDF library so the .typ templates
// survive the port unchanged, and blog.bythewood.me then answered "when" with
// "at build time". That answer does not transfer here, for the same reason it
// did not transfer to analytics: a blog post's PDF is fully determined before
// the process boots, because posts are files that cannot change while it runs.
// A property report describes a live monitoring state that changes every three
// minutes. There is no finite set to precompile.
//
// This is therefore the second app in the repo with a subprocess on the
// request path. It is also, unlike analytics, not the heaviest subprocess it
// runs: lighthouse.go starts a whole browser. Both are why this image is not
// FROM scratch.

// typstTimeout bounds one compile. The Rust version had no bound at all,
// because an in-process compile could not hang a request in a way that
// outlived it. A subprocess can.
const typstTimeout = 30 * time.Second

// Typst runs the CLI, or reports once that it cannot.
type Typst struct {
	once sync.Once
	bin  string
	err  error
}

func NewTypst() *Typst { return &Typst{} }

// ErrTypstMissing means no typst binary is on PATH.
//
// The normal state of a local checkout, and deliberately survivable: the PDF
// route answers 503 and every other route works, which is the right dev
// behaviour. In the image the binary is always there.
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
// source is assembled from a template with property names and page URLs in it:
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
// "/" is in the set, which looks excessive until you notice that a page URL is
// user data and "//" starts a Typst line comment. Without it, a tracked site
// with a "//" anywhere in a URL would silently swallow the rest of that line
// of the report. That was found and fixed in the 2026-07-20 hardening pass.
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
