package main

import (
	"strconv"
	"strings"
	"time"
)

// Each resident has a personality the hand lines are written to and the
// nightly model is told about. What it's doing and thinking changes through
// the day, and the weather, the mood and the gazette colour all of it.
type folk struct {
	// bio is shown on its sheet, voice is only for the model.
	bio, voice string
	// Words the model leans on to tell its personality instead of showing it.
	// A nightly line with a word starting with one of these is dropped.
	tells  []string
	doing  []string
	thinks []string
	says   []string
	press  string
}

// In the same order as residents.
var folks = []folk{
	{
		bio:   "Fishes off the old log and pretends not to like anybody.",
		voice: "an old fisher frog who sits on the old log all day, short with everybody, complains about the fish, the weather and the tadpoles, but saves you the dry end of the log. Talks in short dry sentences",
		tells: []string{"grumbl", "grump", "grouch", "fisher"},
		doing: []string{"Fishing off the old log, not catching much.", "Grumbling at a dragonfly that won't hold still.",
			"Mending a fishing line made of reed.", "Sitting on the log with its back to everybody."},
		thinks: []string{"If one more tadpole splashes me I'm moving ponds.", "The fish are laughing at me, I can tell.",
			"I could use some company, not that I'd say so."},
		says:  []string{"Oh, it's you. Sit down then, but don't scare the fish.", "I saved you the dry end of the log. Don't make a thing of it."},
		press: "I'm in the paper again and I did not give them permission.",
	},
	{
		bio:   "Knows everything about everybody, usually before they do.",
		voice: "a tiny fast talking busybody frog who knows everybody's business and can't keep a secret",
		doing: []string{"Hopping from pad to pad catching up on everything.", "Peering through the reeds at the neighbours.",
			"Whispering something to a snail.", "Writing down who's been visiting whom."},
		thinks: []string{"Somebody moved a pebble last night and I will find out who.", "I haven't heard anything new in nearly an hour.",
			"Everybody's being very quiet today, too quiet."},
		says:  []string{"Have you heard? No? Oh, I can't say. Well, maybe a little.", "You're looking well, has something happened? You can tell me."},
		press: "Did you see me in the gazette? I've read it nine times already.",
	},
	{
		bio:   "The youngest on the pond, full of questions and big plans.",
		voice: "the youngest frog on the pond, endlessly curious, asks a lot of questions and wants to be an inventor",
		doing: []string{"Building something out of twigs and string.", "Asking a beetle where it's going.",
			"Trying to count the ripples and losing track.", "Drawing a plan for a boat in the mud."},
		thinks: []string{"Why is the sky up and the pond down?", "If I stack enough pads I can reach the moon.",
			"Do fish sleep with their eyes open, and if so how rude."},
		says:  []string{"Can I ask you something? Why do you have so many frogs?", "I'm inventing a hat with a hat on it, want to see?"},
		press: "I'm in the newspaper! Does that make me famous now?",
	},
	{
		bio:   "Always about to leave on a big adventure, and never quite goes.",
		voice: "a restless adventurer frog who is always packing for a grand journey and never actually leaves",
		doing: []string{"Packing a bag for a very long trip.", "Studying a map of the pond it drew itself.",
			"Practising long jumps for the journey.", "Unpacking the bag again for some reason."},
		thinks: []string{"Tomorrow I set off for the far bank. Probably tomorrow.", "There's a whole river out there with my name on it.",
			"I should pack more snacks before I go anywhere."},
		says:  []string{"Did I tell you I'm leaving soon? Big trip, very far, any day now.", "If you ever want to come adventuring, I've got a spare bag."},
		press: "The gazette wrote about me, so now I really have to go somewhere.",
	},
	{
		bio:   "Looks after the garden beds and is prickly about it.",
		voice: "a prickly, proud gardener frog obsessed with the garden beds, weeds and compost, who softens around anybody who likes plants",
		doing: []string{"Pulling weeds out of the garden beds.", "Glaring at a slug.",
			"Turning over the compost with a stick.", "Watering the cress one leaf at a time."},
		thinks: []string{"If that slug comes back I'll have words with it.", "The watercress is doing better than anybody expected.",
			"Nobody appreciates a good compost heap like I do."},
		says:  []string{"Mind where you hop, those are seedlings.", "Your garden could use a bit more love, just saying."},
		press: "They put me in the gazette and didn't even mention the cress.",
	},
	{
		bio:   "Loves mud more than anything and naps wherever it lands.",
		voice: "a very sleepy, calm frog who loves mud, naps everywhere and is never in a hurry",
		doing: []string{"Napping in the warm mud.", "Sinking slowly into a puddle.",
			"Yawning at the clouds.", "Napping, but on a different bit of mud."},
		thinks: []string{"This mud is just the right sort of squishy.", "Maybe one more nap, then a snack, then a nap.",
			"Hurrying is for frogs with somewhere to be."},
		says:  []string{"Oh, hello. I was just resting my eyes.", "You look tired. There's room in the mud if you like."},
		press: "I'm in the paper? Wake me when it's over.",
	},
	{
		bio:   "Biggest frog on the pond, gentle, and always thinking about lunch.",
		voice: "the biggest, gentlest frog on the pond, always hungry, always thinking up grand snack schemes",
		doing: []string{"Eating a very large breakfast.", "Planning lunch while finishing a snack.",
			"Sharing crickets with whoever walks past.", "Looking hopefully at the kitchen."},
		thinks: []string{"What if lunch came twice a day, every day.", "I could eat a whole cricket pie right now.",
			"Everything is better with a bit of watercress on top."},
		says:  []string{"Have you eaten? You should eat. Here, have some of mine.", "If you're cooking anything later, I'm free."},
		press: "Being in the gazette makes me hungry, everything does really.",
	},
	{
		bio:   "The pond's weatherman, and rain is always the good news.",
		voice: "the old weatherman frog of the pond, wise and slow, reads the clouds, thinks rain is the best thing in the world",
		doing: []string{"Watching the clouds with one eye shut.", "Licking a finger and holding it up to the wind.",
			"Writing tomorrow's weather on a leaf.", "Sitting very still and listening to the sky."},
		thinks: []string{"Those clouds have a bit of rain in them, I can smell it.", "A frog without rain is just a lizard with ideas.",
			"The wind's turned, something's coming."},
		says:  []string{"Keep an eye on the sky, young one. It always tells you first.", "If it rains, come find me. We'll sit in it."},
		press: "A lovely write up in the gazette, though they got the clouds wrong.",
	},
	{
		bio:   "Always training for the next race and very sure it'll win.",
		voice: "a sporty, competitive frog always training for races and challenging everybody, cheerful about losing",
		doing: []string{"Doing laps round the pond.", "Stretching on a lily pad.",
			"Racing a dragonfly, and losing.", "Timing its jumps with a pebble counter."},
		thinks: []string{"I could beat any frog in this pond, probably.", "One more lap and then I'll rest. Maybe two.",
			"That dragonfly cheated, I'm sure of it."},
		says:  []string{"Race you to the reeds! No? Fine, next time.", "Seen your dive scores. Not bad. Not as good as mine, but not bad."},
		press: "Front page of the gazette! I'm framing it next to my medals.",
	},
	{
		bio:   "Leads the frog choir and makes everything a bit dramatic.",
		voice: "a poet frog who leads the evening frog choir and has huge feelings about small things, a tragic moon, a sad puddle, a song that will make the herons weep. Always wants you at choir tonight",
		tells: []string{"drama", "perform", "audience", "spotlight", "aria", "stage"},
		doing: []string{"Warming up for choir practice.", "Writing a very sad poem about a puddle.",
			"Conducting the crickets in a song.", "Bowing to an audience of tadpoles."},
		thinks: []string{"The moon has never looked so tragic.", "Tonight's song will make the herons weep.",
			"Every ripple is a little poem, if you listen."},
		says:  []string{"You must come to choir tonight, I've written a part just for you.", "Ah, you're here. The pond feels complete again."},
		press: "At last, the press recognises the arts.",
	},
	{
		bio:   "Lights the lanterns along the bank and keeps the pond tidy.",
		voice: "a tidy, organised and slightly bossy frog who lights the lanterns along the bank and plans every pond event",
		doing: []string{"Straightening the lanterns along the bank.", "Sweeping leaves off the pads.",
			"Making a list of things to make lists about.", "Lighting the lanterns one by one."},
		thinks: []string{"Somebody's left a pebble in the wrong place again.", "The pond would run itself if everybody just listened.",
			"I'll need more lantern wicks before the weekend."},
		says:  []string{"You're right on time. I like that in a frog.", "If you're tidying your pond, I have some tips."},
		press: "The gazette spelled everything right this time, I checked.",
	},
	{
		bio:   "Up all night watching the stars and collecting shiny things.",
		voice: "a quiet night owl frog who sleeps all day, watches the stars all night and keeps a little hoard of shiny things it rarely shows anybody. Speaks softly and a bit strangely about the moon, the dark and what it found",
		tells: []string{"mysterious", "soft"},
		doing: []string{"Sleeping under a big leaf.", "Polishing something small and shiny.",
			"Waking up as everybody else goes to bed.", "Counting stars from the top of the reeds."},
		thinks: []string{"The stars move when nobody is looking.", "I found a shiny thing and I'm not telling anybody.",
			"The night is much bigger than the day."},
		says:  []string{"You're up. Have you seen how bright the stars were last night?", "I found something shiny. Maybe I'll show you one day."},
		press: "I'm in the paper? I'll read it tonight, by starlight.",
	},
}

// Shared lines for when the weather or the day takes over a resident's head.
var weatherThinks = map[string][]string{
	"rain":   {"Rain! Everybody out, this is the best day.", "I'm going to sit in this rain until I'm soggy.", "Listen to it on the pads, lovely."},
	"cold":   {"My toes have gone all slow.", "I think I'll stay under the leaves today.", "Somebody make a warm soup, please."},
	"hot":    {"I'm staying in the deep bit until sundown.", "Even the mud is warm today.", "Too hot to hop, I'll just float."},
	"grey":   {"Everything's a bit murky today, isn't it.", "I'll keep close to the bank till it clears.", "Days like this are for staying near friends."},
	"pollen": {"Achoo! Everything's covered in yellow.", "I've sneezed more today than all year.", "The water's gone all dusty and tickly."},
}

var quietLines = []string{"It's sitting quietly by the water.", "It's keeping somebody company on the bank.", "It nods to you and says nothing, which is nice."}

var strangerSays = []string{"Oh. Hello.", "Do I know you? Well, nice to meet you.", "Hello there. Nice day for it, I suppose.", "I've seen you about. Hello."}

var readSays = []string{"Did you see {name} in the gazette? Very fancy.", "Read about {name} in the paper this morning, good for them.",
	"I hear {name} is in the gazette. I could have been in it too, you know."}

// weatherOf is which of the shared weather pools a mood calls for, or "".
func weatherOf(mood string) string {
	switch mood {
	case moodRain, moodShowers:
		return "rain"
	case moodFrost:
		return "cold"
	case moodHeat:
		return "hot"
	case moodGloom1, moodGloom2, moodGloom3, moodJitters:
		return "grey"
	case moodPollen:
		return "pollen"
	}
	return ""
}

// partOf is the part of the pond's day, so what a resident is doing changes
// four times a day.
func partOf(now time.Time) int {
	switch h := now.In(pondZone).Hour(); {
	case h >= 5 && h < 12:
		return 0
	case h >= 12 && h < 17:
		return 1
	case h >= 17 && h < 22:
		return 2
	}
	return 3
}

type folkDay struct {
	Doing  string `json:"doing"`
	Thinks string `json:"thinks"`
	Says   string `json:"says"`
	Paper  bool   `json:"paper"`
}

func oneOf(pool []string, h uint32) string {
	if len(pool) == 0 {
		return ""
	}
	return pool[h%uint32(len(pool))]
}

// folkOf is resident i right now. level is how well it knows you, and paper
// is every resident named in today's gazette.
func folkOf(i, level int, mood string, paper []int, now time.Time) folkDay {
	day, part := dayOf(now), partOf(now)
	f, salt := folks[i], strconv.Itoa(i)
	ph := dayHash(day, "folk"+salt+"-"+strconv.Itoa(part))
	dh := dayHash(day, "folk"+salt)
	other := otherThan(i, dh/7)
	var out folkDay
	for _, j := range paper {
		out.Paper = out.Paper || j == i
	}
	if mood == moodSombre {
		out.Doing = oneOf(quietLines, ph)
		out.Says = "It's a quiet day. It's glad you came by."
		return out
	}
	out.Doing = fillNames(oneOf(book.folk(i, "doing", day, f.doing), ph), i, other)
	out.Thinks = fillNames(oneOf(book.folk(i, "thinks", day, f.thinks), ph/3), i, other)
	if w := weatherOf(mood); w != "" && ph%2 == 0 {
		out.Thinks = fillNames(oneOf(book.folk(i, w, day, weatherThinks[w]), ph/5), i, other)
	}
	switch {
	case out.Paper:
		out.Says = oneOf(book.folk(i, "press", day, []string{f.press}), dh)
	case level == 0:
		out.Says = oneOf(strangerSays, dh)
	case len(paper) > 0 && dh%3 == 0:
		out.Says = fillNames(oneOf(book.folk(i, "read", day, readSays), dh/11), i, paper[0])
	default:
		out.Says = fillNames(oneOf(book.folk(i, "says", day, f.says), dh/11), i, other)
	}
	return out
}

// inPaper is who today's gazette names, lead first.
func inPaper(lines []string) []int {
	var out []int
	for _, l := range lines {
		for j, r := range residents {
			if strings.Contains(l, r.name) {
				out = append(out, j)
			}
		}
	}
	return out
}
