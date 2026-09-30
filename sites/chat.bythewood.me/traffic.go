package main

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"chat.bythewood.me/tools"
)

// Traffic on his own sites is read before the model decides. Asked whether
// eight views in a week sounded right, it asked the betting markets six times.
var (
	ownSite     = regexp.MustCompile(`(?i)\borchard[\s_-]?analytics\b|\banalytics\.bythewood\b|\b(my|our)\b.{0,30}\b(blog|site|sites|website|websites|portfolio|posts?|analytics|traffic)\b`)
	trafficWord = regexp.MustCompile(`(?i)\b(analytics|traffic|page\s?views?|pageviews|views|visits|visitors|readers|sessions|referr?ers|referrals|popular|hits)\b`)
	trafficDays = regexp.MustCompile(`(?i)\b(\d{1,3})\s*days?\b|\b(week|fortnight|month|quarter|year)\b`)
	blogWord    = regexp.MustCompile(`(?i)\bblog\b`)
	folioWord   = regexp.MustCompile(`(?i)\b(portfolio|isaacbythewood)\b`)
)

func asksAboutTraffic(q string) bool { return ownSite.MatchString(q) && trafficWord.MatchString(q) }

// trafficQuestion is the property and the number of days a question about his
// traffic asks for, with a follow up taking whatever it leaves out from the
// question before it.
func trafficQuestion(question string, history []Message) (property string, days int, ok bool) {
	var earlier string
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == RoleUser {
			earlier = history[i].Content
			break
		}
	}
	switch {
	case asksAboutTraffic(question):
	case trafficWord.MatchString(question) && asksAboutTraffic(earlier):
	default:
		return "", 0, false
	}
	property, days = trafficArgs(question)
	if p, d := trafficArgs(earlier); earlier != "" && !asksAboutTraffic(question) {
		if property == "" {
			property = p
		}
		if days == 0 {
			days = d
		}
	}
	return property, days, true
}

func trafficArgs(q string) (property string, days int) {
	switch {
	case blogWord.MatchString(q):
		property = "blog"
	case folioWord.MatchString(q):
		property = "isaacbythewood"
	}
	if m := trafficDays.FindStringSubmatch(q); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			days = n
		} else {
			days = map[string]int{"week": 7, "fortnight": 14, "month": 30, "quarter": 90, "year": 365}[strings.ToLower(m[2])]
		}
	}
	if days < 0 || days > 365 {
		days = 0
	}
	return property, days
}

// A follow up pushing on a figure also gets the last 28 days, since with a
// week alone the model defended eight views by projecting a month it made up.
const contextDays = 28

func (e *Engine) trafficOpening(ctx context.Context, deps *tools.Deps, question string, history []Message) ([]tools.Result, Message, bool) {
	property, days, ok := trafficQuestion(question, history)
	if !ok {
		return nil, Message{}, false
	}
	windows := []int{days}
	if !asksAboutTraffic(question) && days < contextDays {
		windows = append(windows, contextDays)
	}
	var out []tools.Result
	var read []string
	for _, d := range windows {
		a := map[string]any{}
		if property != "" {
			a["property"] = property
		}
		if d > 0 {
			a["days"] = d
		}
		args, err := json.Marshal(a)
		if err != nil {
			return nil, Message{}, false
		}
		res := e.reg.Call(ctx, deps, tools.OrchardAnalytics.Name, args)
		if res.Err != "" {
			break
		}
		out = append(out, res)
		if d == 0 {
			d = 7
		}
		label := "The last " + strconv.Itoa(d) + " days: "
		if len(read) > 0 {
			label = "The last " + strconv.Itoa(d) + " days, for context: "
		}
		read = append(read, label+resultText(res))
	}
	if len(out) == 0 {
		return nil, Message{}, false
	}
	how := "This is his own record of his sites, so answer from it."
	if len(out) > 1 {
		how = "This is his own record of his sites. The question is about the first window, so answer that " +
			"from it and use the second only to put it in context."
	}
	msg := Message{Role: RoleUser, Content: "Before you answer, his own analytics were read for you with " +
		"orchard_analytics.\n\n" + strings.Join(read, "\n\n") + "\n\n" + how + " Call orchard_analytics " +
		"again for another window or another property. Nothing on the web or in a betting market knows his traffic."}
	return out, msg, true
}
