package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Lighthouse audits, by driving the npm CLI as a subprocess.
//
// This is the second subprocess in the repo after analytics' typst, and by far
// the heavier one: it starts a real Chromium and drives it over DevTools for
// up to three minutes. There is no in-process alternative, in Go or in Rust:
// Lighthouse is Google's own JavaScript and reimplementing its scoring would
// mean reimplementing a moving target whose numbers are the entire point.
//
// `bun run --bun` is what keeps Node out of the image. It symlinks `node` to
// bun so the lighthouse shim's `#!/usr/bin/env node` shebang resolves to bun's
// runtime, which is why nothing in this repo installs nodejs or npm.

const (
	lighthouseTimeout = 180 * time.Second
	chromeFlags       = "--headless --no-sandbox --disable-dev-shm-usage --disable-gpu"
	// Enough of stderr to see what went wrong, bounded so a subprocess that
	// dies noisily cannot write a megabyte into a database column.
	stderrKeep = 500
)

// ErrLighthouseMissing means node_modules/.bin/lighthouse is not on disk.
//
// The normal state of a local checkout that has not run `bun install`, and
// deliberately survivable: audits are skipped and recorded as an error on the
// property, and every other part of the app works. In the image it is always
// there.
var ErrLighthouseMissing = errors.New("lighthouse CLI not installed")

// findChromium locates a browser for Lighthouse to drive.
//
// Three sources, in order of how much they were deliberately chosen:
// CHROMIUM_BIN if the operator set it, then PATH, which is what the production
// image's `apk add chromium` provides, then a walk of the Playwright browser
// directory, which is what makes this work inside the webdev container without
// anyone exporting anything.
//
// Lighthouse needs a full Chrome because it drives DevTools; headless-shell is
// last because it can start and then fail in ways that look like a broken site
// rather than a broken browser.
func findChromium() string {
	if p := os.Getenv("CHROMIUM_BIN"); p != "" {
		if isFile(p) {
			return p
		}
	}

	for _, name := range []string{
		"chromium", "chromium-browser", "google-chrome", "chrome", "chrome-headless-shell",
	} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}

	entries, err := os.ReadDir(playwrightDir)
	if err != nil {
		return ""
	}
	// Sorted so the choice is deterministic when two browser builds are
	// installed side by side, which is the normal state of that directory
	// after a Playwright upgrade.
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, rel := range []string{
		"chrome-linux64/chrome",
		"chrome-linux/chrome",
		"chrome-headless-shell-linux64/chrome-headless-shell",
	} {
		for _, name := range names {
			candidate := filepath.Join(playwrightDir, name, rel)
			if isFile(candidate) {
				return candidate
			}
		}
	}
	return ""
}

const playwrightDir = "/opt/playwright-browsers"

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// runLighthouse audits a URL and returns the parsed JSON report.
func runLighthouse(ctx context.Context, root, target string) (map[string]any, error) {
	bin := filepath.Join(root, "node_modules/.bin/lighthouse")
	if !isFile(bin) {
		return nil, fmt.Errorf("%w at %s", ErrLighthouseMissing, bin)
	}

	ctx, cancel := context.WithTimeout(ctx, lighthouseTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bun", "run", "--bun", bin, target,
		"--chrome-flags="+chromeFlags,
		"--output=json",
		"--output-path=stdout",
		"--quiet",
	)
	// A deliberately minimal environment. Lighthouse and Chromium both read a
	// lot of ambient configuration, and an audit whose numbers depend on the
	// shell that started the server is not a measurement.
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/local/bin"}
	if chromium := findChromium(); chromium != "" {
		cmd.Env = append(cmd.Env, "CHROME_PATH="+chromium)
	}
	// Without this, killing the process on timeout leaves the Chromium it
	// spawned running forever. CommandContext kills only the direct child, so
	// the whole group has to go: a wedged audit that leaks a browser every day
	// eats the machine within a week.
	setProcessGroup(cmd)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start lighthouse: %w", err)
	}
	waitErr := cmd.Wait()
	killProcessGroup(cmd)

	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("lighthouse timed out after %s", lighthouseTimeout)
	}
	if waitErr != nil {
		return nil, fmt.Errorf("lighthouse exited: %w: %s", waitErr, tailString(stderr.String(), stderrKeep))
	}

	var report map[string]any
	if err := json.Unmarshal([]byte(stdout.String()), &report); err != nil {
		return nil, fmt.Errorf("parse lighthouse output: %w", err)
	}
	return report, nil
}

// tailString keeps the last n characters, which is where a stack trace puts
// the actual failure.
func tailString(s string, n int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[len(runes)-n:])
}

// Scores are the four category headlines, as whole percentages.
//
// The JSON field names are capitalised because they are rendered directly as
// labels and because they are already in the production database that way. A
// rename would orphan every stored row.
type Scores struct {
	Performance   int64 `json:"Performance"`
	Accessibility int64 `json:"Accessibility"`
	BestPractices int64 `json:"Best practices"`
	SEO           int64 `json:"SEO"`
}

// parseScores pulls the four category scores.
//
// A null score is an error rather than a zero, and that distinction is the
// whole reason this is not three lines. Lighthouse returns null for a category
// it could not evaluate, and storing that as 0 would draw a confident red bar
// claiming the site scored nothing, which is a different and much worse claim
// than "the audit did not complete".
func parseScores(report map[string]any) (*Scores, error) {
	cats, ok := report["categories"].(map[string]any)
	if !ok {
		return nil, errors.New("lighthouse output has no categories")
	}

	pull := func(key string) (float64, bool, error) {
		cat, ok := cats[key].(map[string]any)
		if !ok {
			return 0, false, fmt.Errorf("lighthouse output has no %s category", key)
		}
		score, ok := cat["score"].(float64)
		return score, ok, nil
	}

	type field struct {
		key   string
		label string
		into  *int64
	}
	var s Scores
	fields := []field{
		{"performance", "Performance", &s.Performance},
		{"accessibility", "Accessibility", &s.Accessibility},
		{"best-practices", "Best practices", &s.BestPractices},
		{"seo", "SEO", &s.SEO},
	}

	var nulls []string
	for _, f := range fields {
		score, present, err := pull(f.key)
		if err != nil {
			return nil, err
		}
		if !present {
			nulls = append(nulls, f.label)
			continue
		}
		*f.into = int64(score*100 + 0.5)
	}
	if len(nulls) > 0 {
		return nil, fmt.Errorf("lighthouse returned null scores: %s", strings.Join(nulls, ", "))
	}
	return &s, nil
}

// Metric is one of the weighted performance metrics (LCP, CLS, TBT...).
type Metric struct {
	ID           string   `json:"id"`
	Acronym      string   `json:"acronym"`
	Title        string   `json:"title"`
	DisplayValue string   `json:"display_value"`
	Score        *float64 `json:"score"`
	Weight       float64  `json:"weight"`
}

// Opportunity is a failed audit with an actionable saving attached.
type Opportunity struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	DisplayValue string  `json:"display_value"`
	SavingsMS    float64 `json:"savings_ms"`
}

// Details is the performance breakdown under the headline score.
type Details struct {
	Metrics       []Metric      `json:"metrics"`
	Opportunities []Opportunity `json:"opportunities"`
}

// parseDetails extracts the weighted metrics and the top opportunities.
//
// The filtering is the substance here. Lighthouse's audit list is mostly noise
// for this purpose, and four separate rules cut it down:
//
//   - group "hidden" covers audits Lighthouse keeps for compatibility but no
//     longer scores, such as Time to Interactive. Left in, they would appear
//     as wins the site never earned.
//   - manual, notApplicable and informative audits have no pass or fail to
//     report at all.
//   - anything scoring 0.9 or better already passes.
//   - what survives must carry at least one actionable signal, some saving in
//     milliseconds, in bytes, or against a specific metric. Pure diagnostics
//     like forced-reflow carry none, and would otherwise be listed with a
//     meaningless 0.
func parseDetails(report map[string]any) *Details {
	cats, ok := report["categories"].(map[string]any)
	if !ok {
		return nil
	}
	perf, ok := cats["performance"].(map[string]any)
	if !ok {
		return nil
	}
	audits, ok := report["audits"].(map[string]any)
	if !ok {
		return nil
	}
	refs, ok := perf["auditRefs"].([]any)
	if !ok {
		return nil
	}

	details := &Details{Metrics: []Metric{}, Opportunities: []Opportunity{}}

	for _, raw := range refs {
		ref, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := ref["id"].(string)
		audit, ok := audits[id].(map[string]any)
		if !ok {
			continue
		}
		group, _ := ref["group"].(string)
		weight, _ := ref["weight"].(float64)
		score, hasScore := audit["score"].(float64)

		title, _ := audit["title"].(string)
		displayValue, _ := audit["displayValue"].(string)

		if group == "metrics" && weight > 0 {
			acronym, _ := ref["acronym"].(string)
			if acronym == "" {
				acronym = id
			}
			m := Metric{
				ID:           id,
				Acronym:      acronym,
				Title:        title,
				DisplayValue: displayValue,
				Weight:       weight,
			}
			if hasScore {
				v := score
				m.Score = &v
			}
			details.Metrics = append(details.Metrics, m)
			continue
		}

		if group == "hidden" {
			continue
		}
		switch mode, _ := audit["scoreDisplayMode"].(string); mode {
		case "manual", "notApplicable", "informative":
			continue
		}
		if !hasScore || score >= 0.9 {
			continue
		}

		savingsMS, savingsBytes := 0.0, 0.0
		if d, ok := audit["details"].(map[string]any); ok {
			savingsMS, _ = d["overallSavingsMs"].(float64)
			savingsBytes, _ = d["overallSavingsBytes"].(float64)
		}
		hasMetricSavings := false
		if ms, ok := audit["metricSavings"].(map[string]any); ok {
			for _, v := range ms {
				if n, ok := v.(float64); ok && n > 0 {
					hasMetricSavings = true
					break
				}
			}
		}
		if savingsMS == 0 && savingsBytes == 0 && !hasMetricSavings {
			continue
		}

		details.Opportunities = append(details.Opportunities, Opportunity{
			ID:           id,
			Title:        title,
			DisplayValue: displayValue,
			SavingsMS:    savingsMS,
		})
	}

	// Heaviest metric first, biggest saving first. SliceStable rather than
	// Slice so equal weights keep Lighthouse's own ordering instead of being
	// shuffled: two metrics with the same weight swapping places between
	// audits would look like the page changed when nothing did. That exact
	// class of bug, a HashSet iterated into the UI, was found in analytics'
	// Rust version by this port's predecessor.
	sort.SliceStable(details.Metrics, func(i, j int) bool {
		return details.Metrics[i].Weight > details.Metrics[j].Weight
	})
	sort.SliceStable(details.Opportunities, func(i, j int) bool {
		return details.Opportunities[i].SavingsMS > details.Opportunities[j].SavingsMS
	})
	if len(details.Opportunities) > 10 {
		details.Opportunities = details.Opportunities[:10]
	}

	return details
}

// ScorePair is one category score, for iterating in a fixed order.
type ScorePair struct {
	Label string
	Score int64
}

// Pairs returns the four scores in Lighthouse's own report order.
//
// A method rather than a map because order matters and a map has none. The
// Rust version handed minijinja a serde_json object and iterated it, which is
// the same shape of mistake the analytics port found in its metric tiles: a
// container with no order feeding user-visible output, so the four tiles could
// rearrange themselves between page loads.
func (s *Scores) Pairs() []ScorePair {
	return []ScorePair{
		{"Performance", s.Performance},
		{"Accessibility", s.Accessibility},
		{"Best practices", s.BestPractices},
		{"SEO", s.SEO},
	}
}
