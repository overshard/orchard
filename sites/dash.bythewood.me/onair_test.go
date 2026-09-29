package main

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

const ytLivePage = `<html><head>
<link rel="canonical" href="https://www.youtube.com/watch?v=RIh_OzkyUtw">
<meta property="og:title" content="🔴LIVE | MONDAY FUNDAY | CS2 &amp; PARTY GAMES">
</head><body><script>var ytInitialPlayerResponse = {"microformat":{"playerMicroformatRenderer":{
"liveBroadcastDetails":{"isLiveNow":true,"startTimestamp":"2026-09-28T23:19:08+00:00"}}}};
{"originalViewCount":"47324"}</script></body></html>`

func TestParseYouTubeLive(t *testing.T) {
	now := at(t, "2026-09-28 21:30")
	o, err := parseYouTube([]byte(ytLivePage), now)
	if err != nil {
		t.Fatal(err)
	}
	if !o.Live || o.id != "RIh_OzkyUtw" || o.URL != "https://www.youtube.com/watch?v=RIh_OzkyUtw" {
		t.Fatalf("got %+v", o)
	}
	if o.Title != "MONDAY FUNDAY | CS2 & PARTY GAMES" {
		t.Errorf("title = %q", o.Title)
	}
	if o.Viewers != "47K" || o.Since != "7:19PM" {
		t.Errorf("viewers %q since %q", o.Viewers, o.Since)
	}
}

func TestParseYouTubeNotLive(t *testing.T) {
	offline := `<link rel="canonical" href="https://www.youtube.com/channel/UCMNEVbszv8ZyvSXoTn3yhpQ">`
	upcoming := `<link rel="canonical" href="https://www.youtube.com/watch?v=abc">` +
		`"liveBroadcastDetails":{"isLiveNow":false,"startTimestamp":"2026-09-29T23:00:00+00:00"}`
	vod := `<link rel="canonical" href="https://www.youtube.com/watch?v=abc">`

	for name, page := range map[string]string{"offline": offline, "upcoming": upcoming, "vod": vod} {
		o, err := parseYouTube([]byte(page), time.Now())
		if err != nil || o.Live {
			t.Errorf("%s: live %v err %v", name, o.Live, err)
		}
	}

	// A consent wall or a redesign is neither page, and saying offline would
	// hide that YouTube stopped being read.
	if _, err := parseYouTube([]byte(`<html>Before you continue to YouTube</html>`), time.Now()); err == nil {
		t.Error("a page with no canonical link should be an error")
	}
}

func TestParseTwitch(t *testing.T) {
	var live twitchPayload
	body := `{"data":{"user":{"stream":{"id":"320567189600","title":"MONDAY FUNDAY | CS2","viewersCount":36542,"createdAt":"2026-09-28T23:19:03Z","game":{"name":"Counter-Strike"}}}}}`
	if err := json.Unmarshal([]byte(body), &live); err != nil {
		t.Fatal(err)
	}
	o, err := parseTwitch(live, at(t, "2026-09-28 21:30"))
	if err != nil {
		t.Fatal(err)
	}
	if !o.Live || o.id != "320567189600" || o.Game != "COUNTER-STRIKE" || o.Viewers != "37K" || o.URL != twitchURL {
		t.Errorf("got %+v", o)
	}

	var offline twitchPayload
	json.Unmarshal([]byte(`{"data":{"user":{"stream":null}}}`), &offline)
	if o, err := parseTwitch(offline, time.Now()); err != nil || o.Live {
		t.Errorf("offline: live %v err %v", o.Live, err)
	}

	var gone twitchPayload
	json.Unmarshal([]byte(`{"data":{"user":null}}`), &gone)
	if _, err := parseTwitch(gone, time.Now()); err == nil {
		t.Error("a missing user should be an error, not offline")
	}
}

func TestStreamTitle(t *testing.T) {
	cases := map[string]string{
		"🔴LIVE | MONDAY FUNDAY":  "MONDAY FUNDAY",
		"🔴 live - drops tonight": "drops tonight",
		"LIVE: ranked":           "ranked",
		"Live from Vegas":        "Live from Vegas",
		"🔴LIVE":                  "LIVE",
		"CS2 with the boys":      "CS2 with the boys",
	}
	for in, want := range cases {
		if got := streamTitle(in); got != want {
			t.Errorf("streamTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func source(o OnAir, err error) func() (OnAir, error) {
	return func() (OnAir, error) { return o, err }
}

func TestPickOnAirPrefersYouTube(t *testing.T) {
	yt := OnAir{Live: true, Platform: "YouTube"}
	tw := OnAir{Live: true, Platform: "Twitch"}
	asked := false
	twitch := func() (OnAir, error) { asked = true; return tw, nil }

	o, err := pickOnAir(source(yt, nil), twitch)
	if err != nil || o.Platform != "YouTube" {
		t.Fatalf("got %+v %v", o, err)
	}
	if asked {
		t.Error("Twitch was asked while YouTube had him live")
	}

	// Offline on YouTube or YouTube broken, and Twitch has him.
	for _, first := range []func() (OnAir, error){source(OnAir{}, nil), source(OnAir{}, errors.New("youtube: http 429"))} {
		if o, err := pickOnAir(first, source(tw, nil)); err != nil || o.Platform != "Twitch" {
			t.Errorf("fell over to %+v %v", o, err)
		}
	}
}

func TestPickOnAirNeedsOneAnswer(t *testing.T) {
	broken := source(OnAir{}, errors.New("broken"))

	// One platform saying offline is enough, or a dead source would hold the
	// banner up forever.
	if o, err := pickOnAir(source(OnAir{}, nil), broken); err != nil || o.Live {
		t.Errorf("got %+v %v", o, err)
	}
	if _, err := pickOnAir(broken, broken); err == nil {
		t.Error("with neither answering the banner should be left as it was")
	}
}

// A reconnect comes back under a new id, and a fall over to Twitch is another
// one, and neither should notify twice. A new stream after a real break does.
func TestCarryOnAirKeepsTheSession(t *testing.T) {
	start := at(t, "2026-09-28 19:20")
	first := carryOnAir(OnAir{Live: true, Platform: "YouTube", id: "aaa"}, OnAir{}, start)
	if first.session != "youtube:aaa" {
		t.Fatalf("session = %q", first.session)
	}

	dropped := carryOnAir(OnAir{}, first, start.Add(2*time.Minute))
	if dropped.Live || dropped.session != "youtube:aaa" {
		t.Fatalf("offline reading lost the session: %+v", dropped)
	}

	back := carryOnAir(OnAir{Live: true, Platform: "Twitch", id: "123"}, dropped, start.Add(6*time.Minute))
	if back.session != "youtube:aaa" {
		t.Errorf("reconnect became a new session %q", back.session)
	}

	tomorrow := carryOnAir(OnAir{Live: true, Platform: "YouTube", id: "bbb"}, back, start.Add(20*time.Hour))
	if tomorrow.session != "youtube:bbb" {
		t.Errorf("a new stream kept yesterday's session %q", tomorrow.session)
	}
}

func TestOnAirNotices(t *testing.T) {
	if ns := onAirNotices(OnAir{}); len(ns) != 0 {
		t.Errorf("offline notified: %v", ns)
	}
	o := carryOnAir(OnAir{Live: true, Name: streamerName, Platform: "YouTube", Title: "CS2", URL: "https://www.youtube.com/watch?v=aaa", id: "aaa"}, OnAir{}, time.Now())
	ns := onAirNotices(o)
	if len(ns) != 1 || ns[0].ID != "live:youtube:aaa" || ns[0].Kind != "live" || ns[0].Title != "TheBurntPeanut is live on YouTube" || ns[0].URL != o.URL {
		t.Errorf("got %+v", ns)
	}
}
