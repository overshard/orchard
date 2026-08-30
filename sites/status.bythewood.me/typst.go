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

// ErrTypstMissing means no typst binary is on PATH, the normal state of a local
// checkout. The PDF route answers 503; every other route still works.
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

// Render compiles Typst markup to PDF bytes. root bounds what the compiler may
// read, which matters because the source is assembled from a template carrying
// property names and page URLs.
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
	// Typst reads fonts through fontconfig, which warns on every compile without
	// somewhere writable for its cache.
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

// typstMD escapes a string for Typst's markup mode. "/" is in the set because
// "//" starts a Typst line comment and page URLs are user data.
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

// typstStr escapes a string for a Typst string literal.
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
