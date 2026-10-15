package main

// What the contract asks for and doesn't get is repaired in Go instead of asked
// for again, since a rule the model was given and ignored isn't worth a second
// call. None of it changes what an answer says.

import (
	"regexp"
	"strings"
)

// A closing offer of more help, which his stored preference and the contract both
// rule out and answers still end on. Every pattern needs a first person subject or
// an imperative aimed at him, so a real question about what he meant survives.
var closingOffers = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^(want|would you like|do you want|shall i|should i)\b.*\?$`),
	regexp.MustCompile(`(?i)^(if you want|if you'?d like|if that helps|let me know)\b.*[.?!]$`),
	regexp.MustCompile(`(?i)^i can\b.*\bif you\b.*[.?!]$`),
	regexp.MustCompile(`(?i)^(just )?(say|tell me) (the )?(which|what|when)\b.*\band i'?ll\b.*[.?!]$`),
}

// dropClosingOffer removes an offer of further help from the end of an answer.
// Only from the end, and only when something else is left, since an answer that's
// nothing but an offer had a reason to be and the gate deals with it.
func dropClosingOffer(text string) string {
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		t := strings.TrimSpace(lines[i])
		if t == "" {
			continue
		}
		// A list item or a heading is structure and not a sign off, and a fence
		// is code.
		if strings.HasPrefix(t, "#") || strings.HasPrefix(t, ">") ||
			strings.HasPrefix(t, "|") || strings.HasPrefix(t, "```") {
			return text
		}
		if _, ok := listItem(t); ok {
			return text
		}
		kept := withoutOffer(t)
		if kept == t {
			return text
		}
		if strings.TrimSpace(strings.Join(lines[:i], "")) == "" && kept == "" {
			// The offer was the whole answer, and an empty reply is worse than one
			// that asks a question.
			return text
		}
		if kept == "" {
			lines = lines[:i]
		} else {
			lines[i] = kept
		}
		return strings.TrimRight(strings.Join(lines, "\n"), "\n ")
	}
	return text
}

// withoutOffer drops the trailing sentences of one line that are offers.
func withoutOffer(line string) string {
	parts := sentences(line)
	cut := len(parts)
	for i := len(parts) - 1; i >= 0; i-- {
		s := strings.TrimSpace(stripEmphasis(parts[i]))
		matched := false
		for _, re := range closingOffers {
			if re.MatchString(s) {
				matched = true
				break
			}
		}
		if !matched {
			break
		}
		cut = i
	}
	if cut == len(parts) {
		return line
	}
	return strings.TrimSpace(strings.Join(parts[:cut], " "))
}

func stripEmphasis(s string) string { return strings.Trim(strings.TrimSpace(s), "*_ ") }

// A bracketed label like "[S&P 500]" the model wrote where a citation number goes,
// which links to nothing. RE2 has no lookahead, so a markdown link can't be
// excluded in the pattern and the character after the match is checked instead.
var labelMark = regexp.MustCompile(`\s*\[[^\]\n]{1,40}\]`)

// dropLabelMarks removes those, outside code and outside a fence, and leaves
// anything that is really markdown alone.
func dropLabelMarks(text string) string { return outsideCodeLines(text, dropLabelsIn) }

// The prompt asks for no em dashes and the model writes them anyway, mostly after
// a bold label. A comma reads the same in every one of them.
var (
	spacedDash = regexp.MustCompile(`\s*[—–]\s+|\s+[—–]\s*|—`)
	dashRange  = regexp.MustCompile(`(\d)–(\d)`)
)

// A dash alone in a table cell means none, and as a comma it drew "|, |".
var dashCell = regexp.MustCompile(`\|\s*[—–]\s*\|`)

func dropDashes(text string) string {
	return outsideCodeLines(text, func(part string) string {
		for dashCell.MatchString(part) {
			part = dashCell.ReplaceAllString(part, "| |")
		}
		return spacedDash.ReplaceAllString(dashRange.ReplaceAllString(part, "$1-$2"), ", ")
	})
}

func outsideCodeLines(text string, fn func(string) string) string {
	lines := strings.Split(text, "\n")
	fenced := false
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		lines[i] = eachOutsideCode(line, fn)
	}
	return strings.Join(lines, "\n")
}

func dropLabelsIn(part string) string {
	found := labelMark.FindAllStringIndex(part, -1)
	if found == nil {
		return part
	}
	var b strings.Builder
	last := 0
	for _, loc := range found {
		start, end := loc[0], loc[1]
		// A markdown link is the same shape followed by its address.
		if end < len(part) && part[end] == '(' {
			continue
		}
		inner := strings.TrimSpace(strings.Trim(strings.TrimSpace(part[start:end]), "[]"))
		// A citation is a number and stays. So does a task list box and a
		// footnote reference, which are markdown a reader wrote.
		if inner == "" || isAllDigits(inner) || inner == "x" || inner == "X" ||
			strings.HasPrefix(inner, "^") {
			continue
		}
		b.WriteString(part[last:start])
		last = end
	}
	if last == 0 {
		return part
	}
	b.WriteString(part[last:])
	return b.String()
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}
