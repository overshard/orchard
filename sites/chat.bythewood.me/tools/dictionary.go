package tools

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// Dictionary reads Wiktionary's own definitions. A word is not an article, so
// "define program" landed on a disambiguation page, got nothing, and was
// answered as a command the model made up along with what it printed.
var Dictionary = Tool{
	Name: "dictionary",
	Description: "What a word or a short phrase means, from Wiktionary, with its part of speech and an example. " +
		"Use it to define a word or say what a term means, and use wikipedia for a person, place, thing or event.",
	Schema: obj(map[string]any{"word": str("the word or phrase, like \"program\" or \"ad hoc\"")}, "word"),
	Run: func(ctx context.Context, d *Deps, a map[string]any) (any, error) {
		word := strings.Trim(strings.TrimSpace(argStr(a, "word")), "\"'“”‘’?.!")
		if word == "" {
			return nil, fmt.Errorf("word is required")
		}
		var raw map[string][]struct {
			PartOfSpeech string `json:"partOfSpeech"`
			Language     string `json:"language"`
			Definitions  []struct {
				Definition string   `json:"definition"`
				Examples   []string `json:"examples"`
			} `json:"definitions"`
		}
		var senses []map[string]any
		// Wiktionary keeps a capitalised word as its own entry, and "Wednesday"
		// has one where "wednesday" does not.
		forms, hops := dictionaryForms(word), 0
		for i := 0; i < len(forms); i++ {
			w := forms[i]
			u := "https://en.wiktionary.org/api/rest_v1/page/definition/" + url.PathEscape(strings.ReplaceAll(w, " ", "_"))
			raw = nil
			if err := getJSON(ctx, d, u, &raw); err != nil {
				continue
			}
			senses = englishSenses(raw["en"])
			// "luddite" is only "Alternative letter-case form of Luddite", which
			// left the model to explain the word from memory.
			if to := formOf(senses); to != "" && hops < 2 && !slices.Contains(forms[:i+1], to) {
				forms, i, hops = []string{to}, -1, hops+1
				continue
			}
			if len(senses) > 0 {
				word = w
				break
			}
		}
		if len(senses) == 0 {
			return map[string]any{"word": word, "found": false,
				"note": "Wiktionary has no English entry for that. If it is the name of something, try wikipedia."}, nil
		}
		return map[string]any{"word": word, "found": true, "source": "Wiktionary", "senses": senses}, nil
	},
}

func dictionaryForms(word string) []string {
	forms := []string{word}
	if lower := strings.ToLower(word); lower != word {
		forms = append(forms, lower)
	} else if word != "" {
		forms = append(forms, strings.ToUpper(word[:1])+word[1:])
	}
	return forms
}

var (
	markup = regexp.MustCompile(`<[^>]*>`)
	// A dated sense carries its own stylesheet, which otherwise reads as part of
	// the definition.
	styleBlock = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	// A sense that only points at another entry.
	pointer = regexp.MustCompile(`(?i)^(?:alternative (?:letter-case form|form|spelling|capitalization|case form)|obsolete (?:form|spelling)|archaic (?:form|spelling)|plural|misspelling) of (.+?)\.?$`)
)

// plainText drops the links and spans Wiktionary wraps every definition in.
func plainText(s string) string {
	s = styleBlock.ReplaceAllString(s, "")
	return strings.Join(strings.Fields(html.UnescapeString(markup.ReplaceAllString(s, ""))), " ")
}

// formOf is the entry every sense points at, or empty when any sense means
// something on its own.
func formOf(senses []map[string]any) string {
	to := ""
	for _, s := range senses {
		defs, _ := s["definitions"].([]string)
		for _, d := range defs {
			m := pointer.FindStringSubmatch(d)
			if m == nil || (to != "" && m[1] != to) {
				return ""
			}
			to = m[1]
		}
	}
	return to
}

// englishSenses keeps a few definitions per part of speech. A common word has
// dozens and the first handful are the ones anybody means.
func englishSenses(entries []struct {
	PartOfSpeech string `json:"partOfSpeech"`
	Language     string `json:"language"`
	Definitions  []struct {
		Definition string   `json:"definition"`
		Examples   []string `json:"examples"`
	} `json:"definitions"`
}) []map[string]any {
	var out []map[string]any
	for _, e := range entries {
		if len(out) >= 3 {
			break
		}
		var defs []string
		example := ""
		for _, d := range e.Definitions {
			t := plainText(d.Definition)
			if t == "" {
				continue
			}
			if len(defs) < 4 {
				defs = append(defs, t)
			}
			if example == "" && len(d.Examples) > 0 {
				example = plainText(d.Examples[0])
			}
		}
		if len(defs) == 0 {
			continue
		}
		s := map[string]any{"part_of_speech": strings.ToLower(e.PartOfSpeech), "definitions": defs}
		if example != "" {
			s["example"] = example
		}
		out = append(out, s)
	}
	return out
}
