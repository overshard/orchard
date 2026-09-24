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

// A fresh go at the same thing wants a new picture, not the last one changed.
var pictureAgain = regexp.MustCompile(`(?i)\b(again|another|redo|one more|different one|new one|start over)\b`)

// Words that change a picture wherever they fall, since "can we change the
// background of this, it's weird" opens on nothing the edit pattern knows.
var changeWord = regexp.MustCompile(`(?i)\b(change|make|replace|swap|add|remove|put|use|turn|try|instead|background|brighter|darker|lighter|colou?r|without|with|correct|fix|smooth|adjust|tweak|clean|improve|shadows?|lighting|more|less|keep)\b`)

// A question about the picture is not a change to it.
var askingAbout = regexp.MustCompile(`(?i)^\s*(why|what|how|who|where|when|is|does|did|was)\b`)

// isPictureChange is a message straight after a picture that changes it, as
// opposed to asking for a new one or asking about it.
func isPictureChange(message string, history []Message) bool {
	m := strings.TrimSpace(message)
	if m == "" || !lastWasPicture(history) || askingAbout.MatchString(m) || wantsNewPicture(m) {
		return false
	}
	return pictureEdit.MatchString(m) || changeWord.MatchString(m)
}

// wantsNewPicture is asking for a different picture rather than this one fixed.
func wantsNewPicture(m string) bool {
	return pictureAgain.MatchString(m) || pictureAsk.MatchString(m) || drawVerb.MatchString(m)
}

// A change to where the thing is rather than to the picture. Each edit of an
// edit coarsens the fabric, so these start again from the picture he attached.
var sceneWord = regexp.MustCompile(`(?i)\b(background|backdrop|room|wall|walls|scene|setting|apartment|house|kitchen|office|bedroom|outdoors?|patio|put it|place it|move it)\b`)

// The thing he names with "this sofa" or "my chair", which the image model is
// told to keep. Words that name a part of the picture rather than a thing in it
// are skipped.
var namedThing = regexp.MustCompile(`(?i)\b(?:this|that|my|the)\s+([a-z][a-z-]{2,})`)

var notAThing = map[string]bool{"picture": true, "image": true, "photo": true, "background": true,
	"room": true, "scene": true, "one": true, "same": true, "whole": true, "rest": true, "colour": true, "color": true}

// editPrompt is what the image model is asked for when a picture is changed.
// It is written here and not by the chat model, which cannot see the picture
// and describes what it imagines is in it, and the image model draws that.
// named is where the thing to keep is looked for, which for a new scene is the
// message the picture was attached to, and is empty for a change to the last
// picture as it stands.
func editPrompt(message, named string, keepThing bool) string {
	words := strings.Join(strings.Fields(message), " ")
	words = strings.TrimRight(words, " -.,")
	keep := "Keep everything in the picture that is not asked to change exactly as it is, the same shape, colours, materials and details."
	if keepThing {
		thing := "the subject"
		for _, m := range namedThing.FindAllStringSubmatch(named+" "+words, -1) {
			if !notAThing[strings.ToLower(m[1])] {
				thing = "the " + strings.ToLower(m[1])
				break
			}
		}
		keep = "Keep " + thing + " from the picture exactly as it is, the same shape, fabric, colour and details."
	}
	return keep + " " + upperFirst(words) + ". Photorealistic, professional photography, natural light and realistic shadows."
}

func upperFirst(s string) string {
	r := []rune(s)
	if len(r) > 0 {
		r[0] = unicode.ToUpper(r[0])
	}
	return string(r)
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
