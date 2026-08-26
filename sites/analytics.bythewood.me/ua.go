package main

import (
	"log"
	"strings"
	"sync"

	"github.com/ua-parser/uap-go/uaparser"
)

// UAParser turns a User-Agent string into a platform, browser, device class,
// and a verdict on whether the sender is a robot.
//
// The regexes are compiled into the uap-go package rather than fetched, which
// is the one place this port is straightforwardly better than the Rust version
// rather than merely equivalent. That one downloaded uap-core's regexes.yaml
// on first boot and, until it landed, fell back to a substring heuristic that
// could not name a browser or an operating system at all. So a cold start with
// no network produced events with null platform and browser forever, and
// nothing ever went back to fix them.
//
// Deleted with it: the download, the on-disk copy under the data directory,
// the "reload after download" dance the Rust version never actually wired up,
// and the fallback path. The heuristic below survives, but only as an addition
// to the regexes, never as a replacement for them.
type UAParser struct {
	once   sync.Once
	parser *uaparser.Parser
	err    error
}

// ParsedUA is the enrichment written onto every event.
type ParsedUA struct {
	Platform string
	Browser  string
	Device   string // Mobile, Tablet or Desktop
	IsBot    bool
	BotName  string
}

// NewUAParser builds a parser lazily.
//
// Lazily because compiling the full uap-core regex set is the single most
// expensive thing this process does at startup, and a deploy restarts it on
// every push. Nothing needs the parser until the first collector hit.
func NewUAParser() *UAParser { return &UAParser{} }

func (u *UAParser) get() *uaparser.Parser {
	u.once.Do(func() {
		u.parser, u.err = uaparser.New()
		if u.err != nil {
			log.Printf("ua parser build failed, falling back to heuristics: %v", u.err)
		}
	})
	return u.parser
}

// uap-core reports a crawler as one of these device families. Trusting it
// first is what gets a bot named rather than merely flagged.
var spiderFamilies = map[string]bool{
	"Spider":            true,
	"Spider Desktop":    true,
	"Spider Smartphone": true,
	"Spider Tablet":     true,
}

func (u *UAParser) Parse(ua string) ParsedUA {
	parser := u.get()
	if parser == nil {
		isBot := looksLikeBot(ua)
		out := ParsedUA{IsBot: isBot}
		if isBot {
			out.BotName = "Unknown bot"
		} else {
			out.Device = classifyDevice(ua, "")
		}
		return out
	}

	client := parser.Parse(ua)

	// uap-core writes the literal string "Other" when nothing matched, which
	// is a worse answer than no answer: it would become a dashboard row
	// labelled "Other" sitting alongside real browser names.
	platform := notOther(client.Os.Family)
	browser := notOther(client.UserAgent.Family)
	deviceFamily := client.Device.Family

	// Two independent bot signals, OR'd. uap-core knows the crawlers that
	// declare themselves; the needle list below catches the preview fetchers
	// and uptime probes it classifies as ordinary browsers, which is most of
	// what actually hits a small site.
	isBot := spiderFamilies[deviceFamily] || looksLikeBot(ua)

	out := ParsedUA{Platform: platform, Browser: browser, IsBot: isBot}
	if isBot {
		out.BotName = browser
	} else {
		out.Device = classifyDevice(ua, deviceFamily)
	}
	return out
}

func notOther(family string) string {
	if family == "Other" {
		return ""
	}
	return family
}

// looksLikeBot is the substring pass over the raw User-Agent.
//
// Deliberately broad, because the cost of a false positive is asymmetric: a
// misfiled bot lands in bot_events, which is a table nobody makes decisions
// from, while a missed bot inflates every human metric on the dashboard.
// "monitor" and "headlesschrome" are in here for that reason and would be too
// aggressive under any other goal.
var botNeedles = []string{
	"bot", "crawl", "spider", "slurp", "facebookexternalhit", "ahrefs", "semrush",
	"petalbot", "yandex", "bingpreview", "duckduckgo", "discordbot", "whatsapp",
	"telegrambot", "applebot", "linkedinbot", "embedly", "headlesschrome",
	"phantomjs", "lighthouse", "pingdom", "uptimerobot", "monitor",
}

func looksLikeBot(ua string) bool {
	lower := strings.ToLower(ua)
	for _, n := range botNeedles {
		if strings.Contains(lower, n) {
			return true
		}
	}
	return false
}

// classifyDevice collapses uap-core's hundreds of device families into the
// three buckets the dashboard actually charts. Tablet is tested before mobile
// because an iPad's User-Agent contains neither "mobile" nor "iphone" but a
// generic Android tablet's contains "mobile".
func classifyDevice(ua, family string) string {
	lower := strings.ToLower(ua)

	if family == "iPad" || family == "Tablet" ||
		strings.Contains(lower, "tablet") || strings.Contains(lower, "ipad") {
		return "Tablet"
	}
	if family == "iPhone" || family == "iPod" || family == "Generic Smartphone" ||
		strings.Contains(lower, "mobile") || strings.Contains(lower, "iphone") ||
		strings.Contains(lower, "android") {
		return "Mobile"
	}
	return "Desktop"
}
