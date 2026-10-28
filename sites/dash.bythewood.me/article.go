package main

import (
	"context"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// Article text for the TL;DRs on the cards, pulled out of the page with no
// parser: the paragraphs inside <article> when there is one, else every
// paragraph on the page, with the page's furniture cut out first.

const (
	articleRunes = 6000
	// Under this the page is a paywall, a consent wall or a link list, and
	// the meta description is all there is.
	articleFloor = 400
)

var (
	articleBlocks []*regexp.Regexp
	articleBody   = regexp.MustCompile(`(?is)<article\b[^>]*>(.*)</article>`)
	paragraph     = regexp.MustCompile(`(?is)<p\b[^>]*>(.*?)</p>`)
	metaDesc      = regexp.MustCompile(`(?is)<meta\b[^>]*(?:name|property)\s*=\s*["'](?:og:description|description)["'][^>]*>`)
	metaContent   = regexp.MustCompile(`(?is)content\s*=\s*["']([^"']*)["']`)
)

// Go's regexp has no backreferences, so each block gets its own pattern.
func init() {
	for _, t := range []string{"script", "style", "noscript", "svg", "nav", "header", "footer", "aside", "form", "figure", "button"} {
		articleBlocks = append(articleBlocks, regexp.MustCompile(`(?is)<`+t+`\b.*?</`+t+`>`))
	}
}

// skipArticle is a link with no article behind it worth fetching: a site's own
// discussion page, or Google News, which only redirects with script.
func skipArticle(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return true
	}
	switch strings.TrimPrefix(u.Hostname(), "www.") {
	case "news.ycombinator.com", "lobste.rs", "news.google.com":
		return true
	}
	return strings.HasSuffix(strings.ToLower(u.Path), ".pdf")
}

// fetchArticle is the readable text of the page at raw, or "" when there
// isn't any. Every site shares one budget but never trips the breaker, since
// one paywall answering 403 says nothing about the next site.
func fetchArticle(ctx context.Context, g *Guard, raw string) string {
	if skipArticle(raw) || g.Reserve(ctx, "articles") != nil {
		return ""
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html")
	resp, err := articleClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	g.Succeed("articles")
	if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Content-Type"), "html") {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return ""
	}
	return articleText(string(body))
}

func articleText(page string) string {
	region := page
	if m := articleBody.FindStringSubmatch(page); m != nil {
		region = m[1]
	}
	for _, re := range articleBlocks {
		region = re.ReplaceAllString(region, " ")
	}
	var b strings.Builder
	for _, m := range paragraph.FindAllStringSubmatch(region, -1) {
		p := strings.Join(strings.Fields(html.UnescapeString(tags.ReplaceAllString(m[1], " "))), " ")
		// A caption, a byline or a share prompt, not a paragraph.
		if len(p) < 40 || len(strings.Fields(p)) < 7 {
			continue
		}
		b.WriteString(p + "\n")
		if b.Len() > articleRunes*4 {
			break
		}
	}
	text := b.String()
	if len(text) < articleFloor {
		text = ""
		if m := metaDesc.FindString(page); m != "" {
			if c := metaContent.FindStringSubmatch(m); c != nil {
				text = strings.TrimSpace(html.UnescapeString(c[1]))
			}
		}
		if len(text) < 60 {
			return ""
		}
	}
	if r := []rune(text); len(r) > articleRunes {
		text = string(r[:articleRunes])
	}
	return strings.TrimSpace(text)
}

// tldrMax leaves room past the asked words, like the other caps here.
const tldrMax = 420

// tldr asks for two or three sentences out of what system and user set up,
// with one retry for a cut answer like threadRead's.
func (b *Briefer) tldr(ctx context.Context, system, user string) string {
	schema := map[string]any{
		"type":                 "object",
		"properties":           map[string]any{"tldr": map[string]any{"type": "string", "maxLength": tldrMax}},
		"required":             []string{"tldr"},
		"additionalProperties": false,
	}
	var out struct {
		TLDR string `json:"tldr"`
	}
	ask := user
	for try := 0; try < 2; try++ {
		if err := b.model.Structured(ctx, system, ask, schema, 300, &out); err != nil {
			slog.Warn("brief tldr failed", slog.String("component", "brief"), slog.Any("err", err))
			return ""
		}
		if s := tidy(out.TLDR); s == "" || strings.ContainsAny(s[len(s)-1:], ".!?") {
			return s
		}
		ask = user + "\n\nYour last TL;DR ran past the length limit and was cut off mid sentence. Write it again shorter, at most 50 words."
	}
	return whole(out.TLDR, tldrMax)
}

const articleSystem = `You write a TL;DR of one article for one reader, from the text of its page.
- "tldr" is two or three sentences, at most 50 words in all. Say what the article reports or argues and its main point.
- Use only the text given. No outside knowledge, no opinion of your own, no hype words.
- If the text isn't an article, like a login wall, a cookie notice, an error or a list of links, "tldr" is an empty string.`

func (b *Briefer) articleTLDR(ctx context.Context, title, text string) string {
	if text == "" {
		return ""
	}
	return b.tldr(ctx, articleSystem, fmt.Sprintf("Title: %s\n\nText:\n%s", title, text))
}

// eventRunes is each outlet's share of an event TL;DR's input, so four long
// articles still fit the model's context with room to spare.
const eventRunes = 2500

const eventSystem = "You write a TL;DR of one news event for one reader in the United States, from how several outlets reported it.\n" + neutralRules + `
- "tldr" is two or three sentences, at most 60 words in all. Say what happened, who it involves, and what comes next if the reports say.
- If the outlets report different facts or figures, say so briefly and name them.
- Use only the reports given.`

// eventReports reads every outlet a TODAY point links to, its page when it can
// be fetched and its feed's description when it can't. read is whether there
// was anything past the titles.
func (b *Briefer) eventReports(ctx context.Context, events []cluster, nums []int, links []Link) (text string, read bool) {
	byURL := map[string]story{}
	for _, n := range nums {
		if n >= 1 && n <= len(events) {
			for _, s := range events[n-1].stories {
				byURL[s.url] = s
			}
		}
	}
	var u strings.Builder
	for _, l := range links {
		s := byURL[l.URL]
		fmt.Fprintf(&u, "[%s] %s\n", l.Source, l.Title)
		if s.desc != "" {
			u.WriteString("Summary: " + s.desc + "\n")
			read = true
		}
		if text := fetchArticle(ctx, b.guard, l.URL); text != "" {
			if r := []rune(text); len(r) > eventRunes {
				text = string(r[:eventRunes])
			}
			u.WriteString("Text: " + text + "\n")
			read = true
		}
		u.WriteString("\n")
	}
	return u.String(), read
}

func (b *Briefer) eventTLDR(ctx context.Context, reports string) string {
	return b.tldr(ctx, eventSystem, "Reports of one news event:\n\n"+reports)
}
