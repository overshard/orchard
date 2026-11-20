package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// A crawler is a bot that says who it is in its user agent and can be checked
// against what its owner publishes, a list of ranges, a reverse DNS name that
// resolves back, or the owner's own network.
type crawler struct {
	name  string
	ua    *regexp.Regexp
	lists []string
	rdns  []string
	asns  []int
}

const googleRanges = "https://developers.google.com/static/crawling/ipranges/"

var crawlers = []crawler{
	{name: "google", ua: regexp.MustCompile(`(?i)googlebot|google-inspectiontool|googleother|storebot-google|adsbot-google|mediapartners-google|apis-google|feedfetcher-google|google-read-aloud|google-site-verification|google-cloudvertexbot|google-safety|googlemessages|google-agent|google-pinpoint|googleproducer`),
		lists: []string{googleRanges + "common-crawlers.json", googleRanges + "special-crawlers.json", googleRanges + "user-triggered-fetchers.json",
			googleRanges + "user-triggered-fetchers-google.json", googleRanges + "user-triggered-agents.json"},
		rdns: []string{".googlebot.com", ".google.com", ".gae.googleusercontent.com"}},
	{name: "bing", ua: regexp.MustCompile(`(?i)bingbot|bingpreview|msnbot|adidxbot`),
		lists: []string{"https://www.bing.com/toolbox/bingbot.json"}, rdns: []string{".search.msn.com"}},
	{name: "duckduckgo", ua: regexp.MustCompile(`(?i)duckduckbot|duckassistbot`),
		lists: []string{"https://duckduckgo.com/duckduckbot.json", "https://duckduckgo.com/duckassistbot.json"}},
	{name: "apple", ua: regexp.MustCompile(`(?i)applebot`),
		lists: []string{"https://search.developer.apple.com/applebot.json"}, rdns: []string{".applebot.apple.com"}},
	{name: "yandex", ua: regexp.MustCompile(`(?i)yandex(bot|images|mobilebot|accessibilitybot|metrika|renderresourcesbot)`),
		rdns: []string{".yandex.ru", ".yandex.net", ".yandex.com"}},
	{name: "baidu", ua: regexp.MustCompile(`(?i)baiduspider`), rdns: []string{".baidu.com", ".baidu.jp"}},
	{name: "openai", ua: regexp.MustCompile(`(?i)gptbot|oai-searchbot|chatgpt-user`),
		lists: []string{"https://openai.com/gptbot.json", "https://openai.com/searchbot.json", "https://openai.com/chatgpt-user.json"}},
	{name: "anthropic", ua: regexp.MustCompile(`(?i)claudebot|claude-user|claude-searchbot`),
		lists: []string{"https://claude.com/crawling/bots.json"}},
	{name: "perplexity", ua: regexp.MustCompile(`(?i)perplexitybot|perplexity-user`),
		lists: []string{"https://www.perplexity.ai/perplexitybot.json", "https://www.perplexity.ai/perplexity-user.json"}},
	{name: "ccbot", ua: regexp.MustCompile(`(?i)ccbot`),
		lists: []string{"https://index.commoncrawl.org/ccbot.json"}, rdns: []string{".crawl.commoncrawl.org"}},
	{name: "amazon", ua: regexp.MustCompile(`(?i)amazonbot|amzn-searchbot|amzn-user`),
		lists: []string{"https://developer.amazon.com/amazonbot/ip-addresses/", "https://developer.amazon.com/amazonbot/searchbot-ip-addresses/",
			"https://developer.amazon.com/amazonbot/live-ip-addresses/"}, rdns: []string{".crawl.amazonbot.amazon"}},
	{name: "mistral", ua: regexp.MustCompile(`(?i)mistralai-user`), lists: []string{"https://mistral.ai/mistralai-user-ips.json"}},
	{name: "meta", ua: regexp.MustCompile(`(?i)facebookexternalhit|meta-externalagent|meta-externalfetcher|meta-webindexer|meta-externalads|facebookbot`),
		asns: []int{32934}},
}

// previews fetch a link someone pasted. Nobody publishes where they come from,
// so the user agent is all there is, and they're kept apart from fakes.
var previews = regexp.MustCompile(`(?i)slackbot|slack-imgproxy|discordbot|twitterbot|linkedinbot|telegrambot|whatsapp|mastodon|iframely|embedly|redditbot|skypeuripreview|applenewsbot`)

// Crawlers knows each crawler's published ranges and remembers reverse DNS
// answers, so a check never waits on the network twice for one address.
type Crawlers struct {
	mu     sync.RWMutex
	ranges map[string][]netip.Prefix
	rdns   map[netip.Addr]rdnsAnswer
	looked time.Time
}

type rdnsAnswer struct {
	host string
	ok   bool
	at   time.Time
}

func NewCrawlers() *Crawlers {
	return &Crawlers{ranges: map[string][]netip.Prefix{}, rdns: map[netip.Addr]rdnsAnswer{}}
}

// Classify names what a request is: a crawler's name when it checks out, fake
// when the user agent claims a crawler and the address doesn't belong to it,
// preview for a link preview, and none for everything else.
func (c *Crawlers) Classify(ctx context.Context, ip netip.Addr, ua string, asn int) string {
	for i := range crawlers {
		cr := &crawlers[i]
		if !cr.ua.MatchString(ua) {
			continue
		}
		if c.verify(ctx, cr, ip, asn) {
			return cr.name
		}
		return "fake"
	}
	if previews.MatchString(ua) {
		return "preview"
	}
	return "none"
}

func (c *Crawlers) verify(ctx context.Context, cr *crawler, ip netip.Addr, asn int) bool {
	ip = ip.Unmap()
	if slices.Contains(cr.asns, asn) {
		return true
	}
	c.mu.RLock()
	for _, p := range c.ranges[cr.name] {
		if p.Contains(ip) {
			c.mu.RUnlock()
			return true
		}
	}
	c.mu.RUnlock()
	if len(cr.rdns) == 0 {
		return false
	}
	host, ok := c.reverse(ctx, ip)
	if !ok {
		return false
	}
	for _, suffix := range cr.rdns {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

// reverse is forward confirmed reverse DNS, a PTR whose name resolves back to
// the same address, cached for a day either way.
func (c *Crawlers) reverse(ctx context.Context, ip netip.Addr) (string, bool) {
	c.mu.RLock()
	a, seen := c.rdns[ip]
	c.mu.RUnlock()
	if seen && time.Since(a.at) < 24*time.Hour {
		return a.host, a.ok
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var r net.Resolver
	ans := rdnsAnswer{at: time.Now()}
	if names, err := r.LookupAddr(ctx, ip.String()); err == nil {
		for _, n := range names {
			n = strings.ToLower(strings.TrimSuffix(n, "."))
			addrs, err := r.LookupNetIP(ctx, "ip", n)
			if err != nil {
				continue
			}
			for _, back := range addrs {
				if back.Unmap() == ip {
					ans.host, ans.ok = n, true
				}
			}
			if ans.ok {
				break
			}
		}
	}
	c.mu.Lock()
	if len(c.rdns) > 50000 {
		c.rdns = map[netip.Addr]rdnsAnswer{}
	}
	c.rdns[ip] = ans
	c.mu.Unlock()
	return ans.host, ans.ok
}

var prefixField = regexp.MustCompile(`ipv[46]Prefix\\?"?\s*:\s*\\?"([0-9a-fA-F.:/]+)`)

// refresh fetches every list. Amazon's are HTML with the JSON inside, so the
// prefixes are pulled out by pattern instead of parsed, which reads both. A
// list that fails keeps its last good ranges.
func (c *Crawlers) refresh() {
	client := &http.Client{Timeout: 30 * time.Second}
	for _, cr := range crawlers {
		var got []netip.Prefix
		failed := false
		for _, u := range cr.lists {
			req, _ := http.NewRequest(http.MethodGet, u, nil)
			req.Header.Set("User-Agent", "gate.bythewood.me crawler list check")
			resp, err := client.Do(req)
			if err != nil {
				failed = true
				slog.Info("crawler list", slog.String("url", u), slog.Any("err", err))
				continue
			}
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				failed = true
				slog.Info("crawler list", slog.String("url", u), slog.Int("status", resp.StatusCode))
				continue
			}
			for _, m := range prefixField.FindAllSubmatch(body, -1) {
				if p, err := netip.ParsePrefix(string(m[1])); err == nil {
					got = append(got, p.Masked())
				} else if a, err := netip.ParseAddr(string(m[1])); err == nil {
					got = append(got, netip.PrefixFrom(a, a.BitLen()))
				}
			}
		}
		if len(cr.lists) == 0 {
			continue
		}
		c.mu.Lock()
		if len(got) > 0 && (!failed || len(got) >= len(c.ranges[cr.name])) {
			c.ranges[cr.name] = got
		}
		c.mu.Unlock()
	}
	c.mu.Lock()
	c.looked = time.Now()
	c.mu.Unlock()
}

// Counts is how many ranges each crawler has, for the settings page.
func (c *Crawlers) Counts() (map[string]int, time.Time) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := map[string]int{}
	for k, v := range c.ranges {
		out[k] = len(v)
	}
	return out, c.looked
}
