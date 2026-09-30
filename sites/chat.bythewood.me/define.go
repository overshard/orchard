package main

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"chat.bythewood.me/tools"
)

// A word to define is looked up in the dictionary before the model decides
// anything, the same way a subject is looked up in the snapshot.
var (
	defineAsk   = regexp.MustCompile(`(?i)^\s*(?:please\s+)?(?:define|definition of|meaning of|what(?:'s| is) the (?:meaning|definition) of)\s+(.+?)\s*[?.!]*\s*$`)
	meaningAsk  = regexp.MustCompile(`(?i)^\s*what\s+does\s+(.+?)\s+mean\s*[?.!]*\s*$`)
	defineWords = 4
)

// definedWord is the word a message asks the meaning of, or empty.
func definedWord(question string) string {
	m := defineAsk.FindStringSubmatch(question)
	if m == nil {
		m = meaningAsk.FindStringSubmatch(question)
	}
	if m == nil {
		return ""
	}
	w := strings.Trim(strings.TrimSpace(m[1]), "\"'“”‘’")
	w = regexp.MustCompile(`(?i)^(a|an|the)\s+`).ReplaceAllString(w, "")
	if w == "" || len(strings.Fields(w)) > defineWords || subjectPronoun.MatchString(w) {
		return ""
	}
	return w
}

func (e *Engine) definitionOpening(ctx context.Context, deps *tools.Deps, question string) (tools.Result, Message, bool) {
	word := definedWord(question)
	if word == "" {
		return tools.Result{}, Message{}, false
	}
	args, err := json.Marshal(map[string]string{"word": word})
	if err != nil {
		return tools.Result{}, Message{}, false
	}
	res := e.reg.Call(ctx, deps, tools.Dictionary.Name, args)
	m, ok := res.Content.(map[string]any)
	if res.Err != "" || !ok {
		return tools.Result{}, Message{}, false
	}
	if found, _ := m["found"].(bool); !found {
		return tools.Result{}, Message{}, false
	}
	msg := Message{Role: RoleUser, Content: "Before you answer, here is what Wiktionary says " + quoted(word) +
		" means, looked up for you: " + resultText(res) + "\n\nHe is asking what the word means, so answer with " +
		"the everyday senses, and say nothing about what else could be looked up."}
	return res, msg, true
}

// A bare web address is a site, and a site can launch or change hands after
// the snapshot was taken. "what is america.gov" got the State Department's
// article through a redirect the day after a new site launched there.
var siteName = regexp.MustCompile(`(?i)^[a-z0-9-]+(\.[a-z0-9-]+)*\.(com|org|net|gov|mil|edu|io|dev|ai|app|co|us|uk|me|tv|xyz|info)$`)

func (e *Engine) siteOpening(ctx context.Context, deps *tools.Deps, question string) (tools.Result, Message, bool) {
	site := subjectOf(question)
	if !siteName.MatchString(site) {
		return tools.Result{}, Message{}, false
	}
	args, err := json.Marshal(map[string]string{"query": site})
	if err != nil {
		return tools.Result{}, Message{}, false
	}
	res := e.reg.Call(ctx, deps, tools.WebSearch.Name, args)
	if res.Err != "" {
		return tools.Result{}, Message{}, false
	}
	msg := Message{Role: RoleUser, Content: "Before you answer, here is what a web search for " + quoted(site) +
		" returned: " + resultText(res) + "\n\nA website can launch or change hands after your training, so say " +
		"what these results say it is now, and fetch the site or an article about it if they do not say."}
	return res, msg, true
}
