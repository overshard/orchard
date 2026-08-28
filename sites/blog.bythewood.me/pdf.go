package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// PDF export happens at build time, not per request.
//
// The Rust version compiled Typst in process, because there was a crate for
// it. There is no Go binding, and decisions/0008 left the replacement open
// with the typst CLI as the recommendation. Shelling out per request would
// mean a 50MB static binary in the runtime image and a subprocess on the
// request path, for a document whose inputs cannot change while the process is
// running: posts load once at startup.
//
// So the PDFs are compiled during `docker build`, into their own directory
// beside dist/, and the handler serves a file. That is the same move
// isaacbythewood.com makes with its responsive images, it deletes the byte
// cache and its mutex, and the runtime image needs no typst at all.
//
// A post published on a schedule still gets its PDF here, because the build
// renders every post rather than only the visible ones. The handler is what
// enforces the publish date.

// GeneratePDFs compiles one PDF per post into outDir. root is the directory
// Typst resolves absolute paths against, so /templates/blog_post.typ and
// /content/images/* both land.
func GeneratePDFs(lib *Library, root, fontPath, outDir string) error {
	if _, err := exec.LookPath("typst"); err != nil {
		return fmt.Errorf("typst not on PATH: %w", err)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	posts := lib.All()
	// Typst is single threaded per compile and these are independent, so the
	// whole set costs about one compile of wall clock on any real machine.
	workers := runtime.NumCPU()
	if workers > len(posts) {
		workers = len(posts)
	}

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		errs  []string
		queue = make(chan *Post)
	)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for post := range queue {
				out := filepath.Join(outDir, post.Slug+".pdf")
				if err := compilePDF(typstSource(post), root, fontPath, out); err != nil {
					mu.Lock()
					errs = append(errs, fmt.Sprintf("%s: %v", post.Slug, err))
					mu.Unlock()
					continue
				}
				log.Printf("pdf %s", post.Slug)
			}
		}()
	}
	for _, post := range posts {
		queue <- post
	}
	close(queue)
	wg.Wait()

	if len(errs) > 0 {
		return fmt.Errorf("compile failed for %d post(s):\n  %s", len(errs), strings.Join(errs, "\n  "))
	}
	return nil
}

// compilePDF pipes Typst markup to the compiler and writes a PDF.
//
// Source arrives on stdin rather than through a temporary file: there is
// nothing to clean up on failure, and nothing to collide when the workers run
// side by side.
func compilePDF(source, root, fontPath, out string) error {
	args := []string{"compile", "--root", root}
	if fontPath != "" {
		args = append(args, "--font-path", fontPath)
	}
	args = append(args, "-", out)
	cmd := exec.Command("typst", args...)
	cmd.Stdin = strings.NewReader(source)

	// Typst reports compile errors on stderr and exits non-zero. Carrying the
	// message through matters: "expected string, found content at line 40" is
	// the whole diagnosis, and the exit code alone is not.
	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
