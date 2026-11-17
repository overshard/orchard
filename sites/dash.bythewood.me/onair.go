package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"
)

// Whether TheBurntPeanut is streaming. He simulcasts, so Twitch is asked first
// and Kick only when Twitch has nothing. YouTube answers a server's IP with a
// bot check, and its Data API needs a key.
const (
	streamerName = "TheBurntPeanut"
	twitchLogin  = "theburntpeanut"
	twitchURL    = "https://www.twitch.tv/" + twitchLogin

	// The Client-ID Twitch's own web player sends. Helix wants a registered
	// app and a token, and this has answered without either for years.
	twitchGQL      = "https://gql.twitch.tv/gql"
	twitchClientID = "kimne78kx3ncx6brgo4mv6wki5h1ko" // gitleaks:allow

	kickChannel = "https://kick.com/api/v2/channels/" + twitchLogin
	kickURL     = "https://kick.com/" + twitchLogin

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

	Broadcasts Broadcasts `json:"broadcasts"`

	// id is the platform's own for this broadcast. The notice is named by the
	// session, which outlives a reconnect or a fall over to Twitch.
	id      string
	session string
	seen    time.Time
	started time.Time
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
		started:  s.CreatedAt,
	}
	if s.Game != nil {
		o.Game = strings.ToUpper(s.Game.Name)
	}
	return o, nil
}

type kickPayload struct {
	Livestream *struct {
		ID           int64  `json:"id"`
		SessionTitle string `json:"session_title"`
		IsLive       bool   `json:"is_live"`
		StartTime    string `json:"start_time"`
		ViewerCount  int    `json:"viewer_count"`
		Categories   []struct {
			Name string `json:"name"`
		} `json:"categories"`
	} `json:"livestream"`
}

func fetchKick(ctx context.Context, g *Guard) (OnAir, error) {
	var p kickPayload
	if err := getJSONHeaders(ctx, g, "kick", kickChannel, nil, &p); err != nil {
		return OnAir{}, err
	}
	return parseKick(p, time.Now())
}

func parseKick(p kickPayload, now time.Time) (OnAir, error) {
	s := p.Livestream
	if s == nil || !s.IsLive {
		return OnAir{}, nil
	}
	// start_time is UTC with no zone on it.
	started, _ := time.Parse(time.DateTime, s.StartTime)
	o := OnAir{
		Live:     true,
		Name:     streamerName,
		Platform: "Kick",
		Title:    streamTitle(s.SessionTitle),
		URL:      kickURL,
		Viewers:  compactCount(s.ViewerCount),
		Since:    onAirSince(started, now),
		id:       fmt.Sprint(s.ID),
		started:  started,
	}
	if len(s.Categories) > 0 {
		o.Game = strings.ToUpper(s.Categories[0].Name)
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
