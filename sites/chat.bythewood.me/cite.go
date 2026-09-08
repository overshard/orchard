// Citations. The model points at a source by number and never writes the
// address, so a link on this page is one a tool really fetched rather than one
// a small model half remembered. It wrote three dead hostnames under an answer
// before this existed.
//
// The number it wrote is then repaired here rather than trusted, and a sentence
// it left uncited gets one if the wording clearly came from a source. That is
// search's shape without search's cost: no entailment call per sentence, which
// is the part that makes an answer there take a minute.
package main

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"chat.bythewood.me/tools"
)

// How many sources the model is offered. Pages it fetched go in first, since
// those are what it actually read, and search hits fill the rest.
const (
	maxSources     = 14
	maxNewsSources = 32
)

// Source is one page this turn retrieved. Text never leaves the process, it is
// only what a sentence is matched against.
type Source struct {
	N     int    `json:"n"`
	URL   string `json:"url"`
	Title string `json:"title"`
	Site  string `json:"site"`

	Text   string          `json:"-"`
	tokens map[string]bool `json:"-"`
}

// collectSources numbers everything the turn read. A url seen as a search hit
// and then fetched is one source, keeping the hit's title and the page's text.
func collectSources(used []tools.Result) []Source {
	type entry struct {
		url, title, text string
		fetched          bool
	}
	var order []*entry
	seen := map[string]*entry{}
	add := func(raw, title, text string, fetched bool) {
		u := tidyURL(raw)
		if u == "" {
			return
		}
		e, ok := seen[u]
		if !ok {
			e = &entry{url: u}
			seen[u] = e
			order = append(order, e)
		}
		if e.title == "" {
			e.title = strings.TrimSpace(title)
		}
		if len(text) > len(e.text) {
			e.text = text
		}
		e.fetched = e.fetched || fetched
	}

	for _, r := range used {
		if r.Err != "" {
			continue
		}
		m, _ := r.Content.(map[string]any)
		if m == nil {
			continue
		}
		switch r.Name {
		case tools.WebFetch.Name:
			add(asString(m["url"]), "", asString(m["text"]), true)
		case tools.WebSearch.Name:
			hits, _ := m["results"].([]tools.SearchHit)
			for _, h := range hits {
				add(h.URL, h.Title, h.Title+" "+h.Snippet, false)
			}
		case tools.News.Name:
			// A rundown named its publishers in the prose and linked none of
			// them, so the one answer most worth clicking through was the one
			// with nothing to click. Each headline is a page that was read.
			sections, _ := m["sections"].([]tools.NewsSection)
			for _, sec := range sections {
				for _, it := range sec.Items {
					add(it.URL, it.Headline, it.Headline+" "+it.Summary, false)
				}
			}
		}
	}

	// A rundown is twenty to thirty items and every one of them is a page worth
	// a link, where an ordinary turn reads a handful. Capping both the same way
	// left most of the news with nothing to click.
	cap := maxSources
	for _, r := range used {
		if r.Name == tools.News.Name && r.Err == "" {
			cap = maxNewsSources
			break
		}
	}

	var out []Source
	for _, want := range []bool{true, false} {
		for _, e := range order {
			if e.fetched != want || len(out) >= cap {
				continue
			}
			s := Source{
				N: len(out) + 1, URL: e.url, Title: e.title,
				Site: siteOf(e.url), Text: e.text,
			}
			if s.Title == "" {
				s.Title = s.Site
			}
			s.tokens = tokenSet(s.Text + " " + s.Title)
			out = append(out, s)
		}
	}
	return out
}

// prompt is the numbered list the final turn is handed. Titles are trimmed hard
// because a page title runs to a headline and a sentence of standfirst, and the
// model only needs enough to tell one source from another.
func sourcePrompt(srcs []Source) string {
	if len(srcs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nSources, by number:\n")
	for _, s := range srcs {
		fmt.Fprintf(&b, "[%d] %s, %s\n", s.N, s.Site, trimLine(s.Title, 90))
	}
	b.WriteString("\nEnd a sentence that came from one of these with its number, like [2]. " +
		"Write the number and never the address, and do not list the sources at the end, since they are shown under your answer.")
	return b.String()
}

var citeMark = regexp.MustCompile(`\[(\d{1,3})\]`)

// A marker the model put after the full stop belongs to the sentence in front
// of it, so it moves inside before anything is split.
var markAfterStop = regexp.MustCompile(`([.!?:])((?:\s*\[\d{1,3}\])+)`)

// attach repairs the citations in a block of markdown. A number with no source
// behind it is dropped, a sentence that cites nothing is matched against what
// the turn read, and every marker ends up in the same place.
func attach(text string, srcs []Source) string {
	known := make(map[int]bool, len(srcs))
	for _, s := range srcs {
		known[s.N] = true
	}
	lines := strings.Split(text, "\n")
	fenced := false
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fenced = !fenced
			continue
		}
		// A pill in a heading, a quote or a table cell is in the way rather
		// than in the margin, and a fence is code somebody is about to copy.
		if fenced || t == "" || strings.HasPrefix(t, "#") ||
			strings.HasPrefix(t, ">") || strings.HasPrefix(t, "|") {
			continue
		}
		lines[i] = citeLine(line, srcs, known)
	}
	return strings.Join(lines, "\n")
}

func citeLine(line string, srcs []Source, known map[int]bool) string {
	indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	body := line[len(indent):]
	marker := ""
	if item, ok := listItem(body); ok {
		marker, body = body[:len(body)-len(item)], item
	}
	body = markAfterStop.ReplaceAllString(body, "$2$1")

	var out []string
	for _, s := range sentences(body) {
		if c := citeSentence(s, srcs, known); c != "" {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return line
	}
	return indent + marker + strings.Join(out, " ")
}

// citeSentence keeps the numbers that name a real source, drops the rest, and
// looks one up when the model gave none.
func citeSentence(s string, srcs []Source, known map[int]bool) string {
	var ids []int
	seen := map[int]bool{}
	// Outside code only. In a code span [1] is an index, and both reading it
	// as a citation and deleting it break something a reader is about to copy.
	body := eachOutsideCode(s, func(part string) string {
		for _, m := range citeMark.FindAllStringSubmatch(part, -1) {
			n, _ := strconv.Atoi(m[1])
			if known[n] && !seen[n] {
				seen[n] = true
				ids = append(ids, n)
			}
		}
		return citeMark.ReplaceAllString(part, "")
	})
	body = spaceBeforePunct.ReplaceAllString(spaceRun.ReplaceAllString(body, " "), "$1")
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	if len(ids) == 0 {
		if n := bestSource(body, srcs); n > 0 {
			ids = append(ids, n)
		}
	}
	var b strings.Builder
	b.WriteString(body)
	for _, n := range ids {
		if !endsSentence(body) {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "[%d]", n)
	}
	return b.String()
}

var (
	spaceRun = regexp.MustCompile(`[ \t]{2,}`)
	// Lifting a marker out from in front of the full stop leaves the space it
	// was sitting on.
	spaceBeforePunct = regexp.MustCompile(` +([,.;:!?])`)
)

func endsSentence(s string) bool {
	if s == "" {
		return false
	}
	switch s[len(s)-1] {
	case '.', '!', '?', ':', ')', '"', '\'', '*', ']':
		return true
	}
	return false
}

// How much of a sentence has to be in a source before it is cited, and how many
// of its distinctive words have to be. The share alone is not enough, since two
// pages about one story share most of their ordinary vocabulary and the thing
// that tells them apart is the figure or the name only one of them carries.
const (
	citeFloor      = 0.5
	minDistinctive = 2
)

// bestSource picks what a sentence came from, or 0 when nothing is close
// enough. Silence is the right answer here: a pill on a sentence the page does
// not support is worse than no pill, since it is read as a check that passed.
func bestSource(sentence string, srcs []Source) int {
	words := tokenSet(sentence)
	if len(words) < 4 {
		return 0
	}
	rare := distinctive(sentence)
	best, bestScore, bestRare := 0, 0.0, 0
	for _, s := range srcs {
		var hit, total float64
		matched := 0
		for w := range words {
			weight := 1.0
			if rare[w] {
				weight = 3
			}
			total += weight
			if s.tokens[w] {
				hit += weight
				if rare[w] {
					matched++
				}
			}
		}
		if total == 0 {
			continue
		}
		score := hit / total
		if matched < minDistinctive || score < citeFloor {
			continue
		}
		if matched > bestRare || (matched == bestRare && score > bestScore) {
			best, bestScore, bestRare = s.N, score, matched
		}
	}
	return best
}

var wordRun = regexp.MustCompile(`[a-z0-9]+`)

// tokenSet is the words worth matching on. Commas go first so 48,000 in a
// sentence and 48,000 on the page are the same token.
func tokenSet(s string) map[string]bool {
	s = strings.ToLower(strings.ReplaceAll(s, ",", ""))
	out := map[string]bool{}
	for _, w := range wordRun.FindAllString(s, -1) {
		if stopword[w] || (len(w) < 3 && !hasDigit(w)) {
			continue
		}
		out[w] = true
	}
	return out
}

// distinctive is the half of a sentence that identifies which page it came
// from: the figures, the dates and the names. A word is a name here if it is
// capitalised anywhere but the opening of the sentence.
func distinctive(sentence string) map[string]bool {
	out := map[string]bool{}
	for _, f := range strings.Fields(strings.ReplaceAll(sentence, ",", "")) {
		clean := strings.Trim(f, `.,;:!?"'()[]*_`)
		low := strings.ToLower(strings.Join(wordRun.FindAllString(strings.ToLower(clean), -1), ""))
		if low == "" || stopword[low] {
			continue
		}
		if hasDigit(low) {
			out[low] = true
			continue
		}
		// A capital anywhere counts, the opening word included. The stopword
		// list already covers the ordinary openers, and refusing the first word
		// loses the name in "Hampshire Police opened an investigation", which
		// is the whole of what identifies the page it came from.
		if len(clean) > 2 && clean[0] >= 'A' && clean[0] <= 'Z' {
			out[low] = true
		}
	}
	return out
}

func hasDigit(s string) bool {
	return strings.ContainsAny(s, "0123456789")
}

// sentences splits a line into the units a citation can hang off. It is crude
// on purpose, since a split inside an abbreviation costs nothing here. A code
// span is stepped over, because a full stop in `fmt.Println` is not one.
func sentences(s string) []string {
	rs := []rune(s)
	var out []string
	start, tick := 0, false
	for i := 0; i < len(rs); i++ {
		switch {
		case rs[i] == '`':
			tick = !tick
			continue
		case tick, rs[i] != '.' && rs[i] != '!' && rs[i] != '?':
			continue
		}
		j := i + 1
		for j < len(rs) && strings.ContainsRune(`.!?")']*`, rs[j]) {
			j++
		}
		k := j
		for k < len(rs) && rs[k] == ' ' {
			k++
		}
		if k == j || k >= len(rs) || !opensSentence(rs[k]) {
			continue
		}
		if piece := strings.TrimSpace(string(rs[start:j])); piece != "" {
			out = append(out, piece)
		}
		start, i = k, k-1
	}
	if rest := strings.TrimSpace(string(rs[start:])); rest != "" {
		out = append(out, rest)
	}
	return out
}

func opensSentence(r rune) bool {
	return r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
		strings.ContainsRune("*_`\"([", r)
}

func listItem(t string) (string, bool) {
	if len(t) > 2 && (t[0] == '-' || t[0] == '*' || t[0] == '+') && t[1] == ' ' {
		return strings.TrimSpace(t[2:]), true
	}
	for i := 0; i < len(t) && i < 3; i++ {
		if t[i] >= '0' && t[i] <= '9' {
			continue
		}
		if i > 0 && (t[i] == '.' || t[i] == ')') && i+1 < len(t) && t[i+1] == ' ' {
			return strings.TrimSpace(t[i+2:]), true
		}
		break
	}
	return "", false
}

// cited reports which sources an answer actually points at, in number order, so
// the row under a message lists what was used rather than everything read.
func cited(text string, srcs []Source) []Source {
	used := map[int]bool{}
	for _, m := range citeMark.FindAllStringSubmatch(text, -1) {
		n, _ := strconv.Atoi(m[1])
		used[n] = true
	}
	var out []Source
	for _, s := range srcs {
		if used[s.N] {
			out = append(out, s)
		}
	}
	return out
}

// linkCitations turns [3] into an anchor after rendering rather than before,
// since goldmark drops raw HTML written into the markdown. It substitutes only
// in text, never inside a tag, and never inside code, where [0] is an index
// somebody is about to copy.
func linkCitations(h string, srcs []Source) string {
	byN := make(map[int]Source, len(srcs))
	for _, s := range srcs {
		byN[s.N] = s
	}
	anchor := func(text string) string {
		return citeMark.ReplaceAllStringFunc(text, func(m string) string {
			n, _ := strconv.Atoi(strings.Trim(m, "[]"))
			s, ok := byN[n]
			if !ok {
				return m
			}
			return fmt.Sprintf(
				`<a class="cite" href="%s" target="_blank" rel="noopener noreferrer" title="%s">%d</a>`,
				escapeHTML(s.URL), escapeHTML(s.Site+", "+s.Title), n)
		})
	}

	var out strings.Builder
	out.Grow(len(h) + 64)
	for {
		open := strings.IndexByte(h, '<')
		if open < 0 {
			out.WriteString(anchor(h))
			break
		}
		out.WriteString(anchor(h[:open]))
		shut := strings.IndexByte(h[open:], '>')
		if shut < 0 {
			out.WriteString(h[open:])
			break
		}
		tag := h[open : open+shut+1]
		out.WriteString(tag)
		h = h[open+shut+1:]
		if name, ok := verbatimTag(tag); ok {
			end := strings.Index(h, "</"+name)
			if end < 0 {
				out.WriteString(h)
				break
			}
			out.WriteString(h[:end])
			h = h[end:]
		}
	}
	return out.String()
}

func verbatimTag(tag string) (string, bool) {
	for _, name := range []string{"pre", "code"} {
		if tag == "<"+name+">" || strings.HasPrefix(tag, "<"+name+" ") {
			return name, true
		}
	}
	return "", false
}

func escapeHTML(s string) string {
	return strings.NewReplacer(
		"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;", "'", "&#39;",
	).Replace(s)
}

// A trailing list of addresses, which is what the model wrote before it had
// numbers to write instead. The row under the message replaces it, and half of
// them arrived with no scheme and so were never links at all.
var (
	sourceHeading = regexp.MustCompile(`(?i)^\**(sources?|references?|links?)\**\s*:?\s*`)
	bareAddress   = regexp.MustCompile(`^(?:https?://)?(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,24}(?:/\S*)?$`)
)

// dropSourceList removes an address dump from the end of an answer. It works
// backwards and stops at the first line that is prose, so an answer ending on a
// real sentence is untouched.
//
// A bulleted address only goes when a Sources heading sits above it, because a
// list of links in the middle of an answer is a list of links the reader asked
// for and dropping it loses the answer.
func dropSourceList(text string) string {
	lines := strings.Split(text, "\n")
	plain, any := len(lines), len(lines)
	for i := len(lines) - 1; i >= 0; i-- {
		t := strings.TrimSpace(lines[i])
		if t == "" {
			continue
		}
		bullet := false
		rest := sourceHeading.ReplaceAllString(t, "")
		if trimmed, ok := listItem(rest); ok {
			rest, bullet = trimmed, true
		}
		if rest == "" && t != rest {
			// A heading on its own line, with the addresses below it.
			any, plain = i, i
			continue
		}
		if !onlyAddresses(rest) {
			break
		}
		any = i
		if !bullet {
			plain = i
		}
	}
	cut := plain
	// The heading is what makes a bulleted run a source list rather than part
	// of the answer.
	if any < plain {
		for i := any - 1; i >= 0; i-- {
			t := strings.TrimSpace(lines[i])
			if t == "" {
				continue
			}
			if sourceHeading.MatchString(t) && strings.TrimSpace(sourceHeading.ReplaceAllString(t, "")) == "" {
				cut = i
			}
			break
		}
	}
	return strings.TrimRight(strings.Join(lines[:cut], "\n"), "\n")
}

// onlyAddresses reports whether a line is nothing but addresses.
func onlyAddresses(s string) bool {
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' })
	if len(fields) == 0 {
		return false
	}
	for _, f := range fields {
		if f != "and" && !bareAddress.MatchString(f) {
			return false
		}
	}
	return true
}

// An address written without a scheme is not a link to anything, and this model
// writes them that way about half the time. Adding the scheme is what makes
// goldmark's autolinker see it.
var schemeless = regexp.MustCompile(`(^|[\s(])((?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,24}/[^\s)<>]*)`)

func linkBareAddresses(text string) string {
	lines := strings.Split(text, "\n")
	fenced := false
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fenced = !fenced
			continue
		}
		if fenced || !strings.Contains(line, "/") {
			continue
		}
		lines[i] = eachOutsideCode(line, func(part string) string {
			return schemeless.ReplaceAllString(part, "${1}https://${2}")
		})
	}
	return strings.Join(lines, "\n")
}

// eachOutsideCode applies f to the parts of a line that are not in backticks.
func eachOutsideCode(line string, f func(string) string) string {
	parts := strings.Split(line, "`")
	for i := 0; i < len(parts); i += 2 {
		parts[i] = f(parts[i])
	}
	return strings.Join(parts, "`")
}

// tidyURL drops the fragment and the tracking parameters, so the same page
// reached from two searches is one source.
func tidyURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	u.Fragment = ""
	if q := u.Query(); len(q) > 0 {
		for k := range q {
			if strings.HasPrefix(k, "utm_") || k == "fbclid" || k == "gclid" {
				q.Del(k)
			}
		}
		u.RawQuery = q.Encode()
	}
	return u.String()
}

func siteOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return strings.TrimPrefix(u.Host, "www.")
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

// The words that say nothing about which page a sentence came from.
var stopword = map[string]bool{
	"the": true, "and": true, "but": true, "for": true, "not": true, "you": true,
	"are": true, "was": true, "were": true, "has": true, "had": true, "have": true,
	"his": true, "her": true, "its": true, "our": true, "their": true, "them": true,
	"they": true, "this": true, "that": true, "these": true, "those": true,
	"with": true, "from": true, "into": true, "over": true, "under": true,
	"than": true, "then": true, "when": true, "what": true, "which": true,
	"who": true, "whom": true, "will": true, "would": true, "could": true,
	"should": true, "been": true, "being": true, "there": true, "here": true,
	"also": true, "more": true, "most": true, "some": true, "such": true,
	"only": true, "just": true, "very": true, "much": true, "many": true,
	"each": true, "every": true, "any": true, "all": true, "one": true,
	"two": true, "about": true, "after": true, "before": true, "because": true,
	"while": true, "where": true, "how": true, "why": true, "can": true,
	"said": true, "says": true, "say": true, "make": true, "made": true,
	"does": true, "did": true, "done": true, "get": true, "got": true,
	"out": true, "off": true, "own": true, "same": true, "still": true,
	"other": true, "another": true, "between": true, "through": true,
	"during": true, "against": true, "both": true, "well": true, "back": true,
	"even": true, "way": true, "new": true, "now": true, "day": true,
	"like": true, "time": true, "year": true, "years": true, "people": true,
}
