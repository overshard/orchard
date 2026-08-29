package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Social cards, compiled at build time like the PDFs.
//
// These used to be SVG, rendered per request. The design was fine and the
// format was not: Facebook, X, LinkedIn, Slack, iMessage and Discord all
// refuse image/svg+xml for og:image, so every share of this blog since the
// card was written has fallen back to a bare link. PNG is the only format all
// of them agree on.
//
// Rendering moves to build time for the same reason the PDFs did. A post's
// title and tags cannot change while the process runs, so there is nothing to
// recompute per request, and the runtime image stays typst-free.
//
// The design is a straight port of the SVG it replaces, so shares that were
// already cached keep the same look: dark plate, gradient rule, up to three
// lines of title, byline and right aligned tag pills.

const (
	ogWidth  = 1200
	ogHeight = 630

	// The card every page that is not a post points at, named so it cannot
	// collide with a post slug.
	ogSiteCard = "blog"
)

// ogTypstSource renders one card. Text reaches Typst as string literals rather
// than markup, so a title containing #, $, @ or * is drawn rather than parsed.
func ogTypstSource(title string, tags []string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "#set page(width: %dpt, height: %dpt, margin: 0pt, fill: rgb(\"#0d1117\"))\n", ogWidth, ogHeight)
	// Geist, the same face the post PDFs use, reached through --font-path.
	// Typst embeds only DejaVu Sans Mono and its own serif, so without that
	// path this silently falls back to a serif on a card meant to be sans.
	b.WriteString("#set text(font: (\"Geist\", \"DejaVu Sans\"), fill: rgb(\"#f0f6fc\"))\n")

	// Accent bar, left of the title block.
	b.WriteString("#place(dx: 80pt, dy: 80pt, rect(width: 5pt, height: 160pt, " +
		"fill: gradient.linear(rgb(\"#0e3ff4\"), rgb(\"#842bff\"), angle: 90deg)))\n")

	// One placed line per title line, at the same baselines the SVG used, so
	// the card does not reflow if a title happens to wrap differently.
	for i, line := range wrapTitle(title, 35, 3) {
		fmt.Fprintf(&b, "#place(dx: 110pt, dy: %dpt, text(size: 54pt, weight: \"bold\")[#%s])\n",
			110+i*64, typstString(line))
	}

	fmt.Fprintf(&b, "#place(dx: 80pt, dy: 490pt, line(length: %dpt, stroke: 1pt + rgb(\"#30363d\")))\n", ogWidth-160)
	fmt.Fprintf(&b, "#place(dx: 80pt, dy: 520pt, text(size: 28pt, fill: rgb(\"#c9d1d9\"))[#%s])\n",
		typstString(authorName))

	shown := tags
	if len(shown) > 4 {
		shown = shown[:4]
	}
	for i, tag := range shown {
		x := (ogWidth - 80) - (len(shown)-i)*140
		fmt.Fprintf(&b, "#place(dx: %dpt, dy: 520pt, block(width: 128pt, height: 38pt, radius: 19pt, "+
			"fill: rgb(\"#21262d\"), inset: (y: 8pt), align(center, "+
			"text(size: 18pt, fill: rgb(\"#c9d1d9\"))[#%s])))\n", x, typstString(tag))
	}

	return b.String()
}

// typstString quotes a Go string as a Typst string literal. Only the backslash
// and the quote can end the literal early; everything else, markup characters
// included, is inert inside one.
func typstString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}

// GenerateOGCards compiles one PNG per post into outDir, plus the site card
// every non-post page points at.
func GenerateOGCards(lib *Library, root, fontPath, outDir string) error {
	if _, err := exec.LookPath("typst"); err != nil {
		return fmt.Errorf("typst not on PATH: %w", err)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	type job struct {
		name   string
		source string
	}
	jobs := []job{{name: ogSiteCard, source: ogTypstSource(siteName, nil)}}
	for _, post := range lib.All() {
		jobs = append(jobs, job{name: post.Slug, source: ogTypstSource(post.Title, post.Tags)})
	}

	workers := runtime.NumCPU()
	if workers > len(jobs) {
		workers = len(jobs)
	}

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		errs  []string
		queue = make(chan job)
	)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range queue {
				out := filepath.Join(outDir, j.name+".png")
				if err := compilePNG(j.source, root, fontPath, out); err != nil {
					mu.Lock()
					errs = append(errs, fmt.Sprintf("%s: %v", j.name, err))
					mu.Unlock()
					continue
				}
				slog.Info(fmt.Sprintf("og %s", j.name))
			}
		}()
	}
	for _, j := range jobs {
		queue <- j
	}
	close(queue)
	wg.Wait()

	if len(errs) > 0 {
		return fmt.Errorf("compile failed for %d card(s):\n  %s", len(errs), strings.Join(errs, "\n  "))
	}
	return nil
}

// compilePNG is compilePDF with a raster target. At 72 ppi a Typst point is
// exactly one pixel, so the page size above is the pixel size Facebook and X
// are told to expect in og:image:width/height.
func compilePNG(source, root, fontPath, out string) error {
	args := []string{"compile", "--format", "png", "--ppi", "72", "--root", root}
	if fontPath != "" {
		args = append(args, "--font-path", fontPath)
	}
	args = append(args, "-", out)
	cmd := exec.Command("typst", args...)
	cmd.Stdin = strings.NewReader(source)

	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
