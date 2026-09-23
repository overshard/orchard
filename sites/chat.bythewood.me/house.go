package main

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"chat.bythewood.me/tools"
)

// A question about a house is looked up before the model decides anything. Told
// by the gate to call property, it answered twice more without calling it and
// then gave a clean bill on industry the report had measured, so the call is
// made here rather than asked for.

// Only an address that runs through to a zip code is taken, since a cold lookup
// spends one of a few allowed an hour and a street line on its own often fails to
// place.
var zipAfterStreet = regexp.MustCompile(`^[^?\n]{0,60}?\b\d{5}\b`)

var askingPrice = regexp.MustCompile(`\$\s?(\d{2,3}(?:,\d{3})+|\d{5,7})\b`)

// Which section a question is about, in the order they are checked. The first
// question about a house with none of these words gets the summary.
var houseSections = []struct {
	name string
	re   *regexp.Regexp
}{
	{"industry", regexp.MustCompile(`(?i)\b(industr\w*|factor(y|ies)|data cent(er|re)s?|landfills?|quarr(y|ies)|pollution|superfund|power plants?|substations?|sewage|live near)\b`)},
	{"flood", regexp.MustCompile(`(?i)\b(flood\w*|creeks?|rivers?|how close is the water)\b`)},
	{"schools", regexp.MustCompile(`(?i)\b(schools?|drop ?off|zoned)\b`)},
	{"commutes", regexp.MustCompile(`(?i)\b(commutes?|drives?|work|jobs?|nursing|cna|hospitals?|everyone in the house)\b`)},
	{"area", regexp.MustCompile(`(?i)\b(crime|census|median income|demographics?)\b`)},
	{"neighbours", regexp.MustCompile(`(?i)\b(neighbou?rhood|neighbou?rs|owner occupied|the street)\b`)},
	{"road", regexp.MustCompile(`(?i)\b(road noise|traffic|busy road)\b`)},
	{"land", regexp.MustCompile(`(?i)\b(acres?|acreage|lot size|slope|flat|assess\w*)\b`)},
	{"outings", regexp.MustCompile(`(?i)\b(things to do|outings?|parks?|trails?)\b`)},
	{"links", regexp.MustCompile(`(?i)\b(redfin|zillow|realtor|street view|link to it)\b`)},
	{"cost", regexp.MustCompile(`(?i)\b(cost|loans?|mortgage|payments?|monthly|fha|usda|closing|down payment)\b`)},
}

// houseAddress is the address a message names, running through its zip code.
func houseAddress(text string) string {
	// "$269,900 88 Example St" reads to the street pattern as house number 900.
	text = askingPrice.ReplaceAllStringFunc(text, func(m string) string { return strings.Repeat(" ", len(m)) })
	loc := streetAddress.FindStringIndex(text)
	if loc == nil {
		return ""
	}
	rest := zipAfterStreet.FindString(text[loc[1]:])
	if rest == "" {
		return ""
	}
	return strings.TrimSpace(text[loc[0] : loc[1]+len(rest)])
}

func houseSectionsFor(question string) []string {
	var out []string
	for _, s := range houseSections {
		if s.re.MatchString(question) {
			out = append(out, s.name)
		}
	}
	return out
}

func priceIn(text string) int {
	m := askingPrice.FindStringSubmatch(text)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.ReplaceAll(m[1], ",", ""))
	// A house price, not the insurance or the HOA a message also mentions.
	if n < 20000 {
		return 0
	}
	return n
}

// houseQuestion works out which house a turn is about and which sections it
// wants. A question naming no address is a follow up about the last house the
// conversation named, and only counts when it asks for a section by its words,
// since "what's the weather there" is not a question for the public record.
func houseQuestion(question string, history []Message) (addr string, price int, sections []string) {
	sections = houseSectionsFor(question)
	addr, price = houseAddress(question), priceIn(question)
	if addr == "" {
		if len(sections) == 0 {
			return "", 0, nil
		}
		// A conversation that has moved on from the house is not about it any
		// more, so only the last few questions are read.
		asked := 0
		for i := len(history) - 1; i >= 0 && addr == "" && asked < 4; i-- {
			if history[i].Role == RoleUser {
				asked++
				addr = houseAddress(history[i].Content)
			}
		}
		if addr == "" {
			return "", 0, nil
		}
	}
	// The price is usually only in the first message about a house, and nothing
	// about the money is worked out without it.
	for i := len(history) - 1; i >= 0 && price == 0; i-- {
		if history[i].Role == RoleUser && houseAddress(history[i].Content) == addr {
			price = priceIn(history[i].Content)
		}
	}
	if len(sections) == 0 {
		sections = []string{"summary"}
	}
	return addr, price, sections
}

// houseOpening calls property for the sections the question is about and hands
// the model what came back.
func (e *Engine) houseOpening(ctx context.Context, deps *tools.Deps, question string, history []Message) ([]tools.Result, Message, bool) {
	addr, price, sections := houseQuestion(question, history)
	if addr == "" {
		return nil, Message{}, false
	}
	var results []tools.Result
	var b strings.Builder
	for _, s := range sections {
		args := map[string]any{"address": addr, "section": s}
		if price > 0 {
			args["price"] = price
		}
		raw, _ := json.Marshal(args)
		res := e.reg.Call(ctx, deps, tools.PropertyTool.Name, raw)
		results = append(results, res)
		if res.Err != "" {
			// A failed first call is a failed lookup, and asking for the next
			// section would only fail the same way.
			break
		}
		body, _ := json.Marshal(res.Content)
		if len(body) > 14000 {
			body = body[:14000]
		}
		b.WriteString("\n\n[" + s + "]\n")
		b.Write(body)
	}
	if b.Len() == 0 {
		return results, Message{}, false
	}
	msg := Message{Role: RoleUser, Content: "Before you answer, here is what the public record says about " + addr +
		", looked up for you. It is for you to answer from, so do not name it, its sections or its fields." + b.String() +
		"\n\nAnswer from this. Every fact about the house has to come from it, and a thing it lists is a thing it found, " +
		"so never say there is nothing there when it names something. Call property again with this address for any " +
		"other section the question needs."}
	return results, msg, true
}
