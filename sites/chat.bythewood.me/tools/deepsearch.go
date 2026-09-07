package tools

// search.bythewood.me as a tool.
//
// This is the one the two sites were kept separate for. search checks every
// sentence it writes against the passage it cites and reports how much of the
// answer is supported. Chat cannot do that and should not try, so when an answer
// has to be right rather than quick, it asks search instead of reading pages
// itself.
//
// It is expensive in a way the other tools are not, and the reason is the card.
// search runs its whole pipeline through the same gateway chat is using, on one
// GPU with a single slot, so while it is thinking chat is not. That is why the
// description leads with the cost and why the engine allows one call per turn.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const searchBase = "http://orchard-search:8000"

var DeepSearch = Tool{
	Name: "deep_search",
	Description: "Ask search.bythewood.me, which reads the pages properly and checks every sentence " +
		"of its answer against the passage it cites, then reports how much of the answer is supported. " +
		"It takes a minute or more and blocks everything else, so it is not for ordinary lookups: use " +
		"web_search and web_fetch for those. Use this when being wrong matters and Isaac needs a source " +
		"he can follow, when he asks you to check or verify something, or when a claim is contested. " +
		"Once per question at most.",
	Schema: obj(map[string]any{
		"question": str("the question, written out in full as a standalone sentence, since search has none of this conversation"),
	}, "question"),
	Run: func(ctx context.Context, d *Deps, a map[string]any) (any, error) {
		q := strings.TrimSpace(argStr(a, "question"))
		if q == "" {
			return nil, fmt.Errorf("question is required")
		}
		if d.Session == "" {
			return nil, fmt.Errorf("this needs you to be signed in, and the turn carried no session")
		}

		// incognito, always. Chat keeps this conversation already, and search
		// logging a second copy of the question under its own history is a
		// record Isaac did not ask for and would have to delete twice.
		u := searchBase + "/stream?incognito=1&q=" + url.QueryEscape(q)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.AddCookie(&http.Cookie{Name: SessionCookie, Value: d.Session})
		req.Header.Set("Accept", "text/event-stream")

		resp, err := d.HTTP.Do(req)
		if err != nil {
			return nil, fmt.Errorf("search is not answering: %w", err)
		}
		defer resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden,
			resp.StatusCode == http.StatusSeeOther, resp.StatusCode == http.StatusFound:
			return nil, fmt.Errorf("search refused the session, so it may have been signed out")
		case resp.StatusCode >= 400:
			return nil, fmt.Errorf("search answered %d", resp.StatusCode)
		}
		return readSearchStream(resp.Body)
	},
}

// readSearchStream follows the event stream to its answer. Only two events
// matter here: the page also shows queue position and per step progress, and
// neither is something a model should be handed.
func readSearchStream(body interface{ Read([]byte) (int, error) }) (any, error) {
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)

	var event string
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data := strings.TrimPrefix(line, "data: ")
			switch event {
			case "failed":
				var f struct {
					Error string `json:"error"`
				}
				_ = json.Unmarshal([]byte(data), &f)
				if f.Error == "" {
					f.Error = "search could not answer that"
				}
				return nil, fmt.Errorf("%s", f.Error)
			case "answer":
				return searchAnswer(data)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("search stopped mid answer: %w", err)
	}
	return nil, fmt.Errorf("search closed without answering")
}

func searchAnswer(data string) (any, error) {
	var a struct {
		Text     string   `json:"text"`
		Support  float64  `json:"support"`
		Elapsed  string   `json:"elapsed"`
		Warnings []string `json:"warnings"`
		Sources  []struct {
			Title string `json:"title"`
			URL   string `json:"url"`
		} `json:"sources"`
	}
	if err := json.Unmarshal([]byte(data), &a); err != nil {
		return nil, fmt.Errorf("search sent an answer that could not be read")
	}
	if strings.TrimSpace(a.Text) == "" {
		return nil, fmt.Errorf("search returned an empty answer")
	}

	srcs := make([]map[string]string, 0, len(a.Sources))
	for _, s := range a.Sources {
		srcs = append(srcs, map[string]string{"title": s.Title, "url": s.URL})
	}
	out := map[string]any{
		"answer":  a.Text,
		"sources": srcs,
		"elapsed": a.Elapsed,
		// The fraction of the answer's sentences that a cited passage actually
		// supports. It is the reason for calling this rather than reading pages,
		// so it is passed on rather than kept.
		"support": a.Support,
		"note": "This answer was checked sentence by sentence against the pages it cites. " +
			"Use it as written and keep the urls next to the facts they came from. Do not call this again for this question.",
	}
	if len(a.Warnings) > 0 {
		out["warnings"] = a.Warnings
	}
	if a.Support > 0 && a.Support < 0.6 {
		out["note"] = "Less than two thirds of this answer is supported by the pages it found, so say " +
			"plainly which parts are uncertain rather than presenting it as settled."
	}
	return out, nil
}
