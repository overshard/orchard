package main

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/ua-parser/uap-go/uaparser"
)

// UAParser turns a User-Agent string into a platform, browser, device class and
// a verdict on whether the sender is a robot.
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

// NewUAParser builds a parser lazily; compiling the uap-core regex set is the
// most expensive thing this process would do at startup.
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

// uap-core reports a crawler as one of these device families.
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

	// uap-core writes the literal string "Other" when nothing matched.
	platform := notOther(client.Os.Family)
	browser := notOther(client.UserAgent.Family)
	deviceFamily := client.Device.Family

	// uap-core knows the crawlers that declare themselves; the needle list
	// catches preview fetchers and uptime probes it reads as browsers.
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

// looksLikeBot is the substring pass over the raw User-Agent. It is broad,
// since a false positive only lands in bot_events while a missed bot inflates
// every human metric.
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

// classifyDevice collapses uap-core's device families into the three buckets the
// dashboard charts. Tablet is tested first: an iPad's User-Agent says neither
// "mobile" nor "iphone", and an Android tablet's says "mobile".
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
