package main

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// Checks a line has to pass before it goes on the page. A model's own memory of
// who holds what office or which party they're in can be out of date, so
// whatever a line says has to be in the text it was written from.

var (
	numberPat = regexp.MustCompile(`\d[\d,]*(?:\.\d+)?`)
	partyPat  = regexp.MustCompile(`\s*\([RDI]-[A-Z][A-Za-z.]*(?: [A-Z][A-Za-z.]*)*\)`)
)

// Words a line may capitalise without its source having them, which are the
// offices the prompt asks for and the country the reader lives in.
var plainCaps = []string{"u.s", "us", "united", "states", "u.k", "uk", "america", "americans", "american", "eastern",
	"president", "pres", "vice", "sen", "senator", "rep", "representative", "gov", "governor", "secretary",
	"minister", "prime", "chancellor", "speaker", "leader", "mayor", "judge", "justice", "chief",
	"attorney", "general", "director", "chair", "chairman", "chairwoman", "ceo", "mr", "mrs", "ms", "dr", "gen", "lt", "jr"}

var weekdays = []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}

var smallNumbers = []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"}

// stripParties takes out a party tag the source doesn't carry, since one filled
// in from memory can be wrong.
func stripParties(line, source string) string {
	return partyPat.ReplaceAllStringFunc(line, func(tag string) string {
		if strings.Contains(source, strings.TrimSpace(tag)) {
			return tag
		}
		return ""
	})
}

// unsupported is every number and name in line that its source doesn't have.
// Names are matched on their first five letters, the same stem the clustering
// uses, so Venezuelan finds Venezuela.
func unsupported(line, source string) []string {
	src := strings.ToLower(source)
	srcNums := map[string]bool{}
	for _, n := range numberPat.FindAllString(src, -1) {
		srcNums[strings.ReplaceAll(n, ",", "")] = true
	}
	srcWords := strings.FieldsFunc(src, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })

	var missing []string
	line = partyPat.ReplaceAllString(line, "")
	for _, n := range numberPat.FindAllString(line, -1) {
		n = strings.TrimSuffix(strings.ReplaceAll(n, ",", ""), ".")
		if srcNums[n] {
			continue
		}
		if i, err := strconv.Atoi(n); err == nil && i < len(smallNumbers) && slices.Contains(srcWords, smallNumbers[i]) {
			continue
		}
		missing = append(missing, n)
	}

	for _, w := range capitalised(line) {
		lw := strings.ToLower(strings.Trim(w, "."))
		if slices.Contains(plainCaps, lw) || strings.Contains(src, lw) {
			continue
		}
		stem := []rune(lw)
		stem = stem[:min(len(stem), 5)]
		if !slices.ContainsFunc(srcWords, func(s string) bool {
			return strings.HasPrefix(s, string(stem)) || len(s) >= 4 && strings.HasPrefix(string(stem), s)
		}) {
			missing = append(missing, w)
		}
	}
	return missing
}

// capitalised is the words in line that start with a capital and don't start a
// sentence, with a possessive and punctuation cut off. A name opening a
// sentence is let through, since prose opens on any word.
func capitalised(line string) []string {
	var out []string
	start := true
	for _, f := range strings.Fields(line) {
		for _, part := range strings.Split(f, "-") {
			w := strings.TrimFunc(part, func(r rune) bool { return !unicode.IsLetter(r) && r != '.' })
			w = strings.TrimSuffix(strings.TrimSuffix(w, "'s"), "’s")
			w = strings.TrimSuffix(w, ".")
			first := []rune(strings.TrimLeft(part, "\"'(“"))
			if w != "" && !start && len(first) > 0 && unicode.IsUpper(first[0]) {
				out = append(out, w)
			}
		}
		start = strings.HasSuffix(f, ".") && !isAbbrev(f) || strings.HasSuffix(f, ":") || strings.HasSuffix(f, "?")
	}
	return out
}

// isAbbrev is a word whose full stop doesn't end a sentence, like Sen. or U.S.
func isAbbrev(w string) bool {
	w = strings.TrimSuffix(w, ".")
	return len(w) <= 3 || strings.Contains(w, ".")
}

// namesWeekday is the first weekday named in line, or "".
func namesWeekday(line string) string {
	for _, w := range strings.FieldsFunc(strings.ToLower(line), func(r rune) bool { return !unicode.IsLetter(r) }) {
		w = strings.TrimSuffix(w, "s")
		if slices.Contains(weekdays, w) {
			return w
		}
	}
	return ""
}

// relates is whether a line is about the event at all, by sharing a word of
// substance with how any outlet told it, so a cited number the line isn't
// about doesn't get its links.
func relates(line string, c cluster) bool {
	words := significant(line)
	for _, s := range c.stories {
		for _, w := range significant(s.title + " " + s.desc) {
			if slices.Contains(words, w) {
				return true
			}
		}
	}
	return false
}

// eventText is every outlet's title and description for the events, the text a
// line written from them may draw on.
func eventText(cs ...cluster) string {
	var b strings.Builder
	for _, c := range cs {
		for _, s := range c.stories {
			b.WriteString(s.title + "\n")
			if s.desc != "" {
				b.WriteString(s.desc + "\n")
			}
		}
	}
	return b.String()
}
