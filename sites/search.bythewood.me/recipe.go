package main

import (
	"encoding/json"
	"fmt"
	stdhtml "html"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// A recipe page is the one case where the useful content is published as data
// and then thrown away by reading the prose.
//
// Asking a model to pull ingredients out of an article gives a list with no
// quantities, because the quantities live in a table the markdown conversion
// flattens and the surrounding prose says "add the eggs" rather than "add
// eight eggs". Nearly every recipe site publishes a schema.org Recipe in a
// script tag with `recipeIngredient` as exact strings and `recipeInstructions`
// in order, so the numbers are right there.
//
// The block this renders is prepended to the page markdown, which puts it in
// the first passage and makes it the piece most likely to be selected.

type recipeData struct {
	Name        string
	Yield       string
	PrepTime    string
	CookTime    string
	TotalTime   string
	Ingredients []string
	Steps       []string
}

func (r recipeData) usable() bool {
	return len(r.Ingredients) >= 3 && len(r.Steps) >= 2
}

// Markdown renders the block that goes at the top of the page.
func (r recipeData) Markdown() string {
	var b strings.Builder
	b.WriteString(recipeHeading + "\n")
	if r.Name != "" {
		fmt.Fprintf(&b, "%s\n\n", r.Name)
	}
	for _, f := range []struct{ label, v string }{
		{"Makes", r.Yield}, {"Prep time", r.PrepTime},
		{"Cook time", r.CookTime}, {"Total time", r.TotalTime},
	} {
		if f.v != "" {
			fmt.Fprintf(&b, "%s: %s\n", f.label, f.v)
		}
	}
	b.WriteString("\n### Ingredients\n\n")
	for _, i := range r.Ingredients {
		fmt.Fprintf(&b, "- %s\n", i)
	}
	b.WriteString("\n### Method\n\n")
	for i, s := range r.Steps {
		fmt.Fprintf(&b, "%d. %s\n", i+1, s)
	}
	return b.String()
}

// recipeFromJSONLD finds a schema.org Recipe anywhere in the page's structured
// data. The type can be a string or a list, and the object is often buried in
// an @graph, so this walks rather than looking in a fixed place.
func recipeFromJSONLD(root *html.Node) (recipeData, bool) {
	var out recipeData
	found := false

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found {
			return
		}
		if n.Type == html.ElementNode && n.DataAtom == atom.Script {
			for _, a := range n.Attr {
				if a.Key == "type" && strings.Contains(a.Val, "ld+json") && n.FirstChild != nil {
					var v any
					if json.Unmarshal([]byte(n.FirstChild.Data), &v) == nil {
						if r, ok := digRecipe(v); ok {
							out, found = r, true
							return
						}
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return out, found && out.usable()
}

func digRecipe(v any) (recipeData, bool) {
	switch x := v.(type) {
	case map[string]any:
		if isType(x["@type"], "Recipe") {
			return buildRecipe(x), true
		}
		for _, sub := range x {
			if r, ok := digRecipe(sub); ok {
				return r, true
			}
		}
	case []any:
		for _, sub := range x {
			if r, ok := digRecipe(sub); ok {
				return r, true
			}
		}
	}
	return recipeData{}, false
}

func isType(v any, want string) bool {
	switch t := v.(type) {
	case string:
		return strings.EqualFold(t, want)
	case []any:
		for _, s := range t {
			if str, ok := s.(string); ok && strings.EqualFold(str, want) {
				return true
			}
		}
	}
	return false
}

func buildRecipe(m map[string]any) recipeData {
	r := recipeData{
		Name:      str(m["name"]),
		Yield:     firstString(m["recipeYield"]),
		PrepTime:  humanDuration(str(m["prepTime"])),
		CookTime:  humanDuration(str(m["cookTime"])),
		TotalTime: humanDuration(str(m["totalTime"])),
	}
	for _, i := range asList(m["recipeIngredient"]) {
		if s := strings.TrimSpace(str(i)); s != "" {
			r.Ingredients = append(r.Ingredients, s)
		}
	}
	r.Steps = flattenSteps(m["recipeInstructions"])
	return r
}

// flattenSteps handles the three shapes instructions arrive in: a list of
// strings, a list of HowToStep objects, and a list of HowToSection objects each
// holding its own list.
func flattenSteps(v any) []string {
	var out []string
	switch x := v.(type) {
	case string:
		for _, line := range strings.Split(x, "\n") {
			if s := strings.TrimSpace(line); s != "" {
				out = append(out, s)
			}
		}
	case []any:
		for _, item := range x {
			out = append(out, flattenSteps(item)...)
		}
	case map[string]any:
		if sub, ok := x["itemListElement"]; ok {
			return flattenSteps(sub)
		}
		if s := strings.TrimSpace(str(x["text"])); s != "" {
			out = append(out, s)
		} else if s := strings.TrimSpace(str(x["name"])); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func str(v any) string {
	s, _ := v.(string)
	// These arrive HTML escaped, so "1 &amp; 1/2 pounds bacon" reaches the
	// answer verbatim unless it is unescaped here.
	return strings.TrimSpace(stdhtml.UnescapeString(s))
}

func firstString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return fmt.Sprintf("%g", x)
	case []any:
		for _, s := range x {
			if got := firstString(s); got != "" {
				return got
			}
		}
	}
	return ""
}

func asList(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	case string:
		return []any{x}
	}
	return nil
}

// humanDuration turns ISO 8601 PT30M into words, since nobody reads PT1H15M.
func humanDuration(iso string) string {
	if !strings.HasPrefix(iso, "PT") {
		return ""
	}
	rest := iso[2:]
	var hours, mins string
	if i := strings.Index(rest, "H"); i >= 0 {
		hours, rest = rest[:i], rest[i+1:]
	}
	if i := strings.Index(rest, "M"); i >= 0 {
		mins = rest[:i]
	}
	switch {
	case hours != "" && mins != "" && mins != "0":
		return hours + "h " + mins + "m"
	case hours != "":
		return hours + "h"
	case mins != "":
		return mins + " minutes"
	}
	return ""
}

// recipeHeading is what Markdown writes first, and checking for it is how a
// cached page is recognised as carrying a real recipe without re-parsing it.
const recipeHeading = "## Recipe\n"

func hasStructuredRecipe(p *Page) bool {
	return p != nil && strings.HasPrefix(p.Markdown, recipeHeading)
}
