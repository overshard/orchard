package skills

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// The eval set. Every case is a question and the skill that should claim it,
// and the negative half matters more than the positive half: the routes that
// go wrong are the ones sitting just outside a skill rather than far from every
// skill, so "who won the us open" and "what are the odds on the us open" are
// both here on purpose.
var routes = []struct {
	q    string
	want string
}{
	// maths
	{"30 * 27", "maths"},
	{"what is 15% of 240", "maths"},
	{"(1200 + 450) / 3", "maths"},
	{"how does compound interest work", None},
	{"how many calories in a banana", None},

	// convert
	{"30 celsius to fahrenheit", "convert"},
	{"how many km in 5 miles", "convert"},
	{"100 usd in eur", "convert"},
	{"convert 180 pounds to kg", "convert"},
	{"why is the euro weak against the dollar", None},
	{"what currency does japan use", None},

	// time
	{"what time is it in tokyo", "time"},
	{"how many days until christmas", "time"},
	{"what is the date today", "time"},
	{"why do we have time zones", None},
	{"when was the declaration of independence signed", None},

	// weather
	{"what is the weather this weekend", "weather"},
	{"is it going to rain tomorrow", "weather"},
	{"do i need a jacket today", "weather"},
	{"how do hurricanes form", None},
	{"what was the hottest day ever recorded", None},

	// markets
	{"what is the S&P 500 right now", "markets"},
	{"bitcoin price", "markets"},
	{"is the dow up or down today", "markets"},
	{"why did the market drop", None},
	{"should i buy bitcoin", None},

	// odds
	{"what are the odds on the us open", "odds"},
	{"who is favoured to win the election", "odds"},
	{"what are the chances the fed cuts rates", "odds"},
	{"who won the us open", None},
	{"how do betting odds work", None},

	// sports
	{"did Liverpool win their last match", "sports"},
	{"what was the score in the chiefs game", "sports"},
	{"when do liverpool play next", None},
	{"explain the offside rule", None},

	// the long tail, all of which is the web's job
	{"how do i set up wireguard on debian", None},
	{"sqlite vs postgres for a small site", None},
	{"how do i make a breakfast burrito", None},
	{"what is the status of the artemis program", None},
	{"who is the ceo of anthropic", None},
}

// TestRouterEval needs the model, so it is opt in the same way the live search
// tests are. Without it a full `go test ./...` would load the GPU.
func TestRouterEval(t *testing.T) {
	if os.Getenv("SEARCH_LIVE") == "" {
		t.Skip("set SEARCH_LIVE=1 to evaluate routing against the model")
	}
	m := liveModel(t)
	r := Default()

	only := os.Getenv("SEARCH_EVAL_ONLY")
	var wrong, ran int
	for _, c := range routes {
		if only != "" && !strings.Contains(c.q, only) {
			continue
		}
		ran++
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		got := r.Decide(ctx, m, c.q)
		cancel()
		if got.Skill != c.want {
			wrong++
			t.Errorf("%-45q got %-8s want %-8s (%s)", c.q, got.Skill, c.want, got.Why())
		}
	}
	t.Logf("%d of %d routed correctly", ran-wrong, ran)
}

// TestOfflineMatch covers the fallback that runs when the model is down. It is
// allowed to be worse than the model, but it must never claim a skill for a
// question that is plainly the web's.
func TestOfflineMatch(t *testing.T) {
	r := Default()
	for _, q := range []string{
		"how do i set up wireguard on debian",
		"sqlite vs postgres for a small site",
		"who is the ceo of anthropic",
		"how do hurricanes form",
	} {
		if got := r.match(q); got != None {
			t.Errorf("offline matcher claimed %q for %s, should be %s", q, got, None)
		}
	}
	for _, c := range []struct{ q, want string }{
		{"what is the weather this weekend", "weather"},
		{"bitcoin price", "markets"},
		{"what are the odds on the us open", "odds"},
		{"what time is it in tokyo", "time"},
	} {
		if got := r.match(c.q); got != c.want {
			t.Errorf("offline matcher got %s for %q, want %s", got, c.q, c.want)
		}
	}
}

// TestCardsAreRoutable is the cheap guard on the thing the router depends on:
// a card with no negative triggers is a card that will over-fire, and a name
// that is not a bare word cannot be an enum value.
func TestCardsAreRoutable(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range Default().All() {
		c := s.Card()
		switch {
		case c.Name == "" || strings.ContainsAny(c.Name, " \t"):
			t.Errorf("%q is not usable as an enum value", c.Name)
		case seen[c.Name]:
			t.Errorf("two skills answer to %q", c.Name)
		case c.Does == "":
			t.Errorf("%s has no description, so the router is guessing", c.Name)
		case len(c.Fires) < 3:
			t.Errorf("%s has %d trigger examples, want at least 3", c.Name, len(c.Fires))
		case len(c.NotFor) < 3:
			t.Errorf("%s has %d negative triggers, want at least 3", c.Name, len(c.NotFor))
		}
		seen[c.Name] = true
	}
	if seen[None] {
		t.Errorf("a skill is named %q, which collides with the no-skill route", None)
	}
}
