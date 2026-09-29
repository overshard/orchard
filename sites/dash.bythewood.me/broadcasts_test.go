package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// His archive as Twitch answered on 2026-09-29, newest first.
var archive = []struct {
	at   string
	secs int
}{
	{"2026-09-28T23:19:08Z", 26460},
	{"2026-09-26T23:19:23Z", 26310},
	{"2026-09-25T23:09:00Z", 27030},
	{"2026-09-24T23:04:23Z", 28130},
	{"2026-09-23T23:09:23Z", 26770},
	{"2026-09-19T22:04:41Z", 29130},
	{"2026-09-18T20:54:23Z", 34970},
	{"2026-09-17T23:15:20Z", 25660},
	{"2026-09-16T23:19:36Z", 28560},
	{"2026-09-15T23:17:37Z", 26800},
	{"2026-09-14T23:19:37Z", 30390},
	{"2026-09-12T21:45:22Z", 34350},
	{"2026-09-11T19:43:03Z", 40980},
	{"2026-09-10T16:37:44Z", 48780},
	{"2026-09-09T23:13:54Z", 26690},
	{"2026-09-08T23:19:48Z", 35410},
	{"2026-09-07T23:11:30Z", 35190},
	{"2026-09-05T21:22:32Z", 40160},
	{"2026-09-04T19:24:24Z", 47130},
	{"2026-09-03T19:09:20Z", 39540},
	{"2026-09-02T22:17:53Z", 28070},
	{"2026-09-01T22:13:06Z", 28160},
	{"2026-09-01T00:16:09Z", 25380},
}

func utc(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func pastFixture(t *testing.T, slots ...string) twitchPast {
	t.Helper()
	var p twitchPast
	for _, a := range archive {
		p.vods = append(p.vods, pastBroadcast{start: utc(t, a.at), length: time.Duration(a.secs) * time.Second, title: "stream " + a.at[:10]})
	}
	for _, s := range slots {
		p.slots = append(p.slots, slot{start: utc(t, s), title: "Bungulating"})
	}
	return p
}

// before is the archive as it stood at now.
func before(p twitchPast, now time.Time) twitchPast {
	var out twitchPast
	for _, v := range p.vods {
		if v.start.Before(now) {
			out.vods = append(out.vods, v)
		}
	}
	return out
}

func TestParseBroadcasts(t *testing.T) {
	body := `{"data":{"user":{
		"videos":{"edges":[
			{"node":{"createdAt":"2026-09-26T23:19:23Z","lengthSeconds":26310,"title":"SCAM WITH YOUR FRIENDS"}},
			{"node":{"createdAt":"2026-09-28T23:19:08Z","lengthSeconds":26460,"title":"MONDAY FUNDAY"}}]},
		"channel":{"schedule":{"segments":[
			{"startAt":"2026-09-30T00:00:00Z","endAt":"2026-09-30T07:00:00Z","title":"Bungulating"},
			{"startAt":"2026-09-29T00:00:00Z","endAt":null,"title":"Bungulating"}]}}}}}`
	var p broadcastsPayload
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatal(err)
	}
	past, err := parseBroadcasts(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(past.vods) != 2 || past.vods[0].title != "MONDAY FUNDAY" || past.vods[0].length != 26460*time.Second {
		t.Errorf("vods not newest first: %+v", past.vods)
	}
	if len(past.slots) != 2 || !past.slots[0].start.Equal(utc(t, "2026-09-29T00:00:00Z")) {
		t.Errorf("slots not in order: %+v", past.slots)
	}

	// A channel with no schedule set is a result, not a failure.
	var bare broadcastsPayload
	json.Unmarshal([]byte(`{"data":{"user":{"videos":{"edges":[]},"channel":{"schedule":null}}}}`), &bare)
	if _, err := parseBroadcasts(bare); err != nil {
		t.Errorf("no schedule: %v", err)
	}

	for _, broken := range []string{
		`{"errors":[{"message":"Cannot query field \"segments\" on type \"Schedule\"."}]}`,
		`{"data":{"user":null}}`,
	} {
		var p broadcastsPayload
		json.Unmarshal([]byte(broken), &p)
		if _, err := parseBroadcasts(p); err == nil {
			t.Errorf("%s should be an error", broken)
		}
	}
}

func TestBuildBroadcastsOffAir(t *testing.T) {
	// Tuesday 29 September, 7:01pm, before he has gone on.
	now := at(t, "2026-09-29 19:01")
	b := buildBroadcasts(pastFixture(t, "2026-09-27T00:00:00Z", "2026-09-30T00:00:00Z", "2026-10-01T00:00:00Z"), OnAir{}, now)

	want := LastBroadcast{Start: "MON 7:19PM", Ran: "7H 21M", Title: "stream 2026-09-28"}
	if b.Last != want {
		t.Errorf("last = %+v, want %+v", b.Last, want)
	}

	// Nine starts in the last fourteen days, the middle one 7:09pm.
	if b.Usual != "7:10PM" {
		t.Errorf("usual = %q", b.Usual)
	}

	// He has not streamed a Sunday in four weeks.
	if !reflect.DeepEqual(b.Days, []string{"MON", "TUE", "WED", "THU", "FRI", "SAT"}) {
		t.Errorf("days = %v", b.Days)
	}

	// The Saturday slot is behind us, so the next one is tonight's 8pm.
	if b.Next != (NextBroadcast{At: "TUE 8:00PM", Kind: "scheduled", Title: "Bungulating"}) {
		t.Errorf("next = %+v", b.Next)
	}

	if len(b.Strip) != stripDays {
		t.Fatalf("%d days in the strip", len(b.Strip))
	}
	first, sun, mon, today := b.Strip[0], b.Strip[11], b.Strip[12], b.Strip[13]
	if first.Day != "WED" || first.Date != "16 SEP" || first.Hours != 7.9 {
		t.Errorf("first day = %+v", first)
	}
	if sun.Day != "SUN" || sun.Hours != 0 || sun.Fill != 0 {
		t.Errorf("sunday = %+v", sun)
	}
	if mon.Hours != 7.4 || mon.Fill != 61 {
		t.Errorf("monday = %+v", mon)
	}
	if !today.Today || today.Day != "TUE" || today.Hours != 0 {
		t.Errorf("today = %+v", today)
	}
	for _, d := range b.Strip[:13] {
		if d.Today {
			t.Errorf("%s %s marked as today", d.Day, d.Date)
		}
	}
}

// Without a schedule the next one is a guess, on the next usual day whose usual
// start is still ahead.
func TestNextBroadcastGuesses(t *testing.T) {
	past := pastFixture(t)
	cases := map[string]string{
		"2026-09-29 19:01": "TUE 7:10PM", // a usual day, and not yet
		"2026-09-29 20:30": "WED 7:10PM", // late tonight with nothing on
		"2026-09-27 12:00": "MON 7:10PM", // a Sunday, which he takes off
	}
	for when, wantAt := range cases {
		now := at(t, when)
		b := buildBroadcasts(before(past, now), OnAir{}, now)
		if b.Next != (NextBroadcast{At: wantAt, Kind: "guess"}) {
			t.Errorf("at %s: next = %+v, want %s guess", when, b.Next, wantAt)
		}
	}

	// A Saturday he has already streamed goes to Monday, past the Sunday off.
	now := at(t, "2026-09-27 01:00")
	b := buildBroadcasts(before(past, now), OnAir{}, now)
	if b.Next.At != "MON 7:10PM" {
		t.Errorf("after Saturday's stream: %+v", b.Next)
	}
}

// Live now and not yet in the archive, which runs up to an hour behind. The
// banner's own start fills it in, and tonight's slot is this broadcast.
func TestBuildBroadcastsLiveAheadOfTheArchive(t *testing.T) {
	now := at(t, "2026-09-29 19:40")
	live := OnAir{Live: true, Title: "TUESDAY", started: utc(t, "2026-09-29T23:19:05Z")}
	b := buildBroadcasts(pastFixture(t, "2026-09-30T00:00:00Z", "2026-10-01T00:00:00Z"), live, now)

	if b.Last != (LastBroadcast{Start: "TUE 7:19PM", Ran: "21M", Title: "TUESDAY", Live: true}) {
		t.Errorf("last = %+v", b.Last)
	}
	if b.Next.At != "WED 8:00PM" || b.Next.Kind != "scheduled" {
		t.Errorf("next = %+v, tonight's slot is the one he is on", b.Next)
	}
	if today := b.Strip[len(b.Strip)-1]; today.Hours != 0.3 || !today.Today {
		t.Errorf("today = %+v", today)
	}
}

// Live and already in the archive, where the archive's length is whatever it
// was when Twitch was last asked.
func TestBuildBroadcastsLiveInTheArchive(t *testing.T) {
	now := at(t, "2026-09-29 21:19")
	past := pastFixture(t)
	past.vods = append([]pastBroadcast{{start: utc(t, "2026-09-29T23:19:08Z"), length: 40 * time.Minute, title: "TUESDAY"}}, past.vods...)
	live := OnAir{Live: true, started: utc(t, "2026-09-29T23:19:02Z")}

	b := buildBroadcasts(past, live, now)
	if !b.Last.Live || b.Last.Start != "TUE 7:19PM" || b.Last.Ran != "2H 0M" {
		t.Errorf("last = %+v", b.Last)
	}
	if b.Next.At != "WED 7:10PM" {
		t.Errorf("next = %+v, today is taken", b.Next)
	}

	// The same archive once he has gone off is just the last broadcast.
	off := buildBroadcasts(past, OnAir{}, now)
	if off.Last.Live || off.Last.Ran != "40M" {
		t.Errorf("offline last = %+v", off.Last)
	}
}

// A broadcast that starts after midnight belongs to the night before, and the
// usual start of three late nights is late at night rather than noon.
func TestStreamDayTurnsAtSixInTheMorning(t *testing.T) {
	past := twitchPast{vods: []pastBroadcast{
		{start: at(t, "2026-09-24 01:00"), length: 3 * time.Hour},
		{start: at(t, "2026-09-23 00:30"), length: 3 * time.Hour},
		{start: at(t, "2026-09-21 23:00"), length: 3 * time.Hour},
	}}
	b := buildBroadcasts(past, OnAir{}, at(t, "2026-09-24 12:00"))
	if b.Usual != "12:30AM" {
		t.Errorf("usual = %q", b.Usual)
	}
	var on []string
	for _, d := range b.Strip {
		if d.Hours > 0 {
			on = append(on, d.Day)
		}
	}
	if strings.Join(on, " ") != "MON TUE WED" {
		t.Errorf("streamed on %v", on)
	}
	// Two weeks of archive or less is not a weekly pattern.
	if b.Days != nil || b.Next != (NextBroadcast{}) {
		t.Errorf("days %v next %+v from three days of archive", b.Days, b.Next)
	}
}

func TestSpan(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0:                            "0M",
		59 * time.Minute:             "59M",
		7*time.Hour + 21*time.Minute: "7H 21M",
		13*time.Hour + 30*time.Second + 29*time.Minute: "13H 30M",
	} {
		if got := span(d); got != want {
			t.Errorf("span(%s) = %q, want %q", d, got, want)
		}
	}
}

// The archive rides the banner's endpoint, and a Twitch that stops
// understanding the query has to show on UPLINK without touching the budget or
// the breaker the banner falls back on.
func TestBroadcastsFailureShowsOnItsOwnRow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Client-ID") != twitchClientID {
			t.Errorf("Client-ID = %q", r.Header.Get("Client-ID"))
		}
		fmt.Fprint(w, `{"errors":[{"message":"Cannot query field \"videos\" on type \"User\"."}]}`)
	}))
	defer srv.Close()

	g := NewGuard(t.TempDir())
	if _, err := fetchBroadcasts(t.Context(), g, srv.URL); err == nil {
		t.Fatal("a GraphQL error should be a failure")
	}

	states := map[string]string{}
	for _, f := range g.Feeds(time.Now()) {
		states[f.Name] = f.State
	}
	if states["TWITCH VODS"] != "degraded" {
		t.Errorf("TWITCH VODS = %q, want degraded", states["TWITCH VODS"])
	}
	if states["TWITCH"] != "idle" {
		t.Errorf("TWITCH = %q, the banner's row should not have moved", states["TWITCH"])
	}
}

// chat reads dash by the state's own keys, so this is the name it asks for.
func TestOnAirIsOneSection(t *testing.T) {
	st := State{OnAir: OnAir{Live: true, Name: streamerName}}
	st.OnAir.Broadcasts = buildBroadcasts(pastFixture(t), st.OnAir, at(t, "2026-09-29 19:01"))
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	var onair map[string]any
	if err := json.Unmarshal(raw["onair"], &onair); err != nil {
		t.Fatalf("no onair section: %v", err)
	}
	if onair["live"] != true {
		t.Errorf("onair.live = %v", onair["live"])
	}
	broadcasts, ok := onair["broadcasts"].(map[string]any)
	if !ok {
		t.Fatalf("onair carries no broadcasts: %v", onair)
	}
	for _, k := range []string{"last", "usual_start", "usual_days", "next", "days"} {
		if _, ok := broadcasts[k]; !ok {
			t.Errorf("broadcasts is missing %s", k)
		}
	}
}
