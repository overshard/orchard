package skills

import (
	"context"
	neturl "net/url"
	"strings"
)

// A pasted wall of links is not a question, and five pages is already more of
// the context window than a summary can use well.
const maxGivenURLs = 5

// Page claims any question carrying a web address. The address is the
// instruction: the reader has already found the page and wants that page read,
// so planning a search off the words around it finds five other pages and
// answers from those instead.
//
// It gathers rather than answers, which is what makes it different from every
// other skill here. The addresses go back to the caller, which fetches them
// through the same cache and the same browser headers a search result goes
// through, and the answer is written and checked the usual way.
type Page struct{}

func (Page) Card() Card {
	return Card{
		Name: "page",
		Does: "reads the web page whose address the question carries, and answers from that page and nothing else.",
		Fires: []string{
			"summary of this https://example.com/post",
			"what does https://example.com/post say",
			"tl;dr https://example.com/post",
		},
	}
}

// Claims is the whole routing decision, and it does not need the model. A
// question either carries an address or it does not.
func (Page) Claims(question string) bool { return len(URLsIn(question)) > 0 }

func (Page) Run(ctx context.Context, question string, d Deps) (*Result, error) {
	urls := URLsIn(question)
	if len(urls) == 0 {
		return nil, nil
	}
	return &Result{Skill: "page", Shape: "summary", URLs: urls}, nil
}

// URLsIn pulls the web addresses out of a question, in the order they were
// written.
func URLsIn(question string) []string {
	var out []string
	seen := map[string]bool{}
	for _, f := range strings.FieldsFunc(question, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '<' || r == '>' || r == '"' || r == '\''
	}) {
		// A pasted address is often inside brackets, and one at the end of a
		// sentence takes the punctuation with it.
		f = strings.TrimLeft(f, "([{`*")
		if !strings.HasPrefix(f, "http://") && !strings.HasPrefix(f, "https://") {
			continue
		}
		f = strings.TrimRight(f, ".,;:!?`*")
		for unbalanced(f) {
			f = f[:len(f)-1]
		}
		u, err := neturl.Parse(f)
		if err != nil || !strings.Contains(u.Host, ".") || seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
		if len(out) == maxGivenURLs {
			break
		}
	}
	return out
}

// unbalanced says whether the address ends on a bracket it never opened, which
// means the bracket belongs to the sentence around it. A Wikipedia title opens
// one of its own, so the count is what decides rather than the last character.
func unbalanced(u string) bool {
	for _, p := range [][2]string{{"(", ")"}, {"[", "]"}, {"{", "}"}} {
		if strings.HasSuffix(u, p[1]) && strings.Count(u, p[0]) < strings.Count(u, p[1]) {
			return true
		}
	}
	return false
}
