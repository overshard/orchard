package main

import "strings"

// Shape is what kind of answer a question wants. The prototype wrote every
// answer as four to eight sentences of prose, which is right for a factual
// question and wrong for a recipe, where the ingredients and the order of the
// steps are the answer.
type Shape string

const (
	ShapeFactual    Shape = "factual"
	ShapeRecipe     Shape = "recipe"
	ShapeHowTo      Shape = "howto"
	ShapeComparison Shape = "comparison"
	ShapeNews       Shape = "news"
	ShapeStatus     Shape = "status"
	ShapeCode       Shape = "code"
	ShapeUpcoming   Shape = "upcoming"
)

// shapeEnum is what the plan step offers the model, and every entry has a
// contract below. A shape the planner can pick and the contract map does not
// hold falls back to prose, which is the wrong format quietly rather than
// loudly, so a test walks this list.
var shapeEnum = []Shape{
	ShapeFactual, ShapeRecipe, ShapeHowTo, ShapeComparison,
	ShapeNews, ShapeStatus, ShapeCode, ShapeUpcoming,
}

// Contract is the format instruction handed to the synthesis step, and the
// passage budget the shape needs. A recipe needs more of the page than a
// factual answer does, because ingredients and method are usually far apart.
type Contract struct {
	Shape       Shape
	Instruction string
	MaxPassages int
	PerSource   int
	MaxTokens   int

	// Reminder is the one rule that has to survive to the end, repeated after
	// the passages rather than left in the system prompt. A model reads the
	// beginning and the end of what it was given and is worst in the middle,
	// and by the time this one is generating, the instruction it needs most is
	// several thousand tokens behind it.
	Reminder string
}

// answerFirst is prepended to every shape. The rule Isaac stated: answer the
// question, then the few facts worth skimming, and only then the detail, which
// is a bonus rather than something forced on a reader who wanted one number.
// The closing section used to be "the fuller explanation", and a 4B given that
// instruction after it has already stated the facts writes the list again in
// prose. That restatement is generated from a weaker signal than the bullets
// were, so it contradicted them often enough to be the worst thing on the page:
// a wrong sentence sitting directly under the right one, in a site whose whole
// claim is that every sentence is checked.
//
// So the rule is the same one dash's weather alert landed on. Say a thing once,
// and a section that has nothing new to add does not get written.
// Two rules every shape needs, learned off a recipe that came back with a
// citation after every noun and a line reading "Total Time: Not specified in
// passages". The reader is not in the room with the pipeline and does not know
// what a passage is.
const houseStyle = "Put at most one citation at the end of a sentence or a list item, never in the middle of one and never more than one. " +
	"Never refer to the answer itself, so no \"this response\", \"this answer\" or \"below you will find\". Write the thing rather than describing it. " +
	"Write for someone who cannot see your sources and does not know how you work. " +
	"Never mention passages, sources, context, or what you were given, and never write that something was not specified. " +
	"If a detail is missing, leave the line out entirely rather than writing a line saying it is missing. "

// Passages describe the future as of when they were written, and a model
// repeating their tense says a thing is "targeted for April" in September. The
// date is in the ambient context and the model still did it, so both
// time-sensitive shapes have to be told to compare.
const datesArePast = "Today's date is given above, so check every date you write against it. " +
	"A date that has already passed is not a target, a plan or something upcoming, whatever tense the passage uses, because the passage was written before it. " +
	"If a passage says something was scheduled for a date that has now passed and nothing tells you what happened, say plainly that you do not know how it went rather than repeating the plan. " +
	"Never describe a past date as a future one. " +
	"The same goes for tense: a passage saying something is happening now, is currently underway, or is in progress means when that passage was written, not today. " +
	"If that was weeks or months ago, say what it was doing then and give the date, rather than saying it is doing it now. "

// The mirror of datesArePast, and the half that was missing. Every shape above
// describes the past or a standing fact, so "when is the next Liverpool game"
// was classified news, whose contract opens by saying the question is about
// what already happened, and the answer was four fixtures that had been played
// followed by a line admitting it did not have the next one.
const datesAreFuture = "The question asks about something that has not happened yet, so only a date that is today or later can answer it. " +
	"Today's date is given above. Check every date you are about to write against it. " +
	"A match, launch, release or meeting dated before today has already happened and is not the answer, whatever tense the passage uses. " +
	"If nothing in the passages is dated today or later, say in the first sentence that you do not have it, and stop. " +
	"Listing what has already happened does not answer a question about what is next, so do not do it. "

const answerFirst = houseStyle +
	"Answer the question directly in the first sentence, before anything else. " +
	"Then give the few facts that matter as a short markdown bullet list, bolding the value in each one. " +
	"Keep that part tight, a reader should get what they asked for without scrolling. " +
	"Stop there unless you have something the bullets do not already carry. " +
	"Never restate, summarise or conclude, and never write a closing paragraph that repeats the list in prose. " +
	"If a caveat, a disagreement between sources, or a piece of background genuinely adds something, write it as one or two sentences that mention no fact already in a bullet. " +
	"Never open with background, a definition, or a restatement of the question, and never make someone read to the second paragraph to find what they asked for."

var contracts = map[Shape]Contract{
	ShapeFactual: {
		Shape: ShapeFactual,
		Instruction: strings.Join([]string{
			answerFirst,
			"For a question with one answer, say it plainly and then list the numbers, comparisons and where it is.",
			"Three to six bullets is usually right.",
		}, " "),
		MaxPassages: 12, PerSource: 3, MaxTokens: 700,
	},
	ShapeRecipe: {
		Shape: ShapeRecipe,
		Instruction: strings.Join([]string{
			houseStyle,
			"Write an actual recipe, not a description of one.",
			"Start with one sentence saying what it makes, then a line for yield and a line for total time, each only if you actually have it. Omit the line otherwise, and never write that it was not given.",
			"Then a `## Ingredients` heading with a markdown bullet list, one ingredient per line.",
			"Copy each ingredient line exactly as it is written, keeping its quantity, like `2 tbsp butter` or `8 large eggs`.",
			"Never invent, guess or carry over a quantity. If a line gives no amount, write the ingredient on its own.",
			"The yield is not a quantity for anything. A recipe making 15 burritos does not use 15 of each ingredient.",
			"Then a `## Method` heading with a numbered list, one step per line, in order.",
			"Bold ingredient names in the ingredient list and bold temperatures and times in the steps.",
			"Follow one recipe rather than blending several, and prefer the one that comes with quantities.",
			"The method may only use ingredients that are in your ingredient list. If a step needs something the list does not have, either add it to the list with its quantity or leave the step out.",
			"Notes, substitutions and storage come after the method, only when the passages give them, and they never repeat an ingredient or a step already listed.",
			"Cite once at the end of each step, not after each ingredient in it.",
		}, " "),
		MaxPassages: 18, PerSource: 6, MaxTokens: 1400,
	},
	ShapeHowTo: {
		Shape: ShapeHowTo,
		Instruction: strings.Join([]string{
			"Open with one sentence saying what the steps achieve.",
			"Then a numbered list of steps in the order they must be done, one action per step, written as an instruction.",
			"Bold any command, file name, or exact value the reader has to type.",
			"Cite the passage each step came from, once at the end of it.",
			"Caveats and alternatives come after the steps and only when they add something, never a summary of the steps just given.",
		}, " "),
		MaxPassages: 14, PerSource: 4, MaxTokens: 1000,
	},
	ShapeComparison: {
		Shape: ShapeComparison,
		Instruction: strings.Join([]string{
			"Open with one sentence naming which option suits which case.",
			"Then a markdown bullet list with one line per point of difference, each naming both sides, bolding the option name at the start of the line.",
			"Cite the passage each point of difference came from, once at the end of its line.",
			"Then one sentence on the tradeoff that actually decides it, and stop.",
			"Do not close by restating the differences already listed.",
		}, " "),
		MaxPassages: 14, PerSource: 4, MaxTokens: 900,
	},
	// "What is the status of the Lindsay Clancy case" is not the same question
	// as "what happened". It asks where something stands now, which means the
	// newest source is the one that matters and an old one is misleading rather
	// than merely incomplete.
	ShapeStatus: {
		Shape: ShapeStatus,
		Instruction: strings.Join([]string{
			houseStyle,
			datesArePast,
			"Open with one sentence saying where this stands and the date it is current to.",
			"Then a markdown bullet list of what has happened, oldest first, each with its date.",
			"Bold the dates and the outcomes.",
			"If the newest thing you have is more than a few weeks older than today, say so plainly in the first sentence and say that anything since is not covered, because a stale answer to a status question reads as current and is worse than no answer.",
			"Do not present a scheduled or expected step as though it has happened, and do not present something that was scheduled for a date now past as though it is still ahead.",
			"Say what is outstanding or next if the passages give it.",
		}, " "),
		MaxPassages: 16, PerSource: 4, MaxTokens: 1100,
	},

	// A code answer is the one shape where the reader does not read the
	// answer, they paste it. So the prose is the part that gets cut and the
	// file is the part that has to be whole: a snippet with a comment saying
	// the rest goes here is worth nothing to someone who wanted a working
	// thing, and it is the failure a small model reaches for when the passages
	// only show fragments.
	ShapeCode: {
		Shape: ShapeCode,
		Instruction: strings.Join([]string{
			houseStyle,
			"Open with one sentence saying what this does and what it needs installed, and cite it. Then the code.",
			"Put every file in its own fenced code block, with the language after the opening fence, and the file name in bold on the line above it.",
			"Name each file for what it does rather than app.py or script.py, and use the name the tool requires when there is one, like Dockerfile or docker-compose.yml.",
			"Give whole files that run as they are. Every import, every function it calls, and the entry point.",
			"Never write an ellipsis, a comment saying the rest of the code goes here, or a placeholder for something you did not write.",
			"A value the reader has to supply is a named constant at the top with a real default, not a gap in the middle.",
			"Never put a citation inside a code block, since it is pasted into a file and it would break it. Cite on the sentences around the code.",
			"Comment only what is not obvious from the code, and never annotate a line with what it plainly does.",
			"Never write a comment about what you were reading, what it did or did not say, or what you could not find. A comment reasoning about that leaves working looking code that does nothing.",
			"If you cannot find how a part of it is done, write the simplest version that really works, using the plainest tool available, and say what is left under Watch out for.",
			"After the code, a `## Run it` heading with the exact commands in order, one per line in a single shell block, starting from a clean machine: what to install, then how it is actually started.",
			"That is the only place an install or a run command appears. Never put one between the files, and never write the same command twice.",
			"End that block with the way the question said it would be used. A question about cron ends on the crontab line, one about Docker ends on the docker command, one about a web page ends on the URL to open.",
			"Then `## Watch out for` with at most three lines, each citing where it came from, and only for something that will actually bite: a version that matters, a rate limit, a platform difference. Leave the heading out when there is nothing.",
			"Never explain the code line by line and never restate what the file already says.",
			"If the answer is one command rather than a program, give the command on its own in a shell block and stop.",
			"Never wrap a command in a program that only prints it, and never write a file whose comments say it is conceptual, illustrative, or what would happen in a real environment. Everything you write has to actually run.",
		}, " "),
		// A single page app is one file and one file is the whole answer, so
		// this is nearly double the next largest shape. Under it the model
		// stops mid-tag, which is worth nothing to anybody.
		MaxPassages: 16, PerSource: 4, MaxTokens: 3400,
		Reminder: "Whole files that run as they are. No ellipsis, no placeholder, no comment standing in for code you did not write.",
	},

	// "When is the next Liverpool game" and "who did Liverpool play last night"
	// want opposite halves of the same fixture list, and the plan step had six
	// pasts and a present to choose between until this existed.
	ShapeUpcoming: {
		Shape: ShapeUpcoming,
		Instruction: strings.Join([]string{
			houseStyle,
			datesAreFuture,
			"Open with one sentence giving the next one: what it is, its date, and the time, the place and the competition when the passages carry them. Bold the date and the time.",
			"The next one is the soonest date, whichever passage it is in. A page listing one competition calls its own first match the next match and it is not, so read every date across every passage before choosing.",
			"A question asking when the next one is wants that one. Give it, cite it like any other sentence, and stop.",
			"Only when the question asks for a list of what is coming up, give at most five, soonest first, as markdown bullets with their dates.",
			"Every line of that list is a fixture a passage states. Never continue a pattern, never work out where a season goes next, and never write a date, an opponent or a time you did not read.",
			"Every date and time comes from a passage. Never work one out yourself, never write a day of the week a passage did not give, and never convert a kick off time into another time zone.",
			"Never comment on how old the passages are or when they were written. Whether the schedule has moved since is worked out elsewhere.",
		}, " "),
		MaxPassages: 14, PerSource: 3, MaxTokens: 800,
		Reminder: "Only a date today or later can answer this. Anything earlier has already happened.",
	},

	ShapeNews: {
		Shape: ShapeNews,
		Instruction: strings.Join([]string{
			houseStyle,
			datesArePast,
			"Lead with what happened and when, in one sentence, naming the date.",
			"Then three to five markdown bullets, each a specific development with its date if the passages give one.",
			"Background comes after the bullets, only when it is not already in one, and never as a summary of them.",
			"Bold names, numbers and dates.",
			"The question asks about what already happened.",
			"Passages about scheduled, upcoming or future events do not answer it, so do not use them as if they did.",
			"If every passage is about something upcoming rather than something that happened, say exactly that and name the most recent thing the passages do cover.",
			"Say plainly if the passages disagree or if the newest one is older than the question implies.",
		}, " "),
		MaxPassages: 14, PerSource: 3, MaxTokens: 900,
		Reminder: "This asks what already happened. A passage about something upcoming does not answer it.",
	},
}

// shapeNames is the enum handed to the plan step's grammar.
func shapeNames() []string {
	out := make([]string, len(shapeEnum))
	for i, s := range shapeEnum {
		out[i] = string(s)
	}
	return out
}

func contractFor(s Shape) Contract {
	if c, ok := contracts[s]; ok {
		return c
	}
	return contracts[ShapeFactual]
}

// guessShape is the fallback when the model's classification fails or is
// missing. Cheap keyword matching, and it only has to beat always answering
// with prose.
func guessShape(q string) Shape {
	l := strings.ToLower(q)
	switch {
	case containsAny(l, "recipe", "how do i make", "how to make", "ingredients for", "cook", "bake"):
		return ShapeRecipe
	case containsAny(l, " vs ", "versus", "compare", "difference between", "better than"):
		return ShapeComparison
	case containsAny(l, "write me a", "write a script", "build me a", "give me a", "example code",
		"code for", "dockerfile", "docker compose", "regex for", "sql query", "bash script",
		"python script", "shell script", "one liner", "snippet"):
		return ShapeCode
	case containsAny(l, "how do i", "how to", "how can i", "steps to", "set up", "install", "configure"):
		return ShapeHowTo
	case containsAny(l, "when is the next", "when's the next", "when do ", "when does ",
		"when are ", "when will", "next game", "next match", "next fixture", "next launch",
		"upcoming", "fixtures", "schedule for", "release date", "kick off", "kickoff",
		"coming up", "who do they play next"):
		return ShapeUpcoming
	case containsAny(l, "status of", "what happened to", "where does", "latest on",
		"any update", "how did it end", "is it over", "still going on", "outcome of",
		"verdict", "trial", "case against", "investigation into"):
		return ShapeStatus
	case containsAny(l, "latest", "news", "happened", "announced", "released", "update on",
		"most recent", "last ", "who won", "score", "result of", "this week", "yesterday"):
		return ShapeNews
	}
	return ShapeFactual
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
