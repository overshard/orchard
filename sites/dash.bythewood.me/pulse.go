package main

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
)

// Pulse is the line over a feed saying whether anything on it is blowing up,
// worked out from points, comments and age on every poll, since a thread takes
// off in an hour and a brief only runs three times a day.
type Pulse struct {
	Level string `json:"level"`
	Label string `json:"label"`
	Text  string `json:"text"`
}

// feedItem is a story with what the pulse needs and the row doesn't show.
type feedItem struct {
	title    string
	points   int
	comments int
	age      time.Duration
	tags     []string
}

// pulseScale is what counts as big on one site. Lobsters is about a tenth of
// Hacker News, so the same numbers can't serve both.
type pulseScale struct {
	hugePoints, freshPoints, hugeComments int
	busyPoints, busyComments              int
	fresh                                 time.Duration
	theme                                 int
}

var (
	hnScale       = pulseScale{1000, 500, 700, 400, 400, 6 * time.Hour, 5}
	lobstersScale = pulseScale{150, 80, 100, 60, 50, 12 * time.Hour, 4}
)

func feedPulse(items []feedItem, sc pulseScale, theme func([]feedItem) (string, int)) Pulse {
	if len(items) == 0 {
		return Pulse{}
	}
	// Taking off now is a fresh story already high, and a huge one from
	// yesterday is still the biggest thing on the page but isn't blowing up.
	biggest := func(keep func(feedItem) bool) *feedItem {
		var best *feedItem
		for i := range items {
			if keep(items[i]) && (best == nil || items[i].points+items[i].comments > best.points+best.comments) {
				best = &items[i]
			}
		}
		return best
	}
	hot := biggest(func(it feedItem) bool {
		return it.age <= sc.fresh && it.points >= sc.freshPoints || it.age <= 24*time.Hour && it.points >= sc.hugePoints
	})
	big := biggest(func(it feedItem) bool { return it.points >= sc.hugePoints || it.comments >= sc.hugeComments })
	argued := items[0]
	for _, it := range items[1:] {
		if it.comments > argued.comments {
			argued = it
		}
	}

	var p Pulse
	var parts []string
	switch {
	case hot != nil:
		p.Level, p.Label = "hot", "BLOWING UP"
		parts = append(parts, fmt.Sprintf("%s, %d points in %s", short(hot.title), hot.points, hours(hot.age)))
	case big != nil:
		p.Level, p.Label = "big", "BIG THREAD"
		parts = append(parts, fmt.Sprintf("%s, %d points and %d comments", short(big.title), big.points, big.comments))
	case slices.ContainsFunc(items, func(it feedItem) bool { return it.points >= sc.busyPoints || it.comments >= sc.busyComments }):
		p.Level, p.Label = "busy", "BUSY"
		parts = append(parts, fmt.Sprintf("Most argued: %s, %d comments", short(argued.title), argued.comments))
	default:
		p.Level, p.Label = "quiet", "QUIET"
		parts = append(parts, "Nothing taking off")
	}
	if name, n := theme(items); n >= sc.theme {
		parts = append(parts, fmt.Sprintf("%s in %d of %d", name, n, len(items)))
	}
	p.Text = strings.Join(parts, ". ") + "."
	return p
}

func short(title string) string {
	if r := []rune(title); len(r) > 44 {
		cut := string(r[:44])
		if i := strings.LastIndex(cut, " "); i > 24 {
			cut = cut[:i]
		}
		return "“" + cut + "...”"
	}
	return "“" + title + "”"
}

func hours(d time.Duration) string {
	if d < time.Hour {
		return "under an hour"
	}
	if d < 2*time.Hour {
		return "an hour"
	}
	return fmt.Sprintf("%d hours", int(d.Hours()))
}

// aiWords are folded into one topic, since a front page with a story each on
// Claude, DeepSeek and an LLM benchmark is one conversation and not three.
var aiWords = []string{"ai", "llm", "llms", "gpt", "chatgpt", "openai", "claude", "anthropic", "deepseek",
	"gemini", "agent", "agents", "agentic", "copilot", "mistral", "qwen", "llama", "vibe"}

var titleStop = []string{"the", "and", "for", "with", "from", "your", "you", "that", "this", "what", "why", "how",
	"are", "was", "its", "not", "can", "now", "new", "our", "all", "about", "into", "show", "ask", "hn", "pdf",
	"video", "using", "use", "one", "has", "have", "who", "when", "more", "than", "out", "just", "will", "is",
	"of", "to", "in", "on", "a", "an", "my", "we", "i", "it", "by", "at", "as", "or", "be", "do", "vs"}

// hnTheme is the word most titles share, with the AI words counted as one.
func hnTheme(items []feedItem) (string, int) {
	counts := map[string]int{}
	for _, it := range items {
		seen := map[string]bool{}
		for _, w := range strings.FieldsFunc(strings.ToLower(it.title), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '+' && r != '#'
		}) {
			if slices.Contains(aiWords, w) {
				w = "AI"
			}
			if len(w) < 3 && w != "AI" || slices.Contains(titleStop, w) || seen[w] {
				continue
			}
			seen[w] = true
			counts[w]++
		}
	}
	return mostCommon(counts)
}

// lobstersTheme is the tag most stories carry, since Lobsters tags everything.
func lobstersTheme(items []feedItem) (string, int) {
	counts := map[string]int{}
	for _, it := range items {
		for _, t := range it.tags {
			if t == "ai" || t == "vibecoding" {
				t = "AI"
			}
			counts[t]++
		}
	}
	return mostCommon(counts)
}

func mostCommon(counts map[string]int) (string, int) {
	best, n := "", 0
	for w, c := range counts {
		if c > n || c == n && w < best {
			best, n = w, c
		}
	}
	if best != "AI" && best != "" {
		best = strings.ToUpper(best[:1]) + best[1:]
	}
	return best, n
}
