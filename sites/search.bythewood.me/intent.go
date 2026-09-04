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
)

// Contract is the format instruction handed to the synthesis step, and the
// passage budget the shape needs. A recipe needs more of the page than a
// factual answer does, because ingredients and method are usually far apart.
type Contract struct {
	Shape       Shape
	Instruction string
	MaxPassages int
	PerSource   int
	MaxTokens   int
}

// answerFirst is prepended to every shape. The rule Isaac stated: answer the
// question, then the few facts worth skimming, and only then the detail, which
// is a bonus rather than something forced on a reader who wanted one number.
const answerFirst = "Answer the question directly in the first sentence, before anything else. " +
	"Then give the few facts that matter as a short markdown bullet list, bolding the value in each one. " +
	"Keep that part tight, a reader should get what they asked for without scrolling. " +
	"Then the fuller explanation, which is where any nuance, caveat or background goes. " +
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
			"Write an actual recipe, not a description of one.",
			"Start with one sentence saying what it makes, then a line for yield and total time if the passages give them.",
			"Then a `## Ingredients` heading with a markdown bullet list, one ingredient per line, quantities included exactly as the passages give them.",
			"Then a `## Method` heading with a numbered list, one step per line, in order.",
			"Bold ingredient names in the ingredient list and bold temperatures and times in the steps.",
			"If several passages give different versions, follow the most complete one rather than blending them.",
			"Notes, substitutions and storage come after the method, not before it.",
			"Cite the passage each part came from.",
		}, " "),
		MaxPassages: 18, PerSource: 6, MaxTokens: 1400,
	},
	ShapeHowTo: {
		Shape: ShapeHowTo,
		Instruction: strings.Join([]string{
			"Open with one sentence saying what the steps achieve.",
			"Then a numbered list of steps in the order they must be done, one action per step, written as an instruction.",
			"Bold any command, file name, or exact value the reader has to type.",
			"Caveats, alternatives and explanation come after the steps, not before them.",
		}, " "),
		MaxPassages: 14, PerSource: 4, MaxTokens: 1000,
	},
	ShapeComparison: {
		Shape: ShapeComparison,
		Instruction: strings.Join([]string{
			"Open with one sentence naming which option suits which case.",
			"Then a markdown bullet list with one line per point of difference, each naming both sides, bolding the option name at the start of the line.",
			"Then one sentence on the tradeoff that actually decides it.",
			"Anything further comes after that, not before it.",
		}, " "),
		MaxPassages: 14, PerSource: 4, MaxTokens: 900,
	},
	ShapeNews: {
		Shape: ShapeNews,
		Instruction: strings.Join([]string{
			"Lead with what happened and when, in one sentence, naming the date.",
			"Then three to five markdown bullets, each a specific development with its date if the passages give one.",
			"Background and history come after the bullets, not before them.",
			"Bold names, numbers and dates.",
			"The question asks about what already happened.",
			"Passages about scheduled, upcoming or future events do not answer it, so do not use them as if they did.",
			"If every passage is about something upcoming rather than something that happened, say exactly that and name the most recent thing the passages do cover.",
			"Say plainly if the passages disagree or if the newest one is older than the question implies.",
		}, " "),
		MaxPassages: 14, PerSource: 3, MaxTokens: 900,
	},
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
	case containsAny(l, "how do i", "how to", "how can i", "steps to", "set up", "install", "configure"):
		return ShapeHowTo
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
