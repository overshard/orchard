package skills

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// TestSkillsRun exercises each skill against its own upstream. None of these
// touch the search engine, so they cost nothing against its rate limit, but
// they are still live calls and stay behind the same flag.
func TestSkillsRun(t *testing.T) {
	if os.Getenv("SEARCH_LIVE") == "" {
		t.Skip("set SEARCH_LIVE=1 to run skills against their live sources")
	}
	d := Deps{
		HTTP: &http.Client{Timeout: 30 * time.Second},
		UA:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
		Now:  time.Now,
	}
	cases := []struct {
		skill Skill
		q     string
		// want is a fragment the answer has to carry, so a skill that returns
		// a well formed answer about the wrong thing still fails.
		want string
	}{
		{Calculator{}, "what is 30 * 27", "810"},
		{Convert{}, "30 celsius to fahrenheit", "86.0"},
		{Convert{}, "how many km in 5 miles", "8.05"},
		{Convert{}, "100 usd in eur", "EUR"},
		{Time{}, "what time is it in tokyo", "Tokyo"},
		{Time{}, "how many days until christmas", "Christmas"},
		{Weather{}, "what is the weather this weekend", "°F"},
		{Markets{}, "what is the S&P 500 right now", "S&P 500"},
		{Odds{}, "what are the odds on the us open", "%"},
		{Sports{}, "did Liverpool win their last match", "Liverpool"},
	}
	for _, c := range cases {
		name := c.skill.Card().Name + "/" + c.q
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			res, err := c.skill.Run(ctx, c.q, d)
			if errors.Is(err, errThrottled) {
				// Not a code failure. Running this suite repeatedly is itself
				// enough to get rate limited, which is the thing the cache and
				// the narrowed sweep exist to avoid in normal use.
				t.Skipf("%s is throttling, try again later", c.skill.Card().Name)
			}
			if err != nil {
				t.Fatalf("error: %v", err)
			}
			if res == nil {
				t.Fatal("declined the question it is meant to answer")
			}
			if !strings.Contains(res.Text, c.want) {
				t.Errorf("answer does not carry %q:\n%s", c.want, res.Text)
			}
			t.Logf("%s in %s\n%s", res.Skill, res.Elapsed, res.Text)
		})
	}
}

// TestSkillsDecline is the other half, and the more important one. A skill
// handed a question it cannot answer has to return nothing rather than answer
// about something else, because the router will occasionally send it one.
func TestSkillsDecline(t *testing.T) {
	d := Deps{HTTP: &http.Client{Timeout: 15 * time.Second}, Now: time.Now}
	cases := []struct {
		skill Skill
		q     string
	}{
		{Calculator{}, "how many calories in a banana"},
		{Convert{}, "what currency does japan use"},
		{Convert{}, "why is the euro weak"},
		{Time{}, "when was the declaration of independence signed"},
		{Weather{}, "what is the weather in tokyo"},
		{Markets{}, "should i buy a house"},
		{Odds{}, "the"},
	}
	for _, c := range cases {
		t.Run(c.skill.Card().Name+"/"+c.q, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			res, _ := c.skill.Run(ctx, c.q, d)
			if res != nil {
				t.Errorf("answered a question it should have declined:\n%s", res.Text)
			}
		})
	}
}
