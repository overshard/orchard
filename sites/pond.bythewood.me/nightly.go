package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Once a night the local model writes extra lines for tomorrow, every mood
// and every kind. It only ever sees mood names and slot names, never a reading,
// and every line it writes is checked here before a frog can say it.

// tales is one night's lines. Greeting and the hobbies are by nature, so a
// SHY frog and a BOLD one don't sound alike.
type tales struct {
	Day      string                         `json:"day"`
	Greeting map[string]map[string][]string `json:"greeting"`
	Watching map[string][]string            `json:"watching"`
	Playing  map[string][]string            `json:"playing"`
	Dream    []string                       `json:"dream"`
	Gossip   []string                       `json:"gossip"`
	Gazette  map[string][]string            `json:"gazette"`
	// By resident name and then by what the line is for, doing or press.
	Folk map[string]map[string][]string `json:"folk,omitempty"`
}

type taleBook struct{ cur atomic.Pointer[tales] }

// book holds the latest night that passed. A night older than two days is
// ignored, and the hand written pools carry on alone.
var book taleBook

func (b *taleBook) current(day string) *tales {
	t := b.cur.Load()
	if t == nil || t.Day > day || dayNumber(day)-dayNumber(t.Day) > 1 {
		return nil
	}
	return t
}

// pool is the hand written lines plus whatever the model added for kind and
// key. Greeting and the hobbies go through natured instead.
func (b *taleBook) pool(kind, key, day string, hand []string) []string {
	t := b.current(day)
	if t == nil {
		return hand
	}
	var extra []string
	switch kind {
	case "dream":
		extra = t.Dream
	case "gossip":
		extra = t.Gossip
	case "gazette":
		extra = t.Gazette[key]
	}
	if len(extra) == 0 {
		return hand
	}
	return append(append([]string{}, hand...), extra...)
}

func (b *taleBook) natured(kind, key, nature, day string, hand []string) []string {
	t := b.current(day)
	if t == nil {
		return hand
	}
	var extra []string
	switch kind {
	case "greeting":
		extra = t.Greeting[key][nature]
	case "watching":
		extra = t.Watching[nature]
	case "playing":
		extra = t.Playing[nature]
	}
	if len(extra) == 0 {
		return hand
	}
	return append(append([]string{}, hand...), extra...)
}

// folk is resident i's hand lines for field plus the model's.
func (b *taleBook) folk(i int, field, day string, hand []string) []string {
	t := b.current(day)
	if t == nil {
		return hand
	}
	extra := t.Folk[residents[i].name][field]
	if len(extra) == 0 {
		return hand
	}
	return append(append([]string{}, hand...), extra...)
}

// ---------------------------------------------------------------- checking

const (
	lineMax = 110
	// A thought sits in a bubble over a small card.
	thoughtMax = 80
)

// A resident's line can't name a neighbour or a shopkeeper, even at the start
// of a sentence where a capital is allowed. Names that are plain words aren't here.
var folkNames = map[string]bool{"sedge": true, "tully": true, "pip": true, "bramble": true,
	"clover": true, "tansy": true, "wren": true, "brindle": true}

// The words a line in the first person may capitalise past its first.
var firstPerson = map[string]bool{"I": true, "I'm": true, "I've": true, "I'll": true, "I'd": true}

var slotRe = regexp.MustCompile(`\{[a-z]+\}`)

// A word starting with one of bannedPrefix, or equal to one of bannedWord,
// drops the line. The market and money words keep the real day's numbers off
// the pond too.
var bannedPrefix = []string{
	"kill", "murder", "blood", "suicid", "nazi", "terror", "bomb", "cancer",
	"drunk", "drug", "naked", "idiot", "stupid", "stock", "market", "percent",
	"dollar", "recession", "inflation", "nasdaq", "bitcoin", "crypto",
	"president", "election", "trump", "biden",
}

var bannedWord = map[string]bool{
	"dead": true, "death": true, "die": true, "dies": true, "died": true, "dying": true,
	"war": true, "wars": true, "gun": true, "guns": true, "sex": true, "beer": true,
	"wine": true, "hate": true, "hell": true, "damn": true, "god": true, "jesus": true,
	"fed": true, "dow": true, "news": true, "ai": true, "model": true, "price": true,
	"prices": true, "money": true, "crash": true,
}

// checkLine says whether a line the model wrote can be used, given the slots
// it may carry and the one it has to. seen is lines used in recent weeks.
func checkLine(l string, a taleAsk, seen map[string]bool) (string, bool) {
	allowed, must := a.allowed, a.must
	l = strings.TrimSpace(l)
	l = strings.TrimSpace(strings.Trim(l, `"'`))
	if l != "" && l[0] >= 'a' && l[0] <= 'z' {
		l = strings.ToUpper(l[:1]) + l[1:]
	}
	if a.kind == "gazette" {
		l = strings.TrimSuffix(l, ".")
	} else if l != "" && !strings.ContainsAny(l[len(l)-1:], ".!?") {
		l += "."
	}
	// Asked for a slot in braces the model leaves it out, so it writes the
	// plain words and the slot goes in here.
	if a.stand != "" && !strings.Contains(l, a.must) && strings.Count(l, a.stand) == 1 {
		l = strings.Replace(l, a.stand, a.must, 1)
	}
	max := lineMax
	if a.short {
		max = thoughtMax
	}
	if l == "" || len(l) > max || strings.ContainsAny(l, "0123456789%$#*_<>\n\\|;—–") {
		return "", false
	}
	for _, s := range slotRe.FindAllString(l, -1) {
		ok := false
		for _, a := range allowed {
			ok = ok || s == a
		}
		if !ok {
			return "", false
		}
	}
	if strings.Count(l, "{") != len(slotRe.FindAllString(l, -1)) || strings.Count(l, "}") != strings.Count(l, "{") {
		return "", false
	}
	if must != "" && strings.Count(l, must) != 1 {
		return "", false
	}
	for _, s := range a.avoidStart {
		if strings.HasPrefix(l, s+" ") {
			return "", false
		}
	}
	bare := strings.Fields(slotRe.ReplaceAllString(l, "x"))
	if len(a.starts) > 0 {
		ok := false
		for _, s := range a.starts {
			ok = ok || strings.HasPrefix(l, s+" ")
		}
		if !ok {
			return "", false
		}
	}
	if a.gerund && !strings.HasSuffix(strings.ToLower(strings.Trim(bare[0], ",.!?")), "ing") {
		return "", false
	}
	// The model makes up names, and the only names a line may carry are slots.
	for k, w := range bare[1:] {
		if a.firstPerson && (firstPerson[strings.Trim(w, ",.!?")] || strings.ContainsAny(bare[k][len(bare[k])-1:], ".!?")) {
			continue
		}
		if r := w[0]; r >= 'A' && r <= 'Z' {
			return "", false
		}
	}
	words := strings.FieldsFunc(strings.ToLower(slotRe.ReplaceAllString(l, " ")), func(r rune) bool {
		return !(r >= 'a' && r <= 'z')
	})
	if len(words) < 4 {
		return "", false
	}
	for _, w := range words {
		if a.kind == "folk" && folkNames[w] {
			return "", false
		}
		if bannedWord[w] || a.kind == "gazette" && (w == "you" || w == "your") {
			return "", false
		}
		for _, b := range bannedPrefix {
			if strings.HasPrefix(w, b) {
				return "", false
			}
		}
	}
	key := strings.Join(words, " ")
	if seen[key] {
		return "", false
	}
	seen[key] = true
	return l, true
}

// ---------------------------------------------------------------- asking

var taleNatures = []string{"BOLD", "SHY", "NIGHT OWL", "GLUTTON", "SINGER", "DRIFTER"}

var moodWords = map[string]string{
	moodPlain:   "an ordinary, uneventful day",
	moodBright:  "a bright, cheerful day where everything feels like it's going right",
	moodGloom1:  "a slightly grey, murky day",
	moodGloom2:  "a grey, gloomy day where the frog stays low and wants company",
	moodGloom3:  "a heavy, dark day where the frog hid in the mud but came up to see you",
	moodJitters: "a windy, jumpy day where everybody is a bit nervous",
	moodRain:    "a rainy day, which frogs love more than anything",
	moodShowers: "a dry day with rain coming tomorrow, which the frog is excited about",
	moodFrost:   "a frosty, cold day where the frog is sleepy and slow",
	moodHeat:    "a hot day where the frog keeps cool in the water",
	moodPollen:  "a day with pollen all over the water, lots of sneezing",
	moodSombre:  "a quiet, sombre day. Keep it gentle and calm, no jokes at all",
}

const taleSystem = `You write short lines for a cosy pixel art game about pet frogs living in a pond.
Every line is one plain sentence about the frog, in the third person, starting with It or It's and calling the frog "it", never by a name or its nature. Speak of the owner as "you".
Warm, gentle, a little silly, for all ages. Plain words, contractions are fine.
Never write numbers, prices, money, the stock market, news, politics, people's names, brands, violence or anything sad about death.
Never use quotation marks, dashes, semicolons or emoji. Each line under 100 characters.
Never make up a name for anybody or anything. Write in sentence case, with a capital letter only at the start.
Use a slot like {show} only when asked to, written exactly with its braces and in lower case.`

type taleAsk struct {
	kind, key, prompt string
	allowed           []string
	must              string
	// A line has to open with one of these words.
	starts []string
	// A line can't open with one of these words.
	avoidStart []string
	// The plain words the model writes where must goes.
	stand string
	n     int
	// When set the answer is an object of lines by nature.
	natured bool
	// A resident's ask, whose answer is an object of lines by folkFields.
	who                        int
	firstPerson, gerund, short bool
}

func taleAsks() []taleAsk {
	var asks []taleAsk
	moods := make([]string, 0, len(moodWords))
	for m := range moodWords {
		moods = append(moods, m)
	}
	sort.Strings(moods)
	natures := "BOLD (brave, loud), SHY (timid, sweet), NIGHT OWL (sleepy by day, lively at night), GLUTTON (always thinking about food), SINGER (always humming or croaking a tune), DRIFTER (dreamy, floats about)"
	for _, m := range moods {
		asks = append(asks, taleAsk{kind: "greeting", key: m, natured: true, n: 3, starts: []string{"It", "It's"},
			prompt: "Today is " + moodWords[m] + ". Write lines saying what the frog has been up to today or how it feels, as said to its owner who just arrived. " +
				"Write separate lines for each frog nature: " + natures + ". The nature only changes how it sounds, never name it."})
	}
	asks = append(asks,
		taleAsk{kind: "watching", natured: true, n: 2, starts: []string{"It", "It's"}, allowed: []string{"{show}"}, must: "{show}",
			stand:  "the show",
			prompt: "Write lines about the frog watching a TV show, calling it exactly the show, like: It's halfway through the show and won't say what happens. Write separate lines for each frog nature: " + natures + "."},
		taleAsk{kind: "playing", natured: true, n: 2, starts: []string{"It", "It's"}, allowed: []string{"{game}"}, must: "{game}",
			stand:  "the game",
			prompt: "Write lines about the frog playing a video game, calling it exactly the game, like: It keeps saying one more go of the game. Write separate lines for each frog nature: " + natures + "."},
		taleAsk{kind: "dream", n: 20, starts: []string{"It"}, allowed: []string{"{resident}"},
			prompt: "Write lines about what the frog dreamt last night, each starting with \"It dreamt\". Dreams can be weird and surreal. Some can mention a neighbour frog, written as {resident}."},
		taleAsk{kind: "gossip", n: 20, starts: []string{"Heard", "Says", "Thinks", "Swears"}, allowed: []string{"{name}"}, must: "{name}",
			prompt: "A neighbour frog is passing on a harmless, silly rumour about another frog, written as {name}. Start each with Heard, Says or Thinks, with no subject before it, and say what {name} did, like: Heard {name} has been practising a new croak. Or: Says {name} ate two dinners and denied the second one."},
	)
	for _, tone := range []string{"fair", "grey", "quiet"} {
		feel := map[string]string{"fair": "a good, ordinary day", "grey": "a grey, slow day where everybody stays close", "quiet": "a quiet, sombre day, gentle with no jokes"}[tone]
		asks = append(asks, taleAsk{kind: "gazette", key: tone, n: 10, avoidStart: []string{"It", "It's"}, allowed: []string{"{resident}", "{other}"},
			prompt: "Write newspaper headlines for the Pond Gazette about goings on around the pond, on " + feel + ". Sentence case with no full stop, like: Choir practice runs late again, nobody complains. The only names allowed are {resident} and {other} for one or two neighbour frogs. Keep it about frogs, the water, the reeds, food and the shops."})
	}
	for i := range residents {
		asks = append(asks, taleAsk{kind: "folk", who: i, n: 3, prompt: folkPrompt(i)})
	}
	return asks
}

// What the model writes for each resident, read by folkOf.
var folkFields = []string{"doing", "thinks", "says", "rain", "cold", "hot", "grey", "pollen", "press", "read"}

const folkSystem = `You write short lines for one character in a cosy pixel art game about frogs living round a pond.
Warm, gentle, a little silly, for all ages. Plain words, contractions are fine. The character's personality should come through in every line.
Never write numbers, prices, money, the stock market, news, politics, people's names, brands, violence or anything sad about death.
Never use the character's own name. Never make up a name for anybody or anything, the only name allowed is the slot {name} for another neighbour frog, written exactly with its braces.
Never use quotation marks, dashes, semicolons or emoji. Write in sentence case, with a capital letter only at the start and on I.`

func folkPrompt(i int) string {
	return "The character is a frog who is " + folks[i].voice + ". It has a lily pad by the pond, there's a garden, a kitchen, shops, a choir and a little newspaper called the gazette. Write these, each line under 80 characters:\n" +
		"doing: what it's busy doing right now, starting with a verb ending in ing, like: Sweeping leaves off the pads.\n" +
		"thinks: a passing thought in its own head, in the first person, like: I wonder if the fish are talking about me.\n" +
		"says: something it says to the player who just came over, in the first person, talking to you.\n" +
		"rain, cold, hot, grey, pollen: a thought in the first person on a rainy day, a frosty day, a hot day, a grey gloomy day, and a day with pollen all over the water.\n" +
		"press: what it says to you about being in today's gazette, in the first person.\n" +
		"read: what it says to you about {name} being in today's gazette, in the first person, with {name} exactly once."
}

// folkRule is how a line for one of folkFields is checked.
func folkRule(field string) (taleAsk, bool) {
	a := taleAsk{kind: "folk", allowed: []string{"{name}"}, firstPerson: true}
	switch field {
	case "doing":
		a.gerund, a.firstPerson, a.short = true, false, true
	case "thinks", "rain", "cold", "hot", "grey", "pollen":
		a.short = true
	case "says", "press":
	case "read":
		a.must = "{name}"
	default:
		return a, false
	}
	return a, true
}

// ---------------------------------------------------------------- the model

type model struct {
	url, key string
	http     *http.Client
}

func newModel(url, key string) *model {
	if key == "" {
		return nil
	}
	return &model{url: strings.TrimRight(url, "/"), key: key, http: &http.Client{Timeout: 5 * time.Minute}}
}

var errCardBusy = errors.New("the card is in use")

func (m *model) lines(ctx context.Context, a taleAsk) (any, error) {
	arr := map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": a.n, "maxItems": a.n}
	keys, system := []string{"lines"}, taleSystem
	if a.natured {
		keys = taleNatures
	} else if a.kind == "folk" {
		keys, system = folkFields, folkSystem
	}
	props := map[string]any{}
	for _, k := range keys {
		props[k] = arr
	}
	schema := map[string]any{"type": "object", "properties": props, "required": keys, "additionalProperties": false}
	body, err := json.Marshal(map[string]any{
		"model": "local",
		"messages": []map[string]string{{"role": "system", "content": system},
			{"role": "user", "content": a.prompt + fmt.Sprintf(" Write %d lines.", a.n)}},
		"temperature":          0.9,
		"top_p":                0.95,
		"max_tokens":           3500,
		"chat_template_kwargs": map[string]any{"enable_thinking": false},
		"response_format": map[string]any{"type": "json_schema",
			"json_schema": map[string]any{"name": "lines", "strict": true, "schema": schema}},
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.url+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+m.key)
	req.Header.Set("X-Background", "1")
	resp, err := m.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusServiceUnavailable {
		return nil, errCardBusy
	}
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("model: http %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	var r struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&r); err != nil {
		return nil, err
	}
	if len(r.Choices) == 0 {
		return nil, errors.New("model: no choices")
	}
	text := r.Choices[0].Message.Content
	if i := strings.Index(text, "{"); i > 0 {
		text = text[i:]
	}
	if a.natured || a.kind == "folk" {
		var out map[string][]string
		return out, json.Unmarshal([]byte(text), &out)
	}
	var out struct {
		Lines []string `json:"lines"`
	}
	err = json.Unmarshal([]byte(text), &out)
	return out.Lines, err
}

type gpuState struct {
	Busy    bool   `json:"busy"`
	Reason  string `json:"reason"`
	Loaded  bool   `json:"loaded"`
	RetryAt int64  `json:"retry_at"`
}

func (m *model) call(ctx context.Context, method, path string, into any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, m.url+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+m.key)
	resp, err := m.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("model: %s %s: http %d", method, path, resp.StatusCode)
	}
	if into == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(into)
}

// ---------------------------------------------------------------- the night

// The run starts after dash's last brief and can catch up until morning. The
// lines are for the pond day that starts after it.
const (
	nightFrom = 22*60 + 30
	nightTo   = 6 * 60
	// The desktop is also where he games, so a run waits for this much free.
	nightMem   = 3 << 30
	nightRetry = 30 * time.Minute
	seenDays   = 21
)

type nightly struct {
	m    *model
	dir  string
	next time.Time
	// memAvailable unless a test sets it.
	free func() (int64, bool)
}

func taleDayFor(now time.Time) string { return dayOf(now.Add(6 * time.Hour)) }

func inNight(now time.Time) bool {
	n := now.In(pondZone)
	min := n.Hour()*60 + n.Minute()
	return min >= nightFrom || min < nightTo
}

func (n *nightly) path(day string) string { return filepath.Join(n.dir, "tales-"+day+".json") }

// loadTales puts the newest night on disk in the book.
func loadTales(dir string) {
	files, _ := filepath.Glob(filepath.Join(dir, "tales-2*.json"))
	sort.Strings(files)
	for i := len(files) - 1; i >= 0; i-- {
		raw, err := os.ReadFile(files[i])
		if err != nil {
			continue
		}
		var t tales
		if json.Unmarshal(raw, &t) == nil && t.Day != "" {
			book.cur.Store(&t)
			return
		}
	}
}

func (n *nightly) run(ctx context.Context) {
	if n.m == nil {
		slog.Info("no LLM_KEY, the nightly lines are off")
		return
	}
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		n.tick(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (n *nightly) tick(ctx context.Context, now time.Time) {
	if !inNight(now) || now.Before(n.next) {
		return
	}
	day := taleDayFor(now)
	if _, err := os.Stat(n.path(day)); err == nil {
		return
	}
	if n.free == nil {
		n.free = memAvailable
	}
	if free, ok := n.free(); ok && free < nightMem {
		n.next = now.Add(nightRetry)
		slog.Info("nightly lines waiting on memory", slog.Int64("free_mib", free>>20))
		return
	}
	var gpu gpuState
	if err := n.m.call(ctx, http.MethodGet, "/v1/gpu", &gpu); err != nil {
		n.next = now.Add(nightRetry)
		slog.Warn("nightly lines could not ask about the card", slog.Any("err", err))
		return
	}
	if gpu.Busy {
		n.next = now.Add(nightRetry)
		if gpu.RetryAt > 0 {
			n.next = time.Unix(gpu.RetryAt, 0)
		}
		slog.Info("nightly lines deferred", slog.String("reason", gpu.Reason), slog.Time("next", n.next))
		return
	}
	if !gpu.Loaded {
		defer func() {
			if err := n.m.call(context.WithoutCancel(ctx), http.MethodPost, "/v1/unload", nil); err != nil {
				slog.Warn("nightly lines could not unload", slog.Any("err", err))
			}
		}()
	}
	started := time.Now()
	t, kept, dropped, err := n.write(ctx, day)
	if err != nil {
		n.next = now.Add(nightRetry)
		slog.Warn("nightly lines failed", slog.Any("err", err))
		return
	}
	book.cur.Store(t)
	slog.Info("nightly lines written", slog.String("day", day), slog.Int("kept", kept), slog.Int("dropped", dropped),
		slog.Duration("took", time.Since(started).Round(time.Second)))
}

// write asks for every kind, keeps what passes and saves the night. A kind
// that fails is left empty and its hand written pool stands alone.
func (n *nightly) write(ctx context.Context, day string) (*tales, int, int, error) {
	seen := n.loadSeen(day)
	t := &tales{Day: day, Greeting: map[string]map[string][]string{}, Watching: map[string][]string{},
		Playing: map[string][]string{}, Gazette: map[string][]string{}, Folk: map[string]map[string][]string{}}
	kept, dropped, failed := 0, 0, 0
	var drops []string
	perKind := map[string]int{}
	defer func() {
		if len(drops) > 0 {
			slog.Info("nightly lines dropped", slog.Any("sample", drops))
		}
	}()
	keep := func(a taleAsk, ls []string) []string {
		var out []string
		for _, l := range ls {
			if c, ok := checkLine(l, a, seen); ok {
				out = append(out, c)
				kept++
			} else {
				dropped++
				if perKind[a.kind]++; perKind[a.kind] <= 3 {
					drops = append(drops, a.kind+": "+l)
				}
			}
		}
		return out
	}
	for _, a := range taleAsks() {
		got, err := n.m.lines(ctx, a)
		if errors.Is(err, errCardBusy) || ctx.Err() != nil {
			return nil, 0, 0, errCardBusy
		}
		if err != nil {
			failed++
			slog.Warn("nightly lines kind failed", slog.String("kind", a.kind), slog.String("key", a.key), slog.Any("err", err))
			continue
		}
		switch v := got.(type) {
		case map[string][]string:
			by := map[string][]string{}
			for key, ls := range v {
				rule, known := a, true
				if a.kind == "folk" {
					rule, known = folkRule(key)
				}
				if !known {
					continue
				}
				if ok := keep(rule, ls); len(ok) > 0 {
					by[key] = ok
				}
			}
			switch a.kind {
			case "folk":
				t.Folk[residents[a.who].name] = by
			case "greeting":
				t.Greeting[a.key] = by
			case "watching":
				t.Watching = by
			case "playing":
				t.Playing = by
			}
		case []string:
			ok := keep(a, v)
			switch a.kind {
			case "dream":
				t.Dream = ok
			case "gossip":
				t.Gossip = ok
			case "gazette":
				t.Gazette[a.key] = ok
			}
		}
	}
	if kept == 0 {
		return nil, 0, dropped, fmt.Errorf("nothing passed, %d kinds failed", failed)
	}
	raw, err := json.MarshalIndent(t, "", " ")
	if err != nil {
		return nil, 0, 0, err
	}
	if err := os.WriteFile(n.path(day)+".tmp", raw, 0o644); err != nil {
		return nil, 0, 0, err
	}
	if err := os.Rename(n.path(day)+".tmp", n.path(day)); err != nil {
		return nil, 0, 0, err
	}
	n.prune(day)
	return t, kept, dropped, nil
}

// loadSeen is every line of the last few weeks' nights, so a line doesn't
// come round again for a while.
func (n *nightly) loadSeen(day string) map[string]bool {
	seen := map[string]bool{}
	files, _ := filepath.Glob(filepath.Join(n.dir, "tales-2*.json"))
	for _, f := range files {
		d := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), "tales-"), ".json")
		if d >= day || dayNumber(day)-dayNumber(d) > seenDays {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var t tales
		if json.Unmarshal(raw, &t) != nil {
			continue
		}
		mark := func(ls []string) {
			for _, l := range ls {
				checkLine(l, taleAsk{allowed: []string{"{show}", "{game}", "{resident}", "{other}", "{name}"}, firstPerson: true}, seen)
			}
		}
		for _, by := range t.Greeting {
			for _, ls := range by {
				mark(ls)
			}
		}
		for _, by := range []map[string][]string{t.Watching, t.Playing, t.Gazette} {
			for _, ls := range by {
				mark(ls)
			}
		}
		mark(t.Dream)
		mark(t.Gossip)
		for _, by := range t.Folk {
			for _, ls := range by {
				mark(ls)
			}
		}
	}
	return seen
}

// prune keeps the files the dedupe reads and drops the rest.
func (n *nightly) prune(day string) {
	files, _ := filepath.Glob(filepath.Join(n.dir, "tales-2*.json"))
	for _, f := range files {
		d := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), "tales-"), ".json")
		if dayNumber(day)-dayNumber(d) > seenDays {
			os.Remove(f)
		}
	}
}

// memAvailable reads the VM's free memory, which a container sees whole.
func memAvailable() (int64, bool) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) >= 2 && fs[0] == "MemAvailable:" {
			kb, err := strconv.ParseInt(fs[1], 10, 64)
			return kb << 10, err == nil
		}
	}
	return 0, false
}
