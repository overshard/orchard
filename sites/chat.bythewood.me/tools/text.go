package tools

import (
	"html"
	"regexp"
	"strings"
)

var (
	tagStrip = regexp.MustCompile(`(?is)<[^>]+>`)
	// RE2 has no backreferences, so a paired tag needs its own expression
	// instead of one alternation closing on \1.
	blockTags = func() []*regexp.Regexp {
		var out []*regexp.Regexp
		for _, t := range []string{"script", "style", "nav", "header", "footer", "aside", "form", "svg", "noscript", "template"} {
			out = append(out, regexp.MustCompile(`(?is)<`+t+`[^>]*>.*?</`+t+`\s*>`))
		}
		return out
	}()
	articleRe = []*regexp.Regexp{
		regexp.MustCompile(`(?is)<article[^>]*>(.*?)</article\s*>`),
		regexp.MustCompile(`(?is)<main[^>]*>(.*?)</main\s*>`),
	}
	spaces   = regexp.MustCompile(`[ \t]+`)
	blankRun = regexp.MustCompile(`\n{3,}`)
)

// Text pulls readable prose out of an HTML page, and keeps the byline and the
// date that a readability port throws away.
func Text(h string) string {
	for _, re := range blockTags {
		h = re.ReplaceAllString(h, " ")
	}
	// The body of an <article> or <main> is the page, and everything around
	// it is furniture. Falling through with the whole document is fine when
	// neither is present.
	for _, re := range articleRe {
		if m := re.FindStringSubmatch(h); m != nil && len(m[1]) > 200 {
			h = m[1]
			break
		}
	}
	h = regexp.MustCompile(`(?i)</(p|div|li|h[1-6]|tr|section)>`).ReplaceAllString(h, "\n")
	h = regexp.MustCompile(`(?i)<br\s*/?>`).ReplaceAllString(h, "\n")
	h = tagStrip.ReplaceAllString(h, " ")
	h = html.UnescapeString(h)
	h = spaces.ReplaceAllString(h, " ")
	var keep []string
	for _, line := range strings.Split(h, "\n") {
		if l := strings.TrimSpace(line); l != "" {
			keep = append(keep, l)
		}
	}
	return blankRun.ReplaceAllString(strings.Join(keep, "\n"), "\n\n")
}
