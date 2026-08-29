package main

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/ua-parser/uap-go/uaparser"
)

// UAParser turns a User-Agent string into a platform, browser, device class,
// and a verdict on whether the sender is a robot.
//
// uap-core's regexes are compiled into the uap-go package rather than fetched
// at boot, so a cold start with no network still names browsers. The substring
// heuristic below is an addition to those regexes, never a replacement.
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

// NewUAParser builds a parser lazily. Compiling the full uap-core regex set is
// the most expensive thing this process would do at startup, and nothing needs
// it until the first collector hit.
func NewUAParser() *UAParser { return &UAParser{} }

func (u *UAParser) get() *uaparser.Parser {
	u.once.Do(func() {
		u.parser, u.err = uaparser.New()
		if u.err != nil {
			slog.Error(fmt.Sprintf("ua parser build failed, falling back to heuristics: %v", u.err))
		}
	})
	return u.parser
}

// uap-core reports a crawler as one of these device families, which is what
// gets a bot named rather than merely flagged.
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
	// would become a dashboard row labelled "Other" beside real browser
	// names.
	platform := notOther(client.Os.Family)
	browser := notOther(client.UserAgent.Family)
	deviceFamily := client.Device.Family

	// Two independent signals. uap-core knows the crawlers that declare
	// themselves; the needle list catches the preview fetchers and uptime
	// probes it reads as ordinary browsers, which is most of what hits a
	// small site.
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
// Broad on purpose: a false positive lands in bot_events, a table nobody makes
// decisions from, while a missed bot inflates every human metric on the
// dashboard. "monitor" and "headlesschrome" are here for that reason and would
// be too aggressive under any other goal.
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

// classifyDevice collapses uap-core's device families into the three buckets
// the dashboard charts. Tablet is tested before mobile because an iPad's
// User-Agent contains neither "mobile" nor "iphone", while an Android
// tablet's contains "mobile".
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
