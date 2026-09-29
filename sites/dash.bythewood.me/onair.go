package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Whether TheBurntPeanut is streaming. He simulcasts, so YouTube is asked first
// and Twitch only when YouTube has nothing, which also keeps the banner working
// the day YouTube changes the page this reads.
const (
	streamerName = "TheBurntPeanut"
	youtubeLive  = "https://www.youtube.com/@theburntpeanut/live"
	twitchLogin  = "theburntpeanut"
	twitchURL    = "https://www.twitch.tv/" + twitchLogin

	// The Client-ID Twitch's own web player sends. Helix wants a registered
	// app and a token, and this has answered without either for years.
	twitchGQL      = "https://gql.twitch.tv/gql"
	twitchClientID = "kimne78kx3ncx6brgo4mv6wki5h1ko"

	onAirEvery = 10 * time.Minute

	// A stream that drops and comes back inside this is the same broadcast and
	// is not announced twice.
	onAirGrace = 30 * time.Minute
)

type OnAir struct {
	Live     bool   `json:"live"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	Viewers  string `json:"viewers"`
	Game     string `json:"game"`
	Since    string `json:"since"`

	// id is the platform's own for this broadcast. The notice is named by the
	// session, which outlives a reconnect or a fall over to Twitch.
	id      string
	session string
	seen    time.Time
}

// pickOnAir takes the first source that says he is live. Offline only needs one
// of them to have answered, since a broken source that could hold the banner up
// on its own would hold it up forever.
func pickOnAir(sources ...func() (OnAir, error)) (OnAir, error) {
	var errs []error
	answered := false
	for _, source := range sources {
		o, err := source()
		if err != nil {
			slog.Warn("on air source failed", slog.String("component", "onair"), slog.Any("err", err))
			errs = append(errs, err)
			continue
		}
		if o.Live {
			return o, nil
		}
		answered = true
	}
	if answered {
		return OnAir{}, nil
	}
	return OnAir{}, errors.Join(errs...)
}

// carryOnAir works out which broadcast a live reading belongs to. A dropped
// stream comes back under a new id, so a reading inside onAirGrace of the last
// live one keeps that session.
func carryOnAir(fresh, prev OnAir, now time.Time) OnAir {
	if !fresh.Live {
		fresh.session, fresh.seen = prev.session, prev.seen
		return fresh
	}
	if prev.session != "" && now.Sub(prev.seen) < onAirGrace {
		fresh.session = prev.session
	} else {
		fresh.session = strings.ToLower(fresh.Platform) + ":" + fresh.id
	}
	fresh.seen = now
	return fresh
}

var (
	ytWatch     = regexp.MustCompile(`<link rel="canonical" href="https://www\.youtube\.com/watch\?v=([\w-]+)"`)
	ytChannel   = regexp.MustCompile(`<link rel="canonical" href="https://www\.youtube\.com/channel/`)
	ytBroadcast = regexp.MustCompile(`"liveBroadcastDetails":(\{[^{}]*\})`)
	ytTitle     = regexp.MustCompile(`<meta property="og:title" content="([^"]*)"`)
	ytViewers   = regexp.MustCompile(`"originalViewCount":"(\d+)"`)
)

// parseYouTube reads the channel's /live page. Live, it is the watch page of
// the stream. Offline, its canonical link is the channel, and a page that is
// neither is YouTube having changed something, so it fails where UPLINK shows it.
func parseYouTube(page []byte, now time.Time) (OnAir, error) {
	m := ytWatch.FindSubmatch(page)
	if m == nil {
		if ytChannel.Match(page) {
			return OnAir{}, nil
		}
		return OnAir{}, errors.New("youtube: no canonical link on the live page")
	}

	// The /live page of a scheduled stream is its watch page too, so the
	// canonical link alone does not mean it has started.
	b := ytBroadcast.FindSubmatch(page)
	if b == nil {
		return OnAir{}, nil
	}
	var details struct {
		IsLiveNow      bool      `json:"isLiveNow"`
		StartTimestamp time.Time `json:"startTimestamp"`
	}
	if err := json.Unmarshal(b[1], &details); err != nil {
		return OnAir{}, fmt.Errorf("youtube: %w", err)
	}
	if !details.IsLiveNow {
		return OnAir{}, nil
	}

	o := OnAir{
		Live:     true,
		Name:     streamerName,
		Platform: "YouTube",
		URL:      "https://www.youtube.com/watch?v=" + string(m[1]),
		Since:    onAirSince(details.StartTimestamp, now),
		id:       string(m[1]),
	}
	if t := ytTitle.FindSubmatch(page); t != nil {
		o.Title = streamTitle(html.UnescapeString(string(t[1])))
	}
	if v := ytViewers.FindSubmatch(page); v != nil {
		if n, err := strconv.Atoi(string(v[1])); err == nil {
			o.Viewers = compactCount(n)
		}
	}
	return o, nil
}

func fetchYouTube(ctx context.Context, g *Guard) (OnAir, error) {
	const endpoint = "youtube"
	if err := g.Reserve(ctx, endpoint); err != nil {
		return OnAir{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, youtubeLive, nil)
	if err != nil {
		return OnAir{}, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Language", "en-US,en")

	resp, err := client.Do(req)
	if err != nil {
		g.Fail(endpoint, 0, 0)
		return OnAir{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		g.Fail(endpoint, resp.StatusCode, parseRetryAfter(resp.Header.Get("Retry-After")))
		return OnAir{}, fmt.Errorf("%s: http %d", endpoint, resp.StatusCode)
	}

	page, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		g.Fail(endpoint, resp.StatusCode, 0)
		return OnAir{}, fmt.Errorf("%s: %w", endpoint, err)
	}
	o, err := parseYouTube(page, time.Now())
	if err != nil {
		g.Fail(endpoint, resp.StatusCode, 0)
		return OnAir{}, err
	}

	g.Succeed(endpoint)
	return o, nil
}

const twitchQuery = `query Live($login: String!) {
  user(login: $login) {
    stream { id title viewersCount createdAt game { name } }
  }
}`

type twitchPayload struct {
	Data struct {
		User *struct {
			Stream *struct {
				ID           string    `json:"id"`
				Title        string    `json:"title"`
				ViewersCount int       `json:"viewersCount"`
				CreatedAt    time.Time `json:"createdAt"`
				Game         *struct {
					Name string `json:"name"`
				} `json:"game"`
			} `json:"stream"`
		} `json:"user"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func fetchTwitch(ctx context.Context, g *Guard) (OnAir, error) {
	body := map[string]any{
		"query":     twitchQuery,
		"variables": map[string]string{"login": twitchLogin},
	}
	var p twitchPayload
	if err := postJSONHeaders(ctx, g, "twitch", twitchGQL, map[string]string{"Client-ID": twitchClientID}, body, &p); err != nil {
		return OnAir{}, err
	}
	return parseTwitch(p, time.Now())
}

func parseTwitch(p twitchPayload, now time.Time) (OnAir, error) {
	if len(p.Errors) > 0 {
		return OnAir{}, fmt.Errorf("twitch: %s", p.Errors[0].Message)
	}
	if p.Data.User == nil {
		return OnAir{}, fmt.Errorf("twitch: no user %s", twitchLogin)
	}
	s := p.Data.User.Stream
	if s == nil {
		return OnAir{}, nil
	}
	o := OnAir{
		Live:     true,
		Name:     streamerName,
		Platform: "Twitch",
		Title:    streamTitle(s.Title),
		URL:      twitchURL,
		Viewers:  compactCount(s.ViewersCount),
		Since:    onAirSince(s.CreatedAt, now),
		id:       s.ID,
	}
	if s.Game != nil {
		o.Game = strings.ToUpper(s.Game.Name)
	}
	return o, nil
}

// A title that opens by saying it's live says what the banner already does.
var livePrefix = regexp.MustCompile(`^(?i:live)\s*[|:!\-\x{2013}\x{2014}]+\s*`)

func streamTitle(title string) string {
	t := strings.TrimSpace(title)
	t = strings.TrimSpace(strings.TrimPrefix(t, "🔴"))
	t = livePrefix.ReplaceAllString(t, "")
	if t == "" {
		return strings.TrimSpace(title)
	}
	return t
}

// onAirSince is the start in Isaac's time, with the day only once it is not
// today.
func onAirSince(start, now time.Time) string {
	if start.IsZero() {
		return ""
	}
	s, n := start.In(easternTime()), now.In(easternTime())
	layout := "3:04pm"
	if s.YearDay() != n.YearDay() || s.Year() != n.Year() {
		layout = "Mon 3:04pm"
	}
	return strings.ToUpper(s.Format(layout))
}
