package main

import (
	"fmt"
	"math"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

type auditRun struct {
	doc          fetched
	base         *url.URL
	page         *pageInfo
	subs         []fetched
	robotsStatus int
}

// check is one pass or fail. Weight is how much it moves its category, and a
// failed check with a weight of three or more is an error.
type check struct {
	cat    string
	title  string
	weight int
	pass   bool
	detail string
}

var (
	maxAgeRe   = regexp.MustCompile(`max-age=(\d+)`)
	maxScaleRe = regexp.MustCompile(`maximum-scale=([\d.]+)`)
)

const (
	catPerf = "Performance"
	catA11y = "Accessibility"
	catBest = "Best practices"
	catSEO  = "SEO"
)

func (a *auditRun) score() (*Scores, *Details) {
	checks := append(append(a.a11y(), a.best()...), a.seo()...)
	metrics := a.metrics()

	d := &Details{Metrics: metrics, Findings: []Finding{}}
	got, total := map[string]int{}, map[string]int{}
	for _, c := range checks {
		d.Checked++
		total[c.cat] += c.weight
		if c.pass {
			d.Passed++
			got[c.cat] += c.weight
			continue
		}
		sev := sevWarn
		if c.weight >= 3 {
			sev = sevError
		}
		d.Findings = append(d.Findings, Finding{c.cat, c.title, c.detail, sev})
	}
	d.Findings = append(d.Findings, a.perfFindings()...)
	sort.SliceStable(d.Findings, func(i, j int) bool {
		return d.Findings[i].Severity == sevError && d.Findings[j].Severity != sevError
	})

	pct := func(cat string) int64 {
		if total[cat] == 0 {
			return 100
		}
		return int64(math.Round(100 * float64(got[cat]) / float64(total[cat])))
	}
	var perf, weights float64
	for _, m := range metrics {
		weights += m.Weight
		if m.Score != nil {
			perf += *m.Score * m.Weight
		}
	}
	s := &Scores{
		Performance:   int64(math.Round(100 * perf / math.Max(weights, 1))),
		Accessibility: pct(catA11y),
		BestPractices: pct(catBest),
		SEO:           pct(catSEO),
	}
	return s, d
}

// linear is 1 at or under good, 0 at or past poor, and a straight line between.
func linear(v, good, poor float64) *float64 {
	s := 1 - (v-good)/(poor-good)
	s = math.Round(math.Min(1, math.Max(0, s))*100) / 100
	return &s
}

func (a *auditRun) metrics() []Metric {
	total := a.doc.WireBytes
	requests := 1
	blocking := 0
	for _, f := range a.subs {
		if f.Err == "" {
			total += f.WireBytes
			requests++
		}
	}
	for _, r := range append(append([]pageRef{}, a.page.Stylesheets...), a.page.Scripts...) {
		if r.Blocking {
			blocking++
		}
	}
	return []Metric{
		{"page-weight", "Weight", "Everything the page loads, as sent over the wire", kb(total),
			linear(float64(total), 500<<10, 3<<20), 30},
		{"render-blocking", "Blocking", "Stylesheets and scripts that hold up the first paint", strconv.Itoa(blocking),
			linear(float64(blocking), 2, 8), 25},
		{"ttfb", "TTFB", "Time to first byte, measured from this server", fmt.Sprintf("%d ms", a.doc.TTFBMS),
			linear(float64(a.doc.TTFBMS), 600, 1800), 15},
		{"requests", "Requests", "The page and everything it loads", strconv.Itoa(requests),
			linear(float64(requests), 25, 100), 15},
		{"dom-size", "DOM", "Elements in the HTML as served", strconv.Itoa(a.page.Elements),
			linear(float64(a.page.Elements), 800, 3000), 15},
	}
}

func list(items []string, max int) string {
	if len(items) > max {
		return strings.Join(items[:max], ", ") + fmt.Sprintf(" and %d more", len(items)-max)
	}
	return strings.Join(items, ", ")
}

func (a *auditRun) a11y() []check {
	p := a.page
	var noAlt, emptyLinks []string
	for _, img := range p.Images {
		if !img.HasAlt && !img.Decor {
			noAlt = append(noAlt, img.Snippet)
		}
	}
	for _, l := range p.Links {
		if l.Name == "" {
			emptyLinks = append(emptyLinks, a.short(l.URL))
		}
	}
	scalable := true
	vp := strings.ReplaceAll(strings.ToLower(p.Viewport), " ", "")
	if strings.Contains(vp, "user-scalable=no") || strings.Contains(vp, "user-scalable=0") {
		scalable = false
	}
	if m := maxScaleRe.FindStringSubmatch(vp); m != nil {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil && v < 2 {
			scalable = false
		}
	}
	return []check{
		{catA11y, "Page has no lang attribute", 3, p.Lang != "", "Screen readers pick a voice from <html lang>."},
		{catA11y, "Page has no title", 2, p.Title != "", ""},
		{catA11y, "Images without alt text", 3, len(noAlt) == 0, list(noAlt, 3)},
		{catA11y, "Links with no text", 3, len(emptyLinks) == 0, list(emptyLinks, 3)},
		{catA11y, "Buttons with no name", 3, len(p.Unnamed) == 0, list(p.Unnamed, 3)},
		{catA11y, "Form fields without a label", 3, len(p.Unlabelled) == 0, list(p.Unlabelled, 3)},
		{catA11y, "Zooming is blocked", 2, scalable, p.Viewport},
		{catA11y, "Frames without a title", 1, len(p.Frames) == 0, list(p.Frames, 3)},
		{catA11y, "Unknown ARIA roles", 1, len(p.BadRoles) == 0, list(p.BadRoles, 3)},
		{catA11y, "Positive tabindex changes the tab order", 1, p.PosTabindex == 0, count(p.PosTabindex, "element", "elements")},
		{catA11y, "Duplicate ids", 1, len(p.DupIDs) == 0, list(p.DupIDs, 3)},
		{catA11y, "Headings skip a level", 1, p.HeadingJump == "", p.HeadingJump},
		{catA11y, "No main landmark", 1, p.HasMain, "A <main> lets a screen reader jump past the header."},
		{catA11y, "The body is hidden from screen readers", 3, !p.BodyHidden, ""},
		{catA11y, "Page refreshes itself", 1, !p.Refresh, "meta http-equiv=refresh"},
	}
}

func (a *auditRun) best() []check {
	h := a.doc.Headers
	hsts := strings.ToLower(h.Get("Strict-Transport-Security"))
	hstsOK := false
	if m := maxAgeRe.FindStringSubmatch(hsts); m != nil {
		n, _ := strconv.Atoi(m[1])
		hstsOK = n >= 15552000
	}
	csp := strings.ToLower(h.Get("Content-Security-Policy"))
	framed := strings.Contains(csp, "frame-ancestors") || h.Get("X-Frame-Options") != ""

	var weakCookies []string
	for _, c := range h.Values("Set-Cookie") {
		lc := strings.ToLower(c)
		if !strings.Contains(lc, "secure") || !strings.Contains(lc, "samesite") {
			weakCookies = append(weakCookies, strings.SplitN(c, "=", 2)[0])
		}
	}

	var failed []string
	for _, f := range a.subs {
		if f.Err == "" && f.Status >= 400 {
			failed = append(failed, fmt.Sprintf("%d %s", f.Status, a.short(f.URL)))
		}
	}

	return []check{
		{catBest, "Not served over HTTPS", 5, a.base.Scheme == "https", ""},
		{catBest, "No HSTS, or a max-age under six months", 3, hstsOK, hsts},
		{catBest, "No Content-Security-Policy", 3, csp != "", ""},
		{catBest, "CSP allows inline script", 1, csp == "" || !cspAllowsInline(csp), "script-src carries 'unsafe-inline'"},
		{catBest, "Can be framed by any site", 2, framed, "frame-ancestors or X-Frame-Options"},
		{catBest, "No X-Content-Type-Options: nosniff", 1, strings.EqualFold(h.Get("X-Content-Type-Options"), "nosniff"), ""},
		{catBest, "No Referrer-Policy", 1, h.Get("Referrer-Policy") != "", ""},
		{catBest, "Cookies without Secure or SameSite", 2, len(weakCookies) == 0, list(weakCookies, 3)},
		{catBest, "Mixed content", 3, len(a.page.Mixed) == 0, list(a.page.Mixed, 3)},
		{catBest, "Missing or old doctype", 1, a.page.Doctype, "<!doctype html>"},
		{catBest, "No charset in the first 1KB", 1, a.page.Charset || strings.Contains(a.doc.ContentType, "charset"), ""},
		{catBest, "Requests that fail", 2, len(failed) == 0, list(failed, 3)},
		{catBest, "Outdated JavaScript libraries", 3, len(a.outdatedLibraries()) == 0, list(a.outdatedLibraries(), 3)},
	}
}

func cspAllowsInline(csp string) bool {
	for _, d := range strings.Split(csp, ";") {
		f := strings.Fields(d)
		if len(f) > 0 && (f[0] == "script-src" || f[0] == "default-src") && strings.Contains(d, "'unsafe-inline'") &&
			!strings.Contains(d, "'nonce-") && !strings.Contains(d, "'sha") && !strings.Contains(d, "'strict-dynamic'") {
			return true
		}
	}
	return false
}

func (a *auditRun) seo() []check {
	p := a.page
	h := a.doc.Headers
	noindex := strings.Contains(p.RobotsMeta, "noindex") || strings.Contains(strings.ToLower(h.Get("X-Robots-Tag")), "noindex")
	canonOK := true
	if p.Canonical != "" {
		u, err := url.Parse(p.Canonical)
		canonOK = err == nil && u.IsAbs() && !thirdParty(a.base.Host, u.Host)
	}
	var vague []string
	for _, l := range p.Links {
		switch strings.ToLower(strings.Trim(l.Name, " .!>")) {
		case "click here", "here", "read more", "more", "learn more", "link", "this":
			vague = append(vague, `"`+l.Name+`"`)
		}
	}
	return []check{
		{catSEO, "Blocked from indexing", 5, !noindex, "noindex in robots meta or X-Robots-Tag"},
		{catSEO, "No title", 3, p.Title != "", ""},
		{catSEO, "No meta description", 2, strings.TrimSpace(p.Description) != "", ""},
		{catSEO, "No viewport meta", 2, p.Viewport != "", ""},
		{catSEO, "Canonical points off the site", 2, canonOK, p.Canonical},
		{catSEO, "No lang attribute", 1, p.Lang != "", ""},
		{catSEO, "robots.txt doesn't answer 200", 1, a.robotsStatus == 200, fmt.Sprintf("answered %d", a.robotsStatus)},
		{catSEO, "Links that say nothing about where they go", 1, len(vague) == 0, list(vague, 3)},
	}
}

// perfFindings are the things behind the metrics, listed but not scored, since
// the metrics already count what they cost.
func (a *auditRun) perfFindings() []Finding {
	var out []Finding
	add := func(title, detail string) {
		out = append(out, Finding{catPerf, title, detail, sevWarn})
	}
	var uncompressed, shortCache, bigImages, legacy, blocking []string
	var js int64
	thirdHosts := map[string]bool{}
	all := append([]fetched{a.doc}, a.subs...)
	for _, f := range all {
		if f.Err != "" || f.Status != 200 {
			continue
		}
		u, err := url.Parse(f.URL)
		if err != nil {
			continue
		}
		name := snippet(u.Path)
		if thirdParty(a.base.Host, u.Host) {
			thirdHosts[u.Host] = true
			name = u.Host + name
		}
		text := f.Kind == "document" || f.Kind == "stylesheet" || f.Kind == "script"
		if text && f.WireBytes > 1400 && (f.Encoding == "" || f.Encoding == "identity") {
			uncompressed = append(uncompressed, name)
		}
		// Only this host's own files, since a shared script like an analytics tag
		// keeps a short cache on a stable URL so it can change.
		if f.Kind != "document" && u.Host == a.base.Host && cacheSeconds(f.CacheControl) < 7*24*3600 {
			shortCache = append(shortCache, name)
		}
		if f.Kind == "script" {
			js += f.WireBytes
		}
		if f.Kind == "image" {
			if f.WireBytes > 250<<10 {
				bigImages = append(bigImages, name+" "+kb(f.WireBytes))
			}
			if (strings.Contains(f.ContentType, "jpeg") || strings.Contains(f.ContentType, "png")) && f.WireBytes > 100<<10 {
				legacy = append(legacy, name)
			}
		}
	}
	for _, r := range append(append([]pageRef{}, a.page.Stylesheets...), a.page.Scripts...) {
		if r.Blocking {
			if u, err := url.Parse(r.URL); err == nil {
				blocking = append(blocking, snippet(u.Path))
			}
		}
	}
	var unsized, noDisplay []string
	for _, img := range a.page.Images {
		if !img.Sized {
			unsized = append(unsized, img.Snippet)
		}
	}
	for _, f := range a.subs {
		if f.Kind != "stylesheet" || f.Body == nil {
			continue
		}
		u, _ := url.Parse(f.URL)
		for _, ff := range parseFontFaces(string(f.Body), u) {
			if !ff.Display && !slices.Contains(noDisplay, ff.Family) {
				noDisplay = append(noDisplay, ff.Family)
			}
		}
	}

	if len(blocking) > 2 {
		add("Render-blocking stylesheets and scripts", list(blocking, 4))
	}
	if len(uncompressed) > 0 {
		add("Text sent without compression", list(uncompressed, 3))
	}
	if len(shortCache) > 0 {
		add("Static files cached for under a week", list(shortCache, 3))
	}
	if len(bigImages) > 0 {
		add("Images over 250 KB", list(bigImages, 3))
	}
	if len(legacy) > 0 {
		add("Large JPEG or PNG that could be WebP or AVIF", list(legacy, 3))
	}
	if len(unsized) > 0 {
		add("Images without width and height, which shift the layout", list(unsized, 3))
	}
	if len(noDisplay) > 0 {
		add("Fonts without font-display, so text stays invisible while they load", list(noDisplay, 3))
	}
	if js > 300<<10 {
		add("Heavy JavaScript", kb(js)+" of script")
	}
	if a.doc.WireBytes > 100<<10 {
		add("Large HTML", kb(a.doc.WireBytes))
	}
	if len(thirdHosts) > 4 {
		add("Many third party hosts", list(sortedKeys(thirdHosts), 5))
	}
	return out
}

// cacheSeconds reads max-age out of Cache-Control, and immutable counts as a year.
func cacheSeconds(cc string) int {
	if strings.Contains(cc, "immutable") {
		return 365 * 24 * 3600
	}
	if strings.Contains(cc, "no-store") || strings.Contains(cc, "no-cache") {
		return 0
	}
	if m := maxAgeRe.FindStringSubmatch(cc); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

// libraryRule flags a library by the version in its file name or its banner
// comment. below maps a major version to the first release without the known
// problems, and an empty value means the whole major is past its end of life.
type libraryRule struct {
	name  string
	file  *regexp.Regexp
	body  *regexp.Regexp
	below map[int]string
}

var libraryRules = []libraryRule{
	{"jQuery", regexp.MustCompile(`jquery[.-]v?(\d+\.\d+\.\d+)`), regexp.MustCompile(`jQuery (?:JavaScript Library )?v(\d+\.\d+\.\d+)`),
		map[int]string{1: "", 2: "", 3: "3.5.0"}},
	{"Bootstrap", regexp.MustCompile(`bootstrap[.-]v?(\d+\.\d+\.\d+)`), regexp.MustCompile(`Bootstrap v(\d+\.\d+\.\d+)`),
		map[int]string{2: "", 3: "3.4.1", 4: "4.3.1"}},
	{"AngularJS", regexp.MustCompile(`angular[.-]v?(1\.\d+\.\d+)`), regexp.MustCompile(`AngularJS v(1\.\d+\.\d+)`),
		map[int]string{1: ""}},
	{"Lodash", regexp.MustCompile(`lodash[.-]v?(\d+\.\d+\.\d+)`), regexp.MustCompile(`(?s)lodash.{0,1000}?(?:VERSION\s*=\s*|[,;\s][a-zA-Z_$]\s*=\s*)["'](\d+\.\d+\.\d+)["']`),
		map[int]string{3: "", 4: "4.17.21"}},
	{"Moment", regexp.MustCompile(`moment[.-]v?(\d+\.\d+\.\d+)`), regexp.MustCompile(`//! version : (\d+\.\d+\.\d+)`),
		map[int]string{2: "2.29.4"}},
	{"Handlebars", regexp.MustCompile(`handlebars[.-]v?(\d+\.\d+\.\d+)`), regexp.MustCompile(`handlebars v(\d+\.\d+\.\d+)`),
		map[int]string{1: "", 2: "", 3: "", 4: "4.7.7"}},
	{"Vue 2", regexp.MustCompile(`vue[.-]v?(2\.\d+\.\d+)`), regexp.MustCompile(`Vue\.js v(2\.\d+\.\d+)`),
		map[int]string{2: ""}},
}

func (a *auditRun) outdatedLibraries() []string {
	var out []string
	for _, f := range a.subs {
		if f.Kind != "script" {
			continue
		}
		head := f.Body
		if len(head) > 1<<20 {
			head = head[:1<<20]
		}
		for _, r := range libraryRules {
			m := r.file.FindStringSubmatch(strings.ToLower(f.URL))
			if m == nil && head != nil {
				m = r.body.FindStringSubmatch(string(head))
			}
			if m == nil {
				continue
			}
			if why := libraryProblem(m[1], r.below); why != "" {
				out = append(out, r.name+" "+m[1]+" "+why)
			}
		}
	}
	return out
}

func libraryProblem(version string, below map[int]string) string {
	major, err := strconv.Atoi(strings.SplitN(version, ".", 2)[0])
	if err != nil {
		return ""
	}
	fixed, known := below[major]
	switch {
	case !known:
		return ""
	case fixed == "":
		return "is past its end of life"
	case versionLess(version, fixed):
		return "has known vulnerabilities, fixed in " + fixed
	}
	return ""
}

func versionLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x < y
		}
	}
	return false
}

// short drops the scheme and host from a URL on the audited site.
func (a *auditRun) short(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host != a.base.Host {
		return snippet(raw)
	}
	return snippet(u.RequestURI())
}
