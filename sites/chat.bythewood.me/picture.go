package main

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"chat.bythewood.me/tools"
)

// A message that asks for a picture outright. Left with every tool, a small
// model reads "draw me a fox" as a question about foxes and goes to Wikipedia.
var pictureAsk = regexp.MustCompile(`(?i)^\s*(ok(ay)?|so|hey|now)?[\s,]*(please\s+)?(can you\s+|could you\s+|would you\s+)?` +
	`(draw|paint|sketch|illustrate|render|generate|create|make|design|give me|show me)\b.{0,40}?` +
	`\b(picture|image|drawing|painting|sketch|illustration|photo|photograph|logo|icon|wallpaper|portrait|render|art|artwork|poster|banner|scene)s?\b`)

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
	if !lastWasPicture(history) || aboutIt.MatchString(m) {
		return false
	}
	return newView.MatchString(m) || len(strings.Fields(m)) <= 14 && pictureEdit.MatchString(m)
}

// Something said about the picture or the app rather than a change to it. "this
// didn't do anything, remember this chat" went to the image model as an edit,
// since it has "fix" in it.
var aboutIt = regexp.MustCompile(`(?i)\b(didn'?t|did not|doesn'?t|does not|wasn'?t|was not|isn'?t|is not)\s+(do|work|change|help|come out)\b|\b(remember|bug|broken|when i get home)\b|\b(this|the)\s+(chat|conversation|app)\b`)

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
	if m == "" || !lastWasPicture(history) || askingAbout.MatchString(m) || aboutIt.MatchString(m) || wantsNewPicture(m) {
		return false
	}
	return pictureEdit.MatchString(m) || changeWord.MatchString(m)
}

// wantsNewPicture is asking for a different picture rather than this one fixed.
func wantsNewPicture(m string) bool {
	return pictureAgain.MatchString(m) || pictureAsk.MatchString(m) || drawVerb.MatchString(m) || newView.MatchString(m)
}

// Another view of the same thing is a new picture. An edit keeps the framing,
// so "from the outside" drew the same living room again.
// Moving the camera is the same, since klein keeps the framing it starts from and
// "angled up higher" came back as the same picture.
var newView = regexp.MustCompile(`(?i)\b(from (the )?(outside|inside|above|below|behind|the side|the front|the back|the street|the air|another angle|a different angle|higher|lower|higher up|up high|eye level|the ceiling|the floor|the doorway|across the room)|exterior|interior|aerial|bird'?s[- ]eye|(another|different|other) (angle|side|view)|angled? (up|down|higher|lower)|(higher|lower|low|high|eye[- ]level|overhead|top[- ]down) (angle|view|shot|camera)|(zoom(ed)?|pull(ed)?) (out|back)|wider (shot|view|angle)|close[- ]?up)\b`)

// A size or an orientation he asked for, which beats whatever shape the chat
// model picks and whatever shape the picture being changed already has.
var (
	pixelSize  = regexp.MustCompile(`\b(\d{3,4})\s*(?:x|×|by)\s*(\d{3,4})\b`)
	wideWord   = regexp.MustCompile(`(?i)\b(landscape|widescreen|wide ?screen|horizontal|16:9|desktop wallpaper|wider)\b`)
	tallWord   = regexp.MustCompile(`(?i)\b(vertical|9:16|phone wallpaper|portrait (orientation|mode|shape))\b`)
	squareWord = regexp.MustCompile(`(?i)\b(square|1:1)\b`)
)

// askedShape is the shape his words ask for, or empty when they ask for none.
func askedShape(m string) string {
	if px := pixelSize.FindStringSubmatch(m); px != nil {
		w, _ := strconv.Atoi(px[1])
		h, _ := strconv.Atoi(px[2])
		switch {
		case w > h:
			return "landscape"
		case h > w:
			return "portrait"
		default:
			return "square"
		}
	}
	switch {
	case wideWord.MatchString(m):
		return "landscape"
	case tallWord.MatchString(m):
		return "portrait"
	case squareWord.MatchString(m):
		return "square"
	}
	return ""
}

// withShape puts the shape he asked for into a call's arguments.
func withShape(args json.RawMessage, shape string) json.RawMessage {
	var m map[string]any
	if shape == "" || json.Unmarshal(args, &m) != nil {
		return args
	}
	m["shape"] = shape
	out, err := json.Marshal(m)
	if err != nil {
		return args
	}
	return out
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
	var taut string
	if keepThing {
		thing := "the subject"
		for _, m := range namedThing.FindAllStringSubmatch(named+" "+words, -1) {
			if !notAThing[strings.ToLower(m[1])] {
				thing = "the " + strings.ToLower(m[1])
				break
			}
		}
		keep = "Keep " + thing + " from the picture exactly as it is, the same shape, fabric, colour and details."
		// klein relaxes upholstery into creases on every redraw, and telling it
		// not to add wrinkles puts the word wrinkles in the prompt.
		if upholstered.MatchString(thing) {
			taut = " " + upperFirst(thing) + "'s upholstery stays smooth and taut, as new."
		}
	}
	prompt := upperFirst(words) + "." + taut
	if !saysKeep.MatchString(words) {
		prompt = keep + " " + prompt
	}
	if !strings.Contains(strings.ToLower(words), "photorealistic") {
		prompt += " Photorealistic, professional photography, natural light and realistic shadows."
	}
	return prompt
}

// saysKeep is his own version of the keep sentence, which is often pasted back
// from the last prompt and would otherwise go in twice.
var saysKeep = regexp.MustCompile(`(?i)\bkeep\b.{0,60}\bexactly as\b`)

var upholstered = regexp.MustCompile(`(?i)\b(sofa|couch|sectional|loveseat|chair|armchair|recliner|ottoman|chaise|settee|headboard|bench)\b`)

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

// What a picture ask is of, once the asking and the style are taken off.
var (
	pictureOf    = regexp.MustCompile(`(?i)^.*?\b(?:picture|image|drawing|painting|sketch|illustration|photo|photograph|render|art|artwork|poster)s?\s+(?:of\s+)?`)
	pictureStyle = regexp.MustCompile(`(?i)(,|;|\s--?\s|\s(?:try|make it|in the style|photo ?realistic|realistic|at dusk|at night|at sunset|on a|with)\s).*$`)
	titleWord    = regexp.MustCompile(`[A-Za-z0-9]+`)
)

// referenceWindows are the runs of words in a picture ask worth trying as an
// article title, longest first. Three words or more, or two with a capital,
// since "living room" has an article and is not what anybody wants looked up.
func referenceWindows(message string) []string {
	s := pictureOf.ReplaceAllString(strings.TrimSpace(message), "")
	s = pictureStyle.ReplaceAllString(s, "")
	words := strings.Fields(regexp.MustCompile(`(?i)^(a|an|the)\s+`).ReplaceAllString(s, ""))
	if len(words) > 8 {
		words = words[:8]
	}
	var out []string
	for n := len(words); n >= 2; n-- {
		for i := 0; i+n <= len(words); i++ {
			run := words[i : i+n]
			if n == 2 && !capitalised(run) {
				continue
			}
			out = append(out, strings.Join(run, " "))
			if len(out) == maxReferenceTries {
				return out
			}
		}
	}
	return out
}

// Each try is a call on the bridge to a container on this machine, so ten is
// well under a second and still reaches a five word title inside eight words.
const maxReferenceTries = 10

func capitalised(words []string) bool {
	for _, w := range words {
		if r := []rune(w); len(r) > 0 && unicode.IsUpper(r[0]) {
			return true
		}
	}
	return false
}

// namesTitle is true when every word of an article's title is in what he asked,
// so a search that wandered off to something near it is not taken.
func namesTitle(title, message string) bool {
	have := terms(message)
	n := 0
	for _, w := range titleWord.FindAllString(title, -1) {
		t := terms(w)
		if len(t) == 0 {
			continue
		}
		for k := range t {
			if !have[k] {
				return false
			}
		}
		n++
	}
	return n >= 2
}
