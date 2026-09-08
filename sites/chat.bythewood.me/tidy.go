package main

// The two things the contract asks for and does not get.
//
// Both are repaired in Go rather than asked for again, for cite.go's reason:
// the model writes what it writes, and a rule it has already been given and
// ignored is not worth a second model call. Neither of these changes what an
// answer says, only what is left on the end of it.

import (
	"regexp"
	"strings"
)

// A closing offer of more help. Isaac's stored preference says not to write
// one, the contract repeats it and the final turn repeats it again, and four
// answers on 2026-09-08 still ended on "Want me to...?".
//
// Every pattern needs a first person subject or an imperative aimed at him, so
// a genuine question about what he meant survives. "Which of the two did you
// mean?" is the turn needing an answer to continue and is not an offer.
var closingOffers = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^(want|would you like|do you want|shall i|should i)\b.*\?$`),
	regexp.MustCompile(`(?i)^(if you want|if you'?d like|if that helps|let me know)\b.*[.?!]$`),
	regexp.MustCompile(`(?i)^i can\b.*\bif you\b.*[.?!]$`),
	regexp.MustCompile(`(?i)^(just )?(say|tell me) (the )?(which|what|when)\b.*\band i'?ll\b.*[.?!]$`),
}

// dropClosingOffer removes an offer of further help from the end of an answer.
// Only from the end, and only when something else is left: an answer that is
// nothing but an offer had a reason to be, and the gate is what deals with it.
func dropClosingOffer(text string) string {
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		t := strings.TrimSpace(lines[i])
		if t == "" {
			continue
		}
		// A list item or a heading is structure rather than a sign off, and a
		// fence is code.
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
			// The offer was the whole answer. Leave it: an empty reply is worse
			// than one that asks a question.
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

// A bracketed label the model wrote where a citation number goes. It picked the
// shape up from the numbered sources and filled it with a name, which lands in
// the answer as "The S&P 500 is down 26.05 to 7692.55 [S&P 500]." and links to
// nothing.
//
// RE2 has no lookahead, so a markdown link cannot be excluded in the pattern
// and the character after the match is checked instead.
var labelMark = regexp.MustCompile(`\s*\[[^\]\n]{1,40}\]`)

// dropLabelMarks removes those, outside code and outside a fence, and leaves
// anything that is really markdown alone.
func dropLabelMarks(text string) string {
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
		lines[i] = eachOutsideCode(line, dropLabelsIn)
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
