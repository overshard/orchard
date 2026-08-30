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

// `bun run --bun` below keeps Node out of the image: it symlinks `node` to bun,
// so the lighthouse shim's `#!/usr/bin/env node` shebang resolves to bun.

const (
	lighthouseTimeout = 180 * time.Second
	chromeFlags       = "--headless --no-sandbox --disable-dev-shm-usage --disable-gpu"
	// Bounded so a subprocess that dies noisily cannot fill a database column.
	stderrKeep = 500
)

// ErrLighthouseMissing means node_modules/.bin/lighthouse is not on disk, the
// normal state of a checkout without `bun install`. Audits are skipped.
var ErrLighthouseMissing = errors.New("lighthouse CLI not installed")

// findChromium tries CHROMIUM_BIN, then PATH, then the Playwright browser
// directory. Lighthouse drives DevTools and needs a full Chrome, so
// headless-shell is last: it can fail in ways that look like a broken site.
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
	// Sorted so the choice is stable when two browser builds sit side by side.
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
	// A minimal environment: Lighthouse and Chromium both read a lot of ambient
	// configuration, and the numbers must not depend on the calling shell.
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/local/bin"}
	if chromium := findChromium(); chromium != "" {
		cmd.Env = append(cmd.Env, "CHROME_PATH="+chromium)
	}
	// CommandContext kills only the direct child, so a timeout would otherwise
	// leave the Chromium this spawned running forever.
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

// tailString keeps the last n characters, where a stack trace puts the failure.
func tailString(s string, n int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[len(runes)-n:])
}

// Scores are the four category headlines, as whole percentages. The JSON names
// are the rendered labels and are stored that way, so renaming one orphans rows.
type Scores struct {
	Performance   int64 `json:"Performance"`
	Accessibility int64 `json:"Accessibility"`
	BestPractices int64 `json:"Best practices"`
	SEO           int64 `json:"SEO"`
}

// parseScores pulls the four category scores. A null is an error, not a zero:
// Lighthouse returns null for a category it could not evaluate, and a stored 0
// would draw a red bar claiming the site scored nothing.
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

// parseDetails extracts the weighted metrics and the top opportunities. Group
// "hidden" is audits Lighthouse keeps but no longer scores, and an opportunity
// with no saving attached is a diagnostic; both would read as findings.
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

	// SliceStable, so equal weights keep Lighthouse's own order: two metrics
	// swapping places between audits would look like the page had changed.
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

// Pairs returns the four scores in Lighthouse's own report order. A slice, not
// a map, so the tiles cannot rearrange themselves between page loads.
func (s *Scores) Pairs() []ScorePair {
	return []ScorePair{
		{"Performance", s.Performance},
		{"Accessibility", s.Accessibility},
		{"Best practices", s.BestPractices},
		{"SEO", s.SEO},
	}
}
