package main

import (
	"fmt"
	"sort"
	"strings"
)

// The checks. Each one reads the whole crawl and returns findings.
//
// The wording of every issue string is fixed by the stored history: the
// dashboard groups on `type` and sorts on `severity`, and an operator comparing
// this week's audit to last month's is diffing these strings.

const (
	typeSEO     = "seo"
	typeLinks   = "links"
	typeA11y    = "accessibility"
	typeContent = "content"
	typePerf    = "performance"
	typeSec     = "security"

	sevError = "error"
	sevWarn  = "warning"
	sevInfo  = "info"
)

// Insight is one finding. The lowercase JSON names are fixed by the stored
// data: these live as a JSON array in properties.crawler_insights, and both the
// dashboard and the report templates read them by these names.
type Insight struct {
	URL      string `json:"url"`
	Issue    string `json:"issue"`
	Item     string `json:"item"`
	Type     string `json:"type"`
	Severity string `json:"severity"`
}

func finding(url, issue, kind, severity, item string) Insight {
	return Insight{URL: url, Issue: issue, Item: item, Type: kind, Severity: severity}
}

// checkCtx is the read-only view every check gets.
type checkCtx struct {
	startURL    string
	host        string
	pages       []*Page
	htmlPages   []*Page
	statusByURL map[string]int
	externalRaw map[string]int
	sitemapURLs []string
	robots      RobotsCtx
	compression string
}

func isRedirectStatus(code int) bool {
	switch code {
	case 301, 302, 303, 307, 308:
		return true
	}
	return false
}

// normalizeText is the key duplicate-detection groups on: case-folded with
// whitespace collapsed, so "Home  Page" and "home page" are one title.
func normalizeText(s string) string {
	return strings.ToLower(collapse(s))
}

// runChecks runs everything and returns the flat list.
//
// The order is fixed rather than incidental. The dashboard groups by type but
// preserves order within a group, so a stable list means an operator's eye
// lands in the same place every week.
//
// No check can panic the crawl: they index through bounds-checked helpers, and
// the one real hazard, dereferencing a nil HTML on a non-HTML page, is
// prevented by htmlPages being built by filter rather than asserted per
// check.
func runChecks(result *CrawlResult) []Insight {
	var htmlPages []*Page
	statusByURL := make(map[string]int, len(result.Pages))
	for _, p := range result.Pages {
		statusByURL[p.URL] = p.Status
		if p.IsHTML && p.HTML != nil {
			htmlPages = append(htmlPages, p)
		}
	}

	ctx := &checkCtx{
		startURL:    result.StartURL,
		host:        result.Host,
		pages:       result.Pages,
		htmlPages:   htmlPages,
		statusByURL: statusByURL,
		externalRaw: result.ExternalLinkStatus,
		sitemapURLs: result.SitemapURLs,
		robots:      result.Robots,
		compression: result.Compression,
	}

	checks := []func(*checkCtx) []Insight{
		checkTitleMissing,
		checkTitleLength,
		checkDuplicateTitles,
		checkDescriptionMissing,
		checkDescriptionLength,
		checkDuplicateDescriptions,
		checkH1Missing,
		checkH1Multiple,
		checkH1Length,
		checkDuplicateH1s,
		checkHeadingHierarchy,
		checkCanonicalMissing,
		checkCanonicalOffDomain,
		checkCanonicalBroken,
		checkRobotsMetaNoindex,
		checkLangMissing,
		checkViewportMissing,
		checkOGIncomplete,
		checkTwitterCard,
		checkFavicon,
		checkJSONLDParseError,
		checkBrokenInternalLinks,
		checkBrokenExternalLinks,
		checkRedirectChains,
		checkNofollowInternalLinks,
		checkRobotsMissing,
		checkSitemapMissing,
		checkSitemapNotInRobots,
		checkSitemapBrokenURLs,
		checkPagesMissingFromSitemap,
		checkImagesMissingAlt,
		checkEmptyAnchorText,
		checkFormInputsUnlabeled,
		checkThinContent,
		checkDuplicateContent,
		checkSlowPages,
		checkMissingCompression,
		checkOversizedPages,
		checkMixedContent,
	}

	out := []Insight{}
	for _, check := range checks {
		out = append(out, check(ctx)...)
	}
	return out
}

// groupPages buckets HTML pages by a normalised field, skipping pages where the
// field is empty. Used by the three duplicate checks.
//
// The returned keys are sorted. An unordered map feeding user-visible output
// makes the findings list reshuffle between crawls of an unchanged site.
func groupPages(pages []*Page, field func(*Page) string) []([]*Page) {
	buckets := map[string][]*Page{}
	for _, p := range pages {
		v := field(p)
		if v == "" {
			continue
		}
		key := normalizeText(v)
		buckets[key] = append(buckets[key], p)
	}

	keys := make([]string, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([][]*Page, 0, len(keys))
	for _, k := range keys {
		out = append(out, buckets[k])
	}
	return out
}

func h1sOf(p *Page) []string { return p.HTML.Headings["h1"] }

// ---------- core metadata ----------

func checkTitleMissing(ctx *checkCtx) []Insight {
	var out []Insight
	for _, p := range ctx.htmlPages {
		if p.HTML.Title == "" {
			out = append(out, finding(p.URL, "Page has no title", typeSEO, sevError, ""))
		}
	}
	return out
}

// checkTitleLength flags titles outside 30-60 characters, roughly what Google
// renders before truncating. Counted in runes rather than bytes, so a title in
// a non-Latin script is not reported as three times too long.
func checkTitleLength(ctx *checkCtx) []Insight {
	var out []Insight
	for _, p := range ctx.htmlPages {
		t := p.HTML.Title
		n := len([]rune(t))
		if t != "" && (n < 30 || n > 60) {
			out = append(out, finding(p.URL,
				fmt.Sprintf("Title length is %d chars (recommended 30-60)", n),
				typeSEO, sevWarn, t))
		}
	}
	return out
}

func checkDuplicateTitles(ctx *checkCtx) []Insight {
	var out []Insight
	for _, group := range groupPages(ctx.htmlPages, func(p *Page) string { return p.HTML.Title }) {
		if len(group) < 2 {
			continue
		}
		for _, p := range group {
			out = append(out, finding(p.URL, "Duplicate title", typeSEO, sevWarn, p.HTML.Title))
		}
	}
	return out
}

func checkDescriptionMissing(ctx *checkCtx) []Insight {
	var out []Insight
	for _, p := range ctx.htmlPages {
		if p.HTML.Description == "" {
			out = append(out, finding(p.URL, "Page has no meta description", typeSEO, sevError, ""))
		}
	}
	return out
}

func checkDescriptionLength(ctx *checkCtx) []Insight {
	var out []Insight
	for _, p := range ctx.htmlPages {
		d := p.HTML.Description
		n := len([]rune(d))
		if d != "" && (n < 70 || n > 160) {
			out = append(out, finding(p.URL,
				fmt.Sprintf("Description length is %d chars (recommended 70-160)", n),
				typeSEO, sevWarn, d))
		}
	}
	return out
}

func checkDuplicateDescriptions(ctx *checkCtx) []Insight {
	var out []Insight
	for _, group := range groupPages(ctx.htmlPages, func(p *Page) string { return p.HTML.Description }) {
		if len(group) < 2 {
			continue
		}
		for _, p := range group {
			out = append(out, finding(p.URL, "Duplicate meta description", typeSEO, sevWarn, p.HTML.Description))
		}
	}
	return out
}

func checkH1Missing(ctx *checkCtx) []Insight {
	var out []Insight
	for _, p := range ctx.htmlPages {
		if len(h1sOf(p)) == 0 {
			out = append(out, finding(p.URL, "Page has no h1", typeSEO, sevError, ""))
		}
	}
	return out
}

func checkH1Multiple(ctx *checkCtx) []Insight {
	var out []Insight
	for _, p := range ctx.htmlPages {
		h1s := h1sOf(p)
		if len(h1s) > 1 {
			sample := h1s
			if len(sample) > 3 {
				sample = sample[:3]
			}
			out = append(out, finding(p.URL,
				fmt.Sprintf("Page has %d h1 tags (expected 1)", len(h1s)),
				typeSEO, sevWarn, strings.Join(sample, " | ")))
		}
	}
	return out
}

func checkH1Length(ctx *checkCtx) []Insight {
	var out []Insight
	for _, p := range ctx.htmlPages {
		h1s := h1sOf(p)
		if len(h1s) == 0 {
			continue
		}
		n := len([]rune(h1s[0]))
		if n < 20 || n > 70 {
			out = append(out, finding(p.URL,
				fmt.Sprintf("H1 length is %d chars (recommended 20-70)", n),
				typeSEO, sevWarn, h1s[0]))
		}
	}
	return out
}

func checkDuplicateH1s(ctx *checkCtx) []Insight {
	var withH1 []*Page
	for _, p := range ctx.htmlPages {
		if len(h1sOf(p)) > 0 {
			withH1 = append(withH1, p)
		}
	}
	var out []Insight
	for _, group := range groupPages(withH1, func(p *Page) string { return h1sOf(p)[0] }) {
		if len(group) < 2 {
			continue
		}
		for _, p := range group {
			out = append(out, finding(p.URL, "Duplicate h1", typeSEO, sevWarn, h1sOf(p)[0]))
		}
	}
	return out
}

// checkHeadingHierarchy flags a document that jumps a heading level, such as an
// h2 followed by an h4 with no h3 between them. It reports the first skip and
// stops, because a page with a broken outline usually repeats the same break.
//
// This compares which levels are *present in the document*, not the order they
// appear in, so a page with an h1, an h3 and an h2 in that source order passes.
// Making it order-sensitive would be a different check and would fire on most
// real pages.
func checkHeadingHierarchy(ctx *checkCtx) []Insight {
	var out []Insight
	for _, p := range ctx.htmlPages {
		var levels []int
		for level := 1; level <= 6; level++ {
			if len(p.HTML.Headings[fmt.Sprintf("h%d", level)]) > 0 {
				levels = append(levels, level)
			}
		}
		for i := 1; i < len(levels); i++ {
			if levels[i]-levels[i-1] > 1 {
				out = append(out, finding(p.URL,
					fmt.Sprintf("Heading hierarchy skips from h%d to h%d", levels[i-1], levels[i]),
					typeSEO, sevInfo, ""))
				break
			}
		}
	}
	return out
}

func checkCanonicalMissing(ctx *checkCtx) []Insight {
	var out []Insight
	for _, p := range ctx.htmlPages {
		if p.HTML.Canonical == "" {
			out = append(out, finding(p.URL, "Page has no canonical URL", typeSEO, sevWarn, ""))
		}
	}
	return out
}

func checkCanonicalOffDomain(ctx *checkCtx) []Insight {
	var out []Insight
	for _, p := range ctx.htmlPages {
		c := p.HTML.Canonical
		if c != "" && !sameSite(c, ctx.host) {
			out = append(out, finding(p.URL, "Canonical URL points off-domain", typeSEO, sevWarn, c))
		}
	}
	return out
}

// checkCanonicalBroken only fires for a canonical the crawl visited. An
// unvisited one is unknown rather than broken, and proving otherwise would mean
// an extra request per page.
func checkCanonicalBroken(ctx *checkCtx) []Insight {
	var out []Insight
	for _, p := range ctx.htmlPages {
		c := p.HTML.Canonical
		if c == "" {
			continue
		}
		if status, ok := ctx.statusByURL[c]; ok && status != 200 {
			out = append(out, finding(p.URL,
				fmt.Sprintf("Canonical URL returns %d", status), typeSEO, sevError, c))
		}
	}
	return out
}

func checkRobotsMetaNoindex(ctx *checkCtx) []Insight {
	var out []Insight
	for _, p := range ctx.htmlPages {
		rm := p.HTML.RobotsMeta
		if strings.Contains(strings.ToLower(rm), "noindex") {
			out = append(out, finding(p.URL, "Page has noindex in meta robots tag", typeSEO, sevWarn, rm))
		}
	}
	return out
}

func checkLangMissing(ctx *checkCtx) []Insight {
	var out []Insight
	for _, p := range ctx.htmlPages {
		if p.HTML.Lang == "" {
			out = append(out, finding(p.URL, "HTML lang attribute missing", typeSEO, sevWarn, ""))
		}
	}
	return out
}

func checkViewportMissing(ctx *checkCtx) []Insight {
	var out []Insight
	for _, p := range ctx.htmlPages {
		if p.HTML.Viewport == "" {
			out = append(out, finding(p.URL, "Viewport meta tag missing (mobile)", typeSEO, sevWarn, ""))
		}
	}
	return out
}

func checkOGIncomplete(ctx *checkCtx) []Insight {
	var out []Insight
	for _, p := range ctx.htmlPages {
		og := p.HTML.OG
		var missing []string
		if og.Title == "" {
			missing = append(missing, "og:title")
		}
		if og.Description == "" {
			missing = append(missing, "og:description")
		}
		if og.Image == "" {
			missing = append(missing, "og:image")
		}
		if og.URL == "" {
			missing = append(missing, "og:url")
		}
		if len(missing) > 0 {
			out = append(out, finding(p.URL,
				"Open Graph tags missing: "+strings.Join(missing, ", "),
				typeSEO, sevInfo, ""))
		}
	}
	return out
}

func checkTwitterCard(ctx *checkCtx) []Insight {
	var out []Insight
	for _, p := range ctx.htmlPages {
		if p.HTML.Twitter.Card == "" {
			out = append(out, finding(p.URL, "Twitter card meta tag missing", typeSEO, sevInfo, ""))
		}
	}
	return out
}

func checkFavicon(ctx *checkCtx) []Insight {
	var out []Insight
	for _, p := range ctx.htmlPages {
		if p.HTML.Favicon == "" {
			out = append(out, finding(p.URL, "Favicon link missing", typeSEO, sevInfo, ""))
		}
	}
	return out
}

func checkJSONLDParseError(ctx *checkCtx) []Insight {
	var out []Insight
	for _, p := range ctx.htmlPages {
		if p.HTML.JSONLDBad > 0 {
			out = append(out, finding(p.URL,
				"JSON-LD structured data failed to parse", typeSEO, sevWarn, ""))
		}
	}
	return out
}

// ---------- links ----------

// linkPair keys the per-page deduplication. The same link twice in one page's
// nav is one finding; the same broken link on twenty pages is twenty, because
// each of those pages needs fixing.
type linkPair struct{ from, to string }

func checkBrokenInternalLinks(ctx *checkCtx) []Insight {
	var out []Insight
	reported := map[linkPair]bool{}
	for _, p := range ctx.htmlPages {
		for _, link := range p.HTML.Links {
			if !sameSite(link.URL, ctx.host) {
				continue
			}
			status, ok := ctx.statusByURL[link.URL]
			if !ok || status == 200 || isRedirectStatus(status) {
				continue
			}
			key := linkPair{p.URL, link.URL}
			if reported[key] {
				continue
			}
			reported[key] = true
			out = append(out, finding(p.URL,
				"Broken internal link ("+statusLabel(status)+")", typeLinks, sevError, link.URL))
		}
	}
	return out
}

func checkBrokenExternalLinks(ctx *checkCtx) []Insight {
	var out []Insight
	reported := map[linkPair]bool{}
	for _, p := range ctx.htmlPages {
		for _, link := range p.HTML.Links {
			if sameSite(link.URL, ctx.host) {
				continue
			}
			status, ok := ctx.externalRaw[link.URL]
			if !ok || (status != 0 && status < 400) {
				continue
			}
			key := linkPair{p.URL, link.URL}
			if reported[key] {
				continue
			}
			reported[key] = true
			// A warning rather than an error: the fix is on somebody else's
			// server, and a link that 404s today may be a site that is down
			// this minute.
			out = append(out, finding(p.URL,
				"Broken external link ("+statusLabel(status)+")", typeLinks, sevWarn, link.URL))
		}
	}
	return out
}

func statusLabel(status int) string {
	if status == 0 {
		return "unreachable"
	}
	return fmt.Sprintf("status %d", status)
}

// checkRedirectChains flags a URL that took more than one hop to resolve.
//
// The threshold is two hops rather than one. A single redirect is normal and
// correct (http to https, apex to www), while two or more is a chain worth
// collapsing, since every hop is a full round trip for the visitor.
func checkRedirectChains(ctx *checkCtx) []Insight {
	var out []Insight
	for _, p := range ctx.pages {
		if p.RedirectHops > 1 {
			out = append(out, finding(p.URL,
				fmt.Sprintf("Redirect chain has %d hops", p.RedirectHops),
				typeLinks, sevInfo, p.RequestedURL))
		}
	}
	return out
}

func checkNofollowInternalLinks(ctx *checkCtx) []Insight {
	var out []Insight
	reported := map[linkPair]bool{}
	for _, p := range ctx.htmlPages {
		for _, link := range p.HTML.Links {
			if !sameSite(link.URL, ctx.host) {
				continue
			}
			nofollow := false
			for _, rel := range link.Rel {
				if rel == "nofollow" {
					nofollow = true
					break
				}
			}
			if !nofollow {
				continue
			}
			key := linkPair{p.URL, link.URL}
			if reported[key] {
				continue
			}
			reported[key] = true
			out = append(out, finding(p.URL,
				"Internal link has rel=nofollow", typeLinks, sevInfo, link.URL))
		}
	}
	return out
}

// ---------- robots and sitemap ----------

func checkRobotsMissing(ctx *checkCtx) []Insight {
	if ctx.robots.Exists {
		return nil
	}
	return []Insight{finding(ctx.startURL, "robots.txt missing", typeSEO, sevWarn, ctx.robots.URL)}
}

func checkSitemapMissing(ctx *checkCtx) []Insight {
	if len(ctx.sitemapURLs) > 0 {
		return nil
	}
	return []Insight{finding(ctx.startURL, "sitemap.xml missing or empty", typeSEO, sevWarn, "")}
}

func checkSitemapNotInRobots(ctx *checkCtx) []Insight {
	if !ctx.robots.Exists || len(ctx.sitemapURLs) == 0 || ctx.robots.ReferencesSitemap {
		return nil
	}
	return []Insight{finding(ctx.startURL,
		"robots.txt does not reference a sitemap", typeSEO, sevInfo, "")}
}

func checkSitemapBrokenURLs(ctx *checkCtx) []Insight {
	var out []Insight
	for _, u := range ctx.sitemapURLs {
		status, ok := ctx.statusByURL[u]
		if !ok || status == 200 || isRedirectStatus(status) {
			continue
		}
		out = append(out, finding(u,
			fmt.Sprintf("URL listed in sitemap returns %d", status), typeSEO, sevError, ""))
	}
	return out
}

// checkPagesMissingFromSitemap reports crawled pages the sitemap does not list,
// skipping any page that asked not to be indexed. A noindex page is correctly
// absent, and flagging it would mean asking the operator to undo a choice.
func checkPagesMissingFromSitemap(ctx *checkCtx) []Insight {
	if len(ctx.sitemapURLs) == 0 {
		return nil
	}
	listed := make(map[string]bool, len(ctx.sitemapURLs))
	for _, u := range ctx.sitemapURLs {
		listed[u] = true
	}

	var out []Insight
	for _, p := range ctx.htmlPages {
		if listed[p.URL] {
			continue
		}
		if strings.Contains(strings.ToLower(p.HTML.RobotsMeta), "noindex") {
			continue
		}
		out = append(out, finding(p.URL, "Page not listed in sitemap", typeSEO, sevInfo, ""))
	}
	return out
}

// ---------- accessibility ----------

// checkImagesMissingAlt counts images with no alt attribute at all. alt="" is
// not a finding: it is the correct markup for a decorative image, which is why
// the parser keeps the distinction with a pointer.
func checkImagesMissingAlt(ctx *checkCtx) []Insight {
	var out []Insight
	for _, p := range ctx.htmlPages {
		var missing []Image
		for _, img := range p.HTML.Images {
			if img.Alt == nil {
				missing = append(missing, img)
			}
		}
		if len(missing) == 0 {
			continue
		}
		out = append(out, finding(p.URL,
			fmt.Sprintf("%d image(s) missing alt attribute", len(missing)),
			typeA11y, sevWarn, truncate(missing[0].Src, 160)))
	}
	return out
}

func checkEmptyAnchorText(ctx *checkCtx) []Insight {
	var out []Insight
	for _, p := range ctx.htmlPages {
		var empty []Link
		for _, link := range p.HTML.Links {
			if link.Text == "" {
				empty = append(empty, link)
			}
		}
		if len(empty) == 0 {
			continue
		}
		out = append(out, finding(p.URL,
			fmt.Sprintf("%d link(s) have no visible text", len(empty)),
			typeA11y, sevInfo, truncate(empty[0].URL, 160)))
	}
	return out
}

// checkFormInputsUnlabeled reports one finding per page, for the first form on
// it with unlabeled inputs. One per page rather than one per form, because a
// page whose forms are unlabeled almost always has one template generating all
// of them.
func checkFormInputsUnlabeled(ctx *checkCtx) []Insight {
	// Input types that need no label: they carry no user-entered value, or
	// their own text is the label.
	ignore := map[string]bool{
		"hidden": true, "submit": true, "button": true, "reset": true, "image": true,
	}

	var out []Insight
	for _, p := range ctx.htmlPages {
		for _, form := range p.HTML.Forms {
			labeled := make(map[string]bool, len(form.LabelFors))
			for _, id := range form.LabelFors {
				labeled[id] = true
			}

			unlabeled := 0
			for _, input := range form.Inputs {
				if ignore[input.Type] {
					continue
				}
				if input.AriaLabel != nil {
					continue
				}
				if input.ID != nil && labeled[*input.ID] {
					continue
				}
				unlabeled++
			}

			if unlabeled > 0 {
				out = append(out, finding(p.URL,
					fmt.Sprintf("%d form input(s) without associated label", unlabeled),
					typeA11y, sevWarn, form.Action))
				break
			}
		}
	}
	return out
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// ---------- content ----------

func checkThinContent(ctx *checkCtx) []Insight {
	var out []Insight
	for _, p := range ctx.htmlPages {
		if wc := p.HTML.WordCount; wc < 300 {
			out = append(out, finding(p.URL,
				fmt.Sprintf("Thin content (%d words)", wc), typeContent, sevWarn, ""))
		}
	}
	return out
}

// checkDuplicateContent groups pages by the hash of their visible text.
//
// Iterated in sorted hash order for the same reason groupPages sorts: an
// unordered map makes the findings reshuffle between crawls of a site that has
// not changed.
func checkDuplicateContent(ctx *checkCtx) []Insight {
	buckets := map[string][]string{}
	for _, p := range ctx.htmlPages {
		if h := p.HTML.TextHash; h != "" {
			buckets[h] = append(buckets[h], p.URL)
		}
	}

	hashes := make([]string, 0, len(buckets))
	for h := range buckets {
		hashes = append(hashes, h)
	}
	sort.Strings(hashes)

	var out []Insight
	for _, h := range hashes {
		urls := buckets[h]
		if len(urls) < 2 {
			continue
		}
		for _, u := range urls {
			// Name one of the others, so the finding says what the page is a
			// duplicate *of* rather than only that it is one.
			other := urls[0]
			if other == u {
				other = urls[1]
			}
			out = append(out, finding(u,
				"Page has duplicate visible content with another page",
				typeContent, sevWarn, other))
		}
	}
	return out
}

// ---------- performance ----------

func checkSlowPages(ctx *checkCtx) []Insight {
	var out []Insight
	for _, p := range ctx.pages {
		if !p.IsHTML {
			continue
		}
		if p.ElapsedMS > 1000 {
			out = append(out, finding(p.URL,
				fmt.Sprintf("Slow response (%d ms)", p.ElapsedMS), typePerf, sevWarn, ""))
		}
	}
	return out
}

// checkMissingCompression probes the start URL only. Compression is a
// server-wide setting in practice, so one probe answers for the site and
// probing every page would learn the same fact five hundred times.
func checkMissingCompression(ctx *checkCtx) []Insight {
	if ctx.compression != "" {
		return nil
	}
	return []Insight{finding(ctx.startURL,
		"Response not compressed (no Content-Encoding header)", typePerf, sevInfo, "")}
}

// checkOversizedPages measures the HTML document alone, not the page weight a
// browser would report. Images and scripts are separate requests, and
// Lighthouse covers those.
func checkOversizedPages(ctx *checkCtx) []Insight {
	var out []Insight
	for _, p := range ctx.pages {
		if p.Bytes > 500_000 {
			out = append(out, finding(p.URL,
				fmt.Sprintf("Oversized page (%d KB)", p.Bytes/1024), typePerf, sevWarn, ""))
		}
	}
	return out
}

// ---------- security ----------

// checkMixedContent finds http:// subresources on an https:// page, which
// browsers block outright for scripts and stylesheets.
func checkMixedContent(ctx *checkCtx) []Insight {
	var out []Insight
	for _, p := range ctx.htmlPages {
		if !strings.HasPrefix(p.URL, "https://") {
			continue
		}
		var insecure []string
		for _, r := range p.HTML.Resources {
			if strings.HasPrefix(r, "http://") {
				insecure = append(insecure, r)
			}
		}
		if len(insecure) == 0 {
			continue
		}
		out = append(out, finding(p.URL,
			fmt.Sprintf("Mixed content: %d http:// resource(s) on https:// page", len(insecure)),
			typeSec, sevWarn, insecure[0]))
	}
	return out
}
