package main

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// Fix is every crawl finding of one kind, so a header bug on every page reads
// as one thing to fix instead of a row per page.
type Fix struct {
	Label    string
	Why      string
	Type     string
	Severity string
	Pages    int
	Items    []Insight
}

type fixText struct{ label, why string }

var digits = regexp.MustCompile(`\d+`)

// fixTexts keys on the finding with its numbers taken out.
var fixTexts = map[string]fixText{
	"Page has no title":                                     {"Pages with no title", "Search results and browser tabs fall back to the URL."},
	"Title length is # chars (recommended #-#)":             {"Title outside 30 to 60 characters", "Search results cut long titles, and short ones say too little."},
	"Duplicate title":                                       {"Pages sharing a title", "Search results can't tell them apart."},
	"Page has no meta description":                          {"Pages with no description", "Search results make one up from the page text."},
	"Description length is # chars (recommended #-#)":       {"Description outside 70 to 160 characters", "Search results cut it off past about 160."},
	"Duplicate meta description":                            {"Pages sharing a description", "Search results show the same blurb for each."},
	"Page has no h#":                                        {"Pages with no main heading", "Screen readers and search engines look for one h1."},
	"Page has # h# tags (expected #)":                       {"More than one main heading", "A page reads best with a single h1."},
	"H# length is # chars (recommended #-#)":                {"Main heading outside 20 to 70 characters", "Usually short tag or index page headings, often fine to leave."},
	"Duplicate h#":                                          {"Pages sharing a main heading", "Search engines can't tell them apart."},
	"Heading hierarchy skips from h# to h#":                 {"Headings skip a level", "An h2 followed by an h4 confuses screen readers."},
	"Page has no canonical URL":                             {"No canonical URL", "Search engines guess which copy of the page is the real one."},
	"Canonical URL points off-domain":                       {"Canonical URL points at another site", "Search engines credit that site instead."},
	"Canonical URL returns #":                               {"Canonical URL is broken", "It points at a page that doesn't answer 200."},
	"Page has noindex in meta robots tag":                   {"Pages hidden from search", "A noindex tag keeps them out of results."},
	"HTML lang attribute missing":                           {"No page language set", "Screen readers can't pick the right voice."},
	"Viewport meta tag missing (mobile)":                    {"No viewport tag", "Phones render the page zoomed out."},
	"Twitter card meta tag missing":                         {"No Twitter card tags", "Link previews on X fall back to guesses."},
	"Favicon link missing":                                  {"No favicon link", "Tabs and bookmarks show a blank icon."},
	"JSON-LD structured data failed to parse":               {"Broken structured data", "Search engines skip JSON-LD that doesn't parse."},
	"Internal link has rel=nofollow":                        {"Internal links marked nofollow", "Search engines won't follow them to your own pages."},
	"robots.txt missing":                                    {"No robots.txt", "Crawlers have no rules to read."},
	"sitemap.xml missing or empty":                          {"No sitemap", "Search engines have to find every page on their own."},
	"robots.txt does not reference a sitemap":               {"robots.txt doesn't point at the sitemap", "Crawlers that start there won't find it."},
	"URL listed in sitemap returns #":                       {"Sitemap lists a broken URL", "Search engines are sent to a page that fails."},
	"Page not listed in sitemap":                            {"Pages missing from the sitemap", "Search engines may not find them."},
	"Redirect chain has # hops":                             {"Links that redirect more than once", "Each hop adds a round trip before the page loads."},
	"# image(s) missing alt attribute":                      {"Images with no alt text", "Screen readers have nothing to say for them."},
	"# link(s) have no visible text":                        {"Links with no visible text", "Icon only links need an aria-label so screen readers can name them."},
	"# form input(s) without associated label":              {"Form fields with no label", "Screen readers can't tell what the field is for."},
	"Thin content (# words)":                                {"Very little text on the page", "Search engines can treat thin pages as low value."},
	"Page has duplicate visible content with another page":  {"Pages with the same content", "Search engines pick one and drop the other."},
	"Slow response (# ms)":                                  {"Slow pages", "The server took a long time to answer these."},
	"Response not compressed (no Content-Encoding header)":  {"Responses not compressed", "Every visitor downloads more than they need to."},
	"Oversized page (# KB)":                                 {"Large pages", "The HTML alone is heavy before any images load."},
	"Mixed content: # http:// resource(s) on https:// page": {"Insecure http:// files on an https:// page", "Browsers block or warn about them."},
}

// fixKey drops the numbers from a finding, and the varying tail from the two
// that carry a status or a list instead of a number.
func fixKey(issue string) string {
	for _, p := range []string{"Broken internal link", "Broken external link", "Open Graph tags missing"} {
		if strings.HasPrefix(issue, p) {
			return p
		}
	}
	return digits.ReplaceAllString(issue, "#")
}

func fixTextFor(key string) fixText {
	switch key {
	case "Broken internal link":
		return fixText{"Broken links within the site", "A link points at one of your own pages that fails."}
	case "Broken external link":
		return fixText{"Broken links to other sites", "The page links somewhere that answered with an error."}
	case "Open Graph tags missing":
		return fixText{"Open Graph tags missing", "Link previews in chat apps and social sites fall back to guesses."}
	}
	if t, ok := fixTexts[key]; ok {
		return t
	}
	return fixText{strings.ReplaceAll(key, "#", "N"), ""}
}

func sevRank(sev string) int {
	switch sev {
	case sevError:
		return 0
	case sevWarn:
		return 1
	}
	return 2
}

// groupFixes ranks the kinds worst first, then by how many pages each touches.
func groupFixes(insights []Insight) []Fix {
	byKey := map[string]*Fix{}
	var order []string
	pages := map[string]map[string]bool{}
	for _, in := range insights {
		key := in.Type + "|" + fixKey(in.Issue)
		f, ok := byKey[key]
		if !ok {
			t := fixTextFor(fixKey(in.Issue))
			f = &Fix{Label: t.label, Why: t.why, Type: in.Type, Severity: in.Severity}
			byKey[key] = f
			pages[key] = map[string]bool{}
			order = append(order, key)
		}
		if sevRank(in.Severity) < sevRank(f.Severity) {
			f.Severity = in.Severity
		}
		f.Items = append(f.Items, in)
		pages[key][in.URL] = true
	}
	out := make([]Fix, 0, len(order))
	for _, k := range order {
		f := byKey[k]
		f.Pages = len(pages[k])
		out = append(out, *f)
	}
	sort.SliceStable(out, func(a, b int) bool {
		if sevRank(out[a].Severity) != sevRank(out[b].Severity) {
			return sevRank(out[a].Severity) < sevRank(out[b].Severity)
		}
		return out[a].Pages > out[b].Pages
	})
	return out
}

// FixType is one category's count, for the filter over the fix list.
type FixType struct {
	Type  string
	Title string
	Count int
}

func fixTypes(fixes []Fix) []FixType {
	counts := map[string]int{}
	for _, f := range fixes {
		counts[f.Type] += len(f.Items)
	}
	var out []FixType
	for t, n := range counts {
		out = append(out, FixType{t, insightTitle(t), n})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Type < out[b].Type })
	return out
}

var secFailText = map[string]string{
	"Served over HTTPS":        "isn't served over HTTPS",
	"MIME types set":           "leaves out the content type",
	"Content sniff protection": "is missing nosniff",
	"Clickjack protection":     "is missing frame protection",
	"Server version hidden":    "names its server software in the Server header",
	"HSTS":                     "doesn't send HSTS",
	"HSTS preload":             "isn't set up for HSTS preload",
}

// Note is one line of the list page's worth a look panel.
type Note struct {
	Tag   string
	Class string
	Head  string
	Body  string
	Href  string
}

// notes reads the whole list for what stands out, things true of every site
// first since one fix covers them all.
func notes(views []*PropertyView) []Note {
	var out []Note
	if len(views) == 0 {
		return out
	}
	for _, v := range views {
		if v.CurrentStatus != 200 {
			out = append(out, Note{"down", "bad", v.Name + " is down.", fmt.Sprintf("The last check answered %d.", v.CurrentStatus), "/" + v.ID})
		}
	}

	fails := map[string][]string{}
	for _, v := range views {
		for _, c := range secChecks(v) {
			if !c.OK {
				fails[c.Name] = append(fails[c.Name], v.Name)
			}
		}
	}
	names := make([]string, 0, len(fails))
	for k := range fails {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, name := range names {
		sites := fails[name]
		if len(sites) == len(views) && len(views) > 1 {
			out = append(out, Note{"every site", "warn", "Every site " + secFailText[name] + ".",
				"The same header on all of them, so it's one change in the shared Caddy config.", ""})
		} else {
			out = append(out, Note{"headers", "warn", fmt.Sprintf("%s: %s check fails.", joinNames(sites), strings.ToLower(name)), "", ""})
		}
	}

	if days := sharedBadDays(views); len(days) > 0 {
		out = append(out, Note{"every site", "", joinNames(days) + " dips hit nearly every site.",
			"The same days across the board point at the box or the tunnel, not one site.", ""})
	}

	var ups []float64
	for _, v := range views {
		if v.H != nil && v.H.Up30 != nil {
			ups = append(ups, *v.H.Up30)
		}
	}
	if len(ups) > 2 {
		slices.Sort(ups)
		med := ups[len(ups)/2]
		for _, v := range views {
			if v.H != nil && v.H.Up30 != nil && *v.H.Up30 < med-0.3 {
				out = append(out, Note{"uptime", "warn", fmt.Sprintf("%s is lowest at %.2f%%.", v.Name, *v.H.Up30),
					fmt.Sprintf("%.2f points under the rest, so it had trouble of its own.", med-*v.H.Up30), "/" + v.ID})
			}
		}
	}

	for _, v := range views {
		if v.CertDays != nil && *v.CertDays < 14 {
			out = append(out, Note{"cert", "warn", fmt.Sprintf("%s's certificate expires in %d days.", v.Name, *v.CertDays), "", "/" + v.ID})
		}
		if v.AvgAuditScore != nil && *v.AvgAuditScore < 90 {
			out = append(out, Note{"health", "warn", fmt.Sprintf("%s scored %d on its audit.", v.Name, *v.AvgAuditScore), "", "/" + v.ID + "#health"})
		}
	}

	crawl := slices.Clone(views)
	sort.SliceStable(crawl, func(a, b int) bool { return len(crawl[a].CrawlerInsights) > len(crawl[b].CrawlerInsights) })
	for _, v := range crawl {
		if len(v.CrawlerInsights) < 10 {
			break
		}
		fixes := groupFixes(v.CrawlerInsights)
		out = append(out, Note{"crawl", "warn", fmt.Sprintf("%s has %d crawl issues of %d kinds.", v.Name, len(v.CrawlerInsights), len(fixes)),
			fmt.Sprintf("Biggest is %s, on %d %s.", strings.ToLower(fixes[0].Label), fixes[0].Pages, pagesWord(fixes[0].Pages, "page", "pages")), "/" + v.ID + "#crawl"})
	}
	return out
}

// sharedBadDays are the days at least four in five sites with data went red
// or amber together.
func sharedBadDays(views []*PropertyView) []string {
	type tally struct {
		label     string
		bad, seen int
	}
	byDay := map[string]*tally{}
	var order []string
	for _, v := range views {
		if v.H == nil {
			continue
		}
		for _, d := range v.H.Ticks30() {
			if d.Kind == "none" {
				continue
			}
			t, ok := byDay[d.Day]
			if !ok {
				t = &tally{label: d.Label}
				byDay[d.Day] = t
				order = append(order, d.Day)
			}
			t.seen++
			if d.Kind == "bad" || d.Kind == "warn" {
				t.bad++
			}
		}
	}
	sort.Strings(order)
	var out []string
	for _, day := range order {
		t := byDay[day]
		if t.seen >= 3 && t.bad*5 >= t.seen*4 {
			out = append(out, shortDay(t.label))
		}
	}
	return out
}

// shortDay keeps the month and day of a day label like "Thu Nov 19".
func shortDay(label string) string {
	if f := strings.Fields(label); len(f) == 3 {
		return f[1] + " " + f[2]
	}
	return label
}

func joinNames(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	case 2:
		return names[0] + " and " + names[1]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

func pagesWord(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// secFixes says what to do about a failing header, for the checks with one
// clear answer.
var secFixes = map[string]string{
	"Served over HTTPS":        "Serve the site over https:// and redirect plain http to it.",
	"MIME types set":           "Send a Content-Type on every response so browsers don't guess.",
	"Content sniff protection": "Add X-Content-Type-Options: nosniff.",
	"Clickjack protection":     "Add X-Frame-Options or a frame-ancestors CSP so other sites can't frame it.",
	"Server version hidden":    "The Server header tells a scanner what to try. Drop it in Caddy with header -Server.",
	"HSTS":                     "Add Strict-Transport-Security so browsers never try plain http.",
	"HSTS preload":             "Add preload and includeSubDomains to the HSTS header, then submit the domain to hstspreload.org.",
}

// HealthProblems counts the failing headers and the audit's findings.
func (v *PropertyView) HealthProblems() int {
	n := len(secChecks(v)) - secPass(v)
	if v.AuditDetails != nil {
		n += len(v.AuditDetails.Findings)
	}
	return n
}
