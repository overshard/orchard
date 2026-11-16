package main

import (
	"regexp"
	"strings"
)

// The shapes a question opens with, stripped so what's left is the name of the
// thing asked about. "how much" and "how many" go ahead of the bare question
// words or "how" strips alone and leaves "much is a keystone crickett" behind.
var questionHead = regexp.MustCompile(`(?i)^\s*how\s+(much|many)\s+(is|are|was|were|do|does|did|would|will)?\s*|^\s*(what|who|which|where|when|why|how)('?s| is| are| was| were| do| does| did)?\s+|^\s*(tell me about|explain|describe|define)\s+`)

// Trailing filler left behind once the head is gone, as in "kubernetes for".
var questionTail = regexp.MustCompile(`(?i)\s+((is|are|was|were)\s+(it|this|that|they|there)|for|about|like|used for|good for|mean|means|work|works|released|founded|invented|built|born|made|created|discovered|launched|announced|introduced|formed|established|written|published|directed|designed|coming out|come out|out)\s*[?.!]*\s*$`)

// A trailing time phrase is stripped and not used as a signal on its own, since
// the question may still name a real subject. "what is the RTX 5090 going for
// today" is about the card.
var timeTail = regexp.MustCompile(`(?i)\s+(right\s+now|now|today|tonight|yesterday|currently|lately|so\s+far|this\s+(morning|afternoon|evening|week|weekend|month|year)|last\s+(night|week|weekend|month|year))\s*[?.!]*\s*$`)

// A subject runs to the first clause break. "a b-tree and why do databases use
// them" is one question about one thing.
var clauseBreak = regexp.MustCompile(`(?i)\s+(and|but|or|so|because|since|which|that|vs\.?|versus)\s+`)

// Past this it's a sentence and not the name of something.
const subjectMaxWords = 5

// A short clause still isn't a subject, "That's a crazy high number" is four
// words. These three catch somebody talking instead of a title.

// A pronoun is the giveaway, since a title almost never contains one and a
// sentence about something almost always does.
var subjectPronoun = regexp.MustCompile(`(?i)(^|\s)(i|i'?m|i'?ve|you|you'?re|we|we'?re|they|they'?re|he|she|it|it'?s|me|my|your|our|their|its|us|him|her|this|that|that'?s|these|those|thats)($|[\s',.!?])`)

// A finite verb, which makes the string a statement or a question about a thing
// and not the thing. "is there no service", "oh it needs redis to run", "sorry i
// wanted total calories".
var subjectVerb = regexp.MustCompile(`(?i)(^|\s)(is|are|was|were|be|been|am|do|does|did|has|have|had|can|could|should|would|will|want|wants|wanted|need|needs|needed|think|thinks|thought|said|says|got|get|gets|goes|went|make|makes|made|take|takes|took|seems|looks|feels|means|works|runs|sounds)($|[\s',.!?])`)

// A pasted address, which reduces to itself and is never a title.
var subjectURL = regexp.MustCompile(`(?i)(^|\s)(https?://|www\.)`)

// What a message opens with when it's a reply instead of a question. The rest
// of it is a remark about what was just said, so there is no subject in it to
// find. "tl;dr <url>" is here because the subject of that turn is the page.
var openingFiller = map[string]bool{
	"oh": true, "ok": true, "okay": true, "sorry": true, "thanks": true, "thank": true,
	"yeah": true, "yea": true, "yep": true, "yes": true, "no": true, "nope": true,
	"hmm": true, "huh": true, "wow": true, "lol": true, "haha": true, "well": true,
	"also": true, "actually": true, "wait": true, "hey": true, "hi": true, "hello": true,
	"please": true, "tldr": true, "tl;dr": true, "btw": true, "anyway": true, "right": true,
	"ah": true, "ahh": true, "aha": true, "hm": true, "hmmm": true, "hmmmm": true,
	"ugh": true, "nice": true, "cool": true, "sure": true, "alright": true, "damn": true, "dang": true,
}

// subjectOf pulls the thing a question is about out of it, or returns empty
// when there is not one worth looking up.
func subjectOf(question string) string {
	s := strings.TrimSpace(question)
	if s == "" {
		return ""
	}
	s = questionHead.ReplaceAllString(s, "")
	if loc := clauseBreak.FindStringIndex(s); loc != nil {
		s = s[:loc[0]]
	}
	s = questionTail.ReplaceAllString(s, "")
	// A time word on the end is never part of a name, and leaving it there hid
	// the head noun: "Big news today" ended in "today" and so read as an
	// ordinary subject.
	s = timeTail.ReplaceAllString(s, "")
	s = strings.Trim(s, " \t?.!,:;\"'“”‘’")
	// Leading article, which is never part of a title.
	s = regexp.MustCompile(`(?i)^(a|an|the)\s+`).ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	if s == "" || len(strings.Fields(s)) > subjectMaxWords {
		return ""
	}
	// A bare question word survives the head pattern, which needs a word after
	// it to strip anything, so "why" comes through as its own subject.
	l := strings.ToLower(s)
	if notASubject[l] || liveSubject[l] {
		return ""
	}
	// The whole phrase isn't always the giveaway, "Big news today" reduces to
	// "Big news", which is in neither map. The head noun says what it's about,
	// and "News Corporation" survives because its head is the corporation.
	if f := strings.Fields(l); len(f) > 1 && liveSubject[f[len(f)-1]] {
		return ""
	}
	// Somebody talking, not the name of a thing. Checked after the maps
	// so a real one word subject still costs one map lookup and nothing else.
	if subjectURL.MatchString(s) || subjectPronoun.MatchString(s) || subjectVerb.MatchString(s) {
		return ""
	}
	if f := strings.Fields(l); len(f) > 0 && openingFiller[strings.Trim(f[0], ",.!?:;")] {
		return ""
	}
	return s
}

// Things a question names that are never its subject, since what is being
// asked is today's value. "what's the weather like" reduces to "weather".
var liveSubject = map[string]bool{
	"weather": true, "forecast": true, "temperature": true, "time": true, "date": true,
	"news": true, "score": true, "scores": true, "price": true, "prices": true,
	"stock": true, "stocks": true, "market": true, "markets": true, "traffic": true,
	"pollen": true, "aqi": true, "air quality": true, "exchange rate": true,
	"headline": true, "headlines": true, "story": true, "stories": true,
}

var notASubject = map[string]bool{
	"why": true, "how": true, "what": true, "who": true, "when": true, "where": true,
	"which": true, "it": true, "that": true, "this": true, "them": true, "they": true,
	// A message telling the assistant to do something is not a message about a
	// thing. "remember that i want to watch this" reduces to "remember".
	"remember": true, "forget": true, "note": true, "save": true, "keep": true,
	"add": true, "update": true, "delete": true, "ignore": true,
}
