package tools

// A local Wikipedia, served by kiwix off the mini ZIM, which carries each lead
// section and fits where a full article would spend the window. It's on the
// bridge, so it costs no outbound request and works while a search host blocks us.

import (
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// WikiBase is the kiwix container. Set from main so a dev run can point at a
// server outside the compose network.
var WikiBase = "http://orchard-wiki:8000"

// kiwix names a book after its file, so the deploy calls the ZIM wikipedia.zim
// and this matches. Getting it wrong answers 400, not an empty result.
const wikiBook = "wikipedia"

// What one lead section is allowed to cost. Most run well under it and the cap
// is for the occasional long one.
const wikiMaxChars = 4000

type wikipediaArgs struct {
	Query string
}

func (a *wikipediaArgs) params() []param {
	return []param{
		strArg(&a.Query, "query", "the name of the thing to look up, like 'PostgreSQL' or 'North Korea', not a question").required(),
	}
}

var Wikipedia = Define(Tool{
	Name: "wikipedia",
	Description: "Look a subject up in a local offline Wikipedia snapshot. It answers in " +
		"milliseconds, costs no web request, and returns the opening section of that article. " +
		"Use it first for background on any person, place, organisation, product, species, event " +
		"or technical term, before reaching for web_search. Pass the name of the thing and not " +
		"the question: for 'who is the leader of North Korea' pass 'North Korea', and for 'what " +
		"is a b-tree used for' pass 'b-tree'. It holds background rather than news: it knows " +
		"nothing after its snapshot date and carries each article's opening section only, so use " +
		"web_search or web_fetch for detail or for anything current.",
}, func(ctx context.Context, d *Deps, a wikipediaArgs) (any, error) {
	q := a.Query
	if q == "" {
		return nil, fmt.Errorf("query is required")
	}
	if to := liveInstead(q); to != "" {
		return nil, fmt.Errorf("the snapshot cannot answer that, it holds background and not "+
			"what is happening. Call %s instead", to)
	}
	return wikiLookup(ctx, d, WikiBase, q)
})

// liveInstead names the tool for a subject whose head noun changes by the hour,
// since "Big news today" otherwise finds Universe Today. The head noun and not any
// word, so "News Corporation" and "Marketplace" still look up as what they are.
func liveInstead(q string) string {
	f := strings.Fields(strings.ToLower(strings.Trim(q, " \t?.!,:;")))
	if len(f) == 0 {
		return ""
	}
	return liveHeadNoun[f[len(f)-1]]
}

var liveHeadNoun = map[string]string{
	"news": News.Name, "headline": News.Name, "headlines": News.Name,
	"stories": News.Name, "story": News.Name, "events": News.Name,
	"weather": Weather.Name, "forecast": Weather.Name, "temperature": Weather.Name,
	"price": Markets.Name, "prices": Markets.Name, "stock": Markets.Name,
	"stocks": Markets.Name, "market": Markets.Name, "markets": Markets.Name,
	"score": SportsScores.Name, "scores": SportsScores.Name,
	"time": Now.Name, "date": Now.Name,
}

func wikiLookup(ctx context.Context, d *Deps, base, q string) (any, error) {
	// An exact title beats the ranker. A one word lookup is nearly always the
	// article's own name, and full text search on it can rank a page that
	// merely mentions the word above the page about it.
	if guess, link, ok := wikiExact(ctx, d, base, q); ok {
		title, text, err := wikiArticle(ctx, d, base, link)
		if err == nil {
			// The page knows its own capitalisation and the guessed path does
			// not, so a lookup for "postgresql" comes back as PostgreSQL.
			if title == "" {
				title = guess
			}
			return wikiResult(ctx, d, base, title, text, nil), nil
		}
	}

	hits, err := wikiSearch(ctx, d, base, q)
	if err != nil {
		return nil, err
	}
	if len(hits) == 0 {
		return wikiMiss(nil), nil
	}

	// Titles of the runners up, so a wrong first guess can be corrected with a
	// second call instead of a search.
	var also []string
	for _, h := range hits[1:] {
		also = append(also, h.Title)
	}

	top := hits[0]
	// The argument names a thing, so its article is named after it, and the top
	// ranked lead is unrelated often enough to tell the model the leader of North
	// Korea is a military history article.
	if !wikiMatches(q, top.Title) {
		return wikiMiss(append([]string{top.Title}, also...)), nil
	}

	_, text, err := wikiArticle(ctx, d, base, top.Link)
	if err != nil {
		return nil, err
	}

	return wikiResult(ctx, d, base, top.Title, text, also), nil
}

func wikiResult(ctx context.Context, d *Deps, base, title, text string, also []string) map[string]any {
	date := wikiDate(ctx, d, base)
	out := map[string]any{
		"found":   true,
		"title":   title,
		"summary": text,
		"url":     "https://en.wikipedia.org/wiki/" + strings.ReplaceAll(title, " ", "_"),
		// Its own field as well as the sentence below, since anything reading
		// this programmatically has to weigh the age without parsing prose.
		"snapshot_date": date,
		"note": "This is the opening section only, from an offline snapshot taken " + date +
			". Anything after that date is not in it. For the rest of the article, or for anything " +
			"current, call web_fetch on the url above or use web_search.",
	}
	if len(also) > 0 {
		out["other_matches"] = also
	}
	if isDisambiguation(text) {
		out["disambiguation"] = true
		out["note"] = "This is a list of things that share the name, not an article about any of them. " +
			"Call again with the one the question means, or use web_search."
	}
	return out
}

// isDisambiguation spots a page that lists the things sharing a name, which a
// model otherwise reads as a definition.
func isDisambiguation(text string) bool {
	head := strings.ToLower(strings.TrimSpace(text))
	if len(head) > 400 {
		head = head[:400]
	}
	return strings.HasPrefix(head, "look up ") || strings.Contains(head, "may refer to") ||
		strings.Contains(head, "may also refer to")
}

func wikiMiss(near []string) map[string]any {
	out := map[string]any{
		"found": false,
		"note": "No article in the offline snapshot matches that. If you passed a question, call this " +
			"again with just the name of the thing it is about. Otherwise use web_search, and do not " +
			"treat the titles below as the answer, since they are what was rejected.",
	}
	if len(near) > 0 {
		out["near_titles"] = near
	}
	return out
}

// wikiExact tries the query as an article title. Titles capitalise their first
// letter, so a lowercase query needs the second try to hit.
func wikiExact(ctx context.Context, d *Deps, base, q string) (title, link string, ok bool) {
	t := strings.ReplaceAll(strings.TrimSpace(q), " ", "_")
	if t == "" {
		return "", "", false
	}
	for _, cand := range []string{t, strings.ToUpper(t[:1]) + t[1:]} {
		link := "/content/" + wikiBook + "/" + url.PathEscape(cand)
		if _, err := wikiGet(ctx, d, base+link); err == nil {
			return strings.ReplaceAll(cand, "_", " "), link, true
		}
	}
	return "", "", false
}

var wikiWord = regexp.MustCompile(`[a-z0-9]+`)

// Words that carry no subject, so they neither count towards a match nor
// against one. "The Beatles" has to match on Beatles alone.
var wikiStop = map[string]bool{
	"the": true, "a": true, "an": true, "of": true, "in": true, "on": true, "and": true,
	"for": true, "to": true, "is": true, "are": true, "was": true, "were": true, "what": true,
	"who": true, "when": true, "where": true, "why": true, "how": true, "does": true, "do": true,
	"about": true, "me": true, "tell": true, "it": true, "its": true, "that": true, "this": true,
}

func wikiWords(s string) []string {
	var out []string
	for _, w := range wikiWord.FindAllString(strings.ToLower(s), -1) {
		if !wikiStop[w] {
			out = append(out, w)
		}
	}
	return out
}

// wikiMatches asks whether the article is about the thing asked for and doesn't
// only mention it. Half the title's own words have to appear in the query, so
// "North Korea" matches and "Military history of Korea" doesn't.
func wikiMatches(q, title string) bool {
	tw := wikiWords(title)
	if len(tw) == 0 {
		return false
	}
	asked := map[string]bool{}
	for _, w := range wikiWords(q) {
		asked[w] = true
	}
	hit := 0
	for _, w := range tw {
		if asked[w] {
			hit++
		}
	}
	return hit*2 >= len(tw)
}

type wikiHit struct {
	Title string `xml:"title"`
	Link  string `xml:"link"`
}

func wikiSearch(ctx context.Context, d *Deps, base, q string) ([]wikiHit, error) {
	u := fmt.Sprintf("%s/search?books.name=%s&pattern=%s&format=xml&pageLength=5",
		base, wikiBook, url.QueryEscape(q))
	body, err := wikiGet(ctx, d, u)
	if err != nil {
		return nil, err
	}
	var feed struct {
		Items []wikiHit `xml:"channel>item"`
	}
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("the wikipedia snapshot sent a result that could not be read")
	}
	return feed.Items, nil
}

var (
	wikiHead    = regexp.MustCompile(`(?is)<head[^>]*>.*?</head\s*>`)
	wikiH1      = regexp.MustCompile(`(?is)<h1[^>]*>.*?</h1\s*>`)
	wikiCutHead = regexp.MustCompile(`(?is)<h[23][^>]*>`)
	// kiwix appends the Creative Commons notice to every article, so without
	// this every summary ends on the same two sentences of licence.
	wikiFooter = regexp.MustCompile(`(?is)<div[^>]*class="[^"]*zim-footer[^"]*"`)
	wikiTitle  = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title\s*>`)
	// The infobox. Flattened to text it reads as a run of labels with no
	// sentence in it, which is noise a small model has to wade through to reach
	// the prose underneath.
	wikiTable = regexp.MustCompile(`(?is)<table[^>]*>.*?</table\s*>`)
)

// stripTables runs to a fixed point because infoboxes nest, and RE2 has no
// backreference to match an innermost table in one pass.
func stripTables(h string) string {
	for i := 0; i < 5; i++ {
		out := wikiTable.ReplaceAllString(h, " ")
		if out == h {
			return h
		}
		h = out
	}
	return h
}

var (
	wikiRow  = regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr\s*>`)
	wikiCell = regexp.MustCompile(`(?is)<t([hd])[^>]*>(.*?)</t[hd]\s*>`)
	wikiCSS  = regexp.MustCompile(`(?is)<style[^>]*>.*?</style\s*>`)
)

// What the fact block is allowed to cost, since an infobox can run to forty
// rows of styling and footnotes and the prose is what the reader came for.
const (
	wikiMaxFacts    = 8
	wikiMaxFactLen  = 160
	wikiMaxFactsLen = 600
)

// Captions on the images an infobox opens with, the only unlabelled rows that
// aren't facts, so they're named instead of guessed at.
var captionStart = []string{"emblem of", "standard of", "flag of", "seal of", "logo of",
	"coat of arms", "portrait of", "official portrait", "map of", "photograph of"}

func wikiCaption(v string) bool {
	l := strings.ToLower(strings.TrimSpace(v))
	for _, p := range captionStart {
		if strings.HasPrefix(l, p) {
			return true
		}
	}
	return false
}

// A model reads the spouse row's "m. 2012; sep. 2021" as still married.
var (
	zeroWidth    = strings.NewReplacer("\u200b", "", "\u200c", "", "\u200d", "", "\ufeff", "")
	marriageMark = regexp.MustCompile(`\b(m|sep|div)\.\s+(\d{4})`)
	marriageWord = map[string]string{"m": "married", "sep": "separated", "div": "divorced"}
)

func spellMarriage(t string) string {
	t = zeroWidth.Replace(t)
	t = marriageMark.ReplaceAllStringFunc(t, func(m string) string {
		p := marriageMark.FindStringSubmatch(m)
		return marriageWord[p[1]] + " " + p[2]
	})
	return strings.Join(strings.Fields(t), " ")
}

// wikiInfobox keeps the rows of the first table, since "Prime Minister of Japan"
// names its incumbent only here. A row with no label is kept on its own, since
// that's the shape the incumbent row comes in.
func wikiInfobox(h string) []string {
	m := wikiTable.FindString(h)
	if m == "" {
		return nil
	}
	var out []string
	total := 0
	for _, r := range wikiRow.FindAllStringSubmatch(m, -1) {
		var label, value string
		for _, c := range wikiCell.FindAllStringSubmatch(r[1], -1) {
			t := strings.TrimSpace(Text(wikiCSS.ReplaceAllString(c[2], " ")))
			t = strings.ReplaceAll(t, "\n", " ")
			t = spellMarriage(t)
			if c[1] == "h" && label == "" {
				label = t
				continue
			}
			if value == "" {
				value = t
			}
		}
		// The class rules in a cell flatten to a stylesheet, which is never a
		// fact.
		if strings.Contains(value, "mw-parser-output") || strings.Contains(label, "mw-parser-output") {
			continue
		}
		// An unlabelled row is usually the caption under a picture, and the few
		// that are not are the office rows worth keeping.
		if label == "" && wikiCaption(value) {
			continue
		}
		line := strings.TrimSpace(value)
		if label != "" && value != "" {
			line = label + ": " + value
		} else if label != "" && value == "" {
			continue
		}
		if line == "" || len(line) > wikiMaxFactLen {
			continue
		}
		out = append(out, line)
		total += len(line)
		if len(out) >= wikiMaxFacts || total >= wikiMaxFactsLen {
			break
		}
	}
	return out
}

// wikiArticle returns the lead section as plain text. The mini snapshot has no
// sections below the lead, and cutting at the first heading anyway means this
// still returns a lead if the ZIM is ever swapped for a full flavour.
func wikiArticle(ctx context.Context, d *Deps, base, link string) (title, text string, err error) {
	if !strings.HasPrefix(link, "/") {
		link = "/" + link
	}
	body, err := wikiGet(ctx, d, base+link)
	if err != nil {
		return "", "", err
	}
	h := string(body)
	if m := wikiTitle.FindStringSubmatch(h); m != nil {
		title = strings.TrimSpace(html.UnescapeString(m[1]))
	}
	h = wikiHead.ReplaceAllString(h, " ")
	if loc := wikiCutHead.FindStringIndex(h); loc != nil {
		h = h[:loc[0]]
	}
	h = wikiH1.ReplaceAllString(h, " ")
	if loc := wikiFooter.FindStringIndex(h); loc != nil {
		h = h[:loc[0]]
	}
	facts := wikiInfobox(h)
	h = stripTables(h)

	text = strings.TrimSpace(Text(h))
	if len(facts) > 0 {
		text = strings.Join(facts, "\n") + "\n\n" + text
	}
	if len(text) > wikiMaxChars {
		// Cut on a sentence so the model is not handed half a clause.
		cut := text[:wikiMaxChars]
		if i := strings.LastIndex(cut, ". "); i > wikiMaxChars/2 {
			cut = cut[:i+1]
		}
		text = cut
	}
	if text == "" {
		return "", "", fmt.Errorf("that article is in the snapshot but its opening section is empty")
	}
	return title, text, nil
}

// wikiGet skips get() since the Guard and the budgets are for third party hosts
// that rate limit this address, and boxing a container on the bridge would take
// the snapshot out over a blip.
func wikiGet(ctx context.Context, d *Deps, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := d.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("the offline wikipedia is not answering: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("the offline wikipedia answered %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

// The snapshot's date, latched on the first success, since a model not told how
// old an offline fact is presents it as current, and latching a failure would
// leave a container that started before kiwix with an unknown date for good.
var (
	wikiDateMu  sync.Mutex
	wikiDateVal string
)

func wikiDate(ctx context.Context, d *Deps, base string) string {
	wikiDateMu.Lock()
	defer wikiDateMu.Unlock()
	if wikiDateVal != "" {
		return wikiDateVal
	}
	const unknown = "an unknown date"
	body, err := wikiGet(ctx, d, base+"/catalog/v2/entries")
	if err != nil {
		return unknown
	}
	var feed struct {
		Entries []struct {
			Updated string `xml:"updated"`
		} `xml:"entry"`
	}
	if err := xml.Unmarshal(body, &feed); err != nil || len(feed.Entries) == 0 {
		return unknown
	}
	t, err := time.Parse(time.RFC3339, feed.Entries[0].Updated)
	if err != nil {
		return unknown
	}
	wikiDateVal = t.Format("January 2006")
	return wikiDateVal
}
