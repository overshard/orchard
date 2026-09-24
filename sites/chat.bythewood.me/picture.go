package main

import (
	"regexp"
	"strings"
	"unicode"

	"chat.bythewood.me/tools"
)

// A message that asks for a picture outright. Left with every tool, a small
// model reads "draw me a fox" as a question about foxes and goes to Wikipedia.
var pictureAsk = regexp.MustCompile(`(?i)^\s*(ok(ay)?|so|hey|now)?[\s,]*(please\s+)?(can you\s+|could you\s+|would you\s+)?` +
	`(draw|paint|sketch|illustrate|render|generate|create|make|design|give me|show me)\b.{0,40}?` +
	`\b(picture|image|drawing|painting|sketch|illustration|photo|photograph|logo|icon|wallpaper|portrait|render|art|artwork|poster|banner)s?\b`)

// Drawing verbs are unambiguous on their own, so "draw a fox" needs no noun.
var drawVerb = regexp.MustCompile(`(?i)^\s*(ok(ay)?|so|hey|now)?[\s,]*(please\s+)?(can you\s+|could you\s+)?(draw|paint|sketch|illustrate)\b`)

// A short instruction straight after a picture is a change to it: "make it
// night", "now in watercolour", "try again".
var pictureEdit = regexp.MustCompile(`(?i)^\s*(ok(ay)?|now|and|but|hmm)?[\s,]*(please\s+)?(can you\s+|could you\s+)?` +
	`(make|try|redo|do|draw|change|add|remove|put|give|turn|swap|use|another|again|same|more|less|without|with|one more|now)\b`)

// isPictureAsk reports whether the message wants a picture drawn, and only
// then is every other tool taken off the table.
func isPictureAsk(message string, history []Message) bool {
	m := strings.TrimSpace(message)
	if m == "" || strings.Contains(m, "?") && !pictureAsk.MatchString(m) {
		return false
	}
	if pictureAsk.MatchString(m) || drawVerb.MatchString(m) {
		return true
	}
	return lastWasPicture(history) && len(strings.Fields(m)) <= 14 && pictureEdit.MatchString(m)
}

func lastWasPicture(history []Message) bool {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == RoleAssistant {
			return strings.HasPrefix(history[i].Content, drewPrefix)
		}
	}
	return false
}

// drewPrefix opens every stored picture turn. The model reads it back as the
// record of what was drawn, which is what a change to the picture needs.
const drewPrefix = "Drew it from this prompt:\n\n> "

// pictureReply is the whole answer to a picture. It is written here rather
// than by the model, because the model is off the card by then and bringing it
// back for one sentence costs more than the sentence is worth.
func pictureReply(res tools.Result) string {
	if res.Err != "" {
		return "The picture didn't come out, " + res.Err + "."
	}
	m, _ := res.Content.(map[string]any)
	prompt, _ := m["prompt"].(string)
	return drewPrefix + strings.Join(strings.Fields(prompt), " ")
}

func hasPicture(widgets []Widget) bool {
	for _, w := range widgets {
		if w.Kind == "image" {
			return true
		}
	}
	return false
}

// pictureTitle names a conversation after what was drawn, off the stored
// reply, so the title needs no model call.
func pictureTitle(reply string) string {
	p := strings.TrimPrefix(reply, drewPrefix)
	p = strings.TrimSpace(strings.SplitN(p, ",", 2)[0])
	if p == "" {
		return "A picture"
	}
	r := []rune(trimLine(p, 48))
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}
