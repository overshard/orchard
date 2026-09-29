package main

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// His past broadcasts and posted schedule, off the banner's GraphQL endpoint but
// under a guard key of its own, so a failure here shows on UPLINK without
// opening the breaker the banner's fallback depends on.
const (
	broadcastsKey   = "twitchvods"
	broadcastsEvery = time.Hour

	// A broadcast belongs to the evening it started on, so a stream day turns
	// over at 6am and one that starts at 1am counts toward the night before.
	streamDayStart = 6 * time.Hour

	stripDays    = 14
	stripCeiling = 12.0

	// Days looked back over for the usual start and for the usual weekdays.
	usualStartDays = 14
	usualWeekDays  = 28
)

const broadcastsQuery = `query Broadcasts($login: String!) {
  user(login: $login) {
    videos(first: 30, type: ARCHIVE, sort: TIME) { edges { node { createdAt lengthSeconds title } } }
    channel { schedule { segments { startAt endAt title } } }
  }
}`

// Broadcasts is his pattern as the ON AIR panel shows it. It sits inside OnAir
// so the `onair` section of the state carries both what is live and what is
// usual.
type Broadcasts struct {
	Last  LastBroadcast `json:"last"`
	Usual string        `json:"usual_start"`
	Days  []string      `json:"usual_days"`
	Next  NextBroadcast `json:"next"`
	Strip []StreamDay   `json:"days"`
}

type LastBroadcast struct {
	Start string `json:"start"`
	Ran   string `json:"ran"`
	Title string `json:"title"`
	Live  bool   `json:"live"`
}

// Kind is "scheduled" when the time came off his own Twitch schedule and
// "guess" when it was worked out from the usual start and the usual days.
type NextBroadcast struct {
	At    string `json:"at"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
}

type StreamDay struct {
	Day   string  `json:"day"`
	Date  string  `json:"date"`
	Hours float64 `json:"hours"`
	Fill  int     `json:"fill"`
	Today bool    `json:"today"`
}

type pastBroadcast struct {
	start  time.Time
	length time.Duration
	title  string
	live   bool
}

type slot struct {
	start time.Time
	title string
}

// twitchPast is the archive and the schedule as fetched, kept so the panel can
// be rebuilt against every banner poll without asking Twitch again.
type twitchPast struct {
	vods  []pastBroadcast
	slots []slot
}

type broadcastsPayload struct {
	Data struct {
		User *struct {
			Videos struct {
				Edges []struct {
					Node struct {
						CreatedAt     time.Time `json:"createdAt"`
						LengthSeconds int       `json:"lengthSeconds"`
						Title         string    `json:"title"`
					} `json:"node"`
				} `json:"edges"`
			} `json:"videos"`
			Channel *struct {
				Schedule *struct {
					Segments []struct {
						StartAt time.Time `json:"startAt"`
						Title   string    `json:"title"`
					} `json:"segments"`
				} `json:"schedule"`
			} `json:"channel"`
		} `json:"user"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func fetchBroadcasts(ctx context.Context, g *Guard, url string) (twitchPast, error) {
	body := map[string]any{
		"query":     broadcastsQuery,
		"variables": map[string]string{"login": twitchLogin},
	}
	var p broadcastsPayload
	if err := postJSONHeaders(ctx, g, broadcastsKey, url, map[string]string{"Client-ID": twitchClientID}, body, &p); err != nil {
		return twitchPast{}, err
	}
	past, err := parseBroadcasts(p)
	if err != nil {
		// GraphQL reports a query it no longer understands with a 200, which
		// the guard has already counted as a success.
		g.Fail(broadcastsKey, 0, 0)
	}
	return past, err
}

func parseBroadcasts(p broadcastsPayload) (twitchPast, error) {
	if len(p.Errors) > 0 {
		return twitchPast{}, fmt.Errorf("%s: %s", broadcastsKey, p.Errors[0].Message)
	}
	u := p.Data.User
	if u == nil {
		return twitchPast{}, fmt.Errorf("%s: no user %s", broadcastsKey, twitchLogin)
	}

	var past twitchPast
	for _, e := range u.Videos.Edges {
		if e.Node.CreatedAt.IsZero() {
			continue
		}
		past.vods = append(past.vods, pastBroadcast{
			start:  e.Node.CreatedAt,
			length: time.Duration(e.Node.LengthSeconds) * time.Second,
			title:  e.Node.Title,
		})
	}
	sort.Slice(past.vods, func(i, j int) bool { return past.vods[i].start.After(past.vods[j].start) })

	if u.Channel != nil && u.Channel.Schedule != nil {
		for _, s := range u.Channel.Schedule.Segments {
			if !s.StartAt.IsZero() {
				past.slots = append(past.slots, slot{start: s.StartAt, title: s.Title})
			}
		}
	}
	sort.Slice(past.slots, func(i, j int) bool { return past.slots[i].start.Before(past.slots[j].start) })
	return past, nil
}

// buildBroadcasts reads the archive against what the banner last saw. The
// banner polls every ten minutes and the archive hourly, so whether he is on
// now comes from the banner.
func buildBroadcasts(past twitchPast, live OnAir, now time.Time) Broadcasts {
	vods := withLive(past.vods, live, now)

	b := Broadcasts{Strip: streamStrip(vods, now)}
	switch {
	case len(vods) > 0 && (vods[0].live || !live.Live):
		v := vods[0]
		b.Last = LastBroadcast{Start: dayClock(v.start), Ran: span(v.length), Title: v.title, Live: v.live}
	case live.Live:
		b.Last = LastBroadcast{Title: live.Title, Live: true}
	}

	usual, known := usualStart(vods, now)
	if known {
		b.Usual = strings.ToUpper(wallClock(usual).Format("3:04pm"))
	}
	days := usualDays(vods, now)
	for _, d := range days {
		b.Days = append(b.Days, strings.ToUpper(d.String()[:3]))
	}
	b.Next = nextBroadcast(past.slots, vods, usual, known, days, now)
	return b
}

// withLive marks the broadcast that is on now. The archive lags a live stream
// by up to an hour, so one the banner can see and the archive cannot is added
// from the banner.
func withLive(vods []pastBroadcast, live OnAir, now time.Time) []pastBroadcast {
	out := append([]pastBroadcast(nil), vods...)
	if !live.Live {
		return out
	}
	if len(out) > 0 && sameBroadcast(out[0], live, now) {
		out[0].live = true
		// The banner's start, so the panel and the banner give the same minute.
		if !live.started.IsZero() {
			out[0].start = live.started
		}
		if ran := now.Sub(out[0].start); ran > out[0].length {
			out[0].length = ran
		}
		return out
	}
	if live.started.IsZero() {
		return out
	}
	return append([]pastBroadcast{{start: live.started, length: now.Sub(live.started), title: live.Title, live: true}}, out...)
}

// sameBroadcast matches the newest archive to the stream the banner has. The
// two platforms stamp the start a few seconds apart.
func sameBroadcast(v pastBroadcast, live OnAir, now time.Time) bool {
	if live.started.IsZero() {
		return now.Sub(v.start.Add(v.length)) < onAirGrace
	}
	d := v.start.Sub(live.started)
	return d > -onAirGrace && d < onAirGrace
}

func streamDay(t time.Time) time.Time {
	s := t.In(easternTime()).Add(-streamDayStart)
	return time.Date(s.Year(), s.Month(), s.Day(), 0, 0, 0, 0, easternTime())
}

func dayKey(t time.Time) string { return streamDay(t).Format("2006-01-02") }

// minutesIntoDay counts from 6am, so a stream just after midnight sorts after
// the evening ones and a median over them stays in the evening.
func minutesIntoDay(t time.Time) int {
	e := t.In(easternTime())
	m := e.Hour()*60 + e.Minute() - int(streamDayStart/time.Minute)
	if m < 0 {
		m += 24 * 60
	}
	return m
}

func wallClock(minutes int) time.Time {
	m := (minutes + int(streamDayStart/time.Minute)) % (24 * 60)
	return time.Date(2000, 1, 1, m/60, m%60, 0, 0, time.UTC)
}

// usualStart is the median start over the last two weeks, to the nearest five
// minutes. Fewer than three broadcasts is not a habit.
func usualStart(vods []pastBroadcast, now time.Time) (int, bool) {
	from := streamDay(now).AddDate(0, 0, -(usualStartDays - 1))
	var mins []int
	for _, v := range vods {
		if !streamDay(v.start).Before(from) {
			mins = append(mins, minutesIntoDay(v.start))
		}
	}
	if len(mins) < 3 {
		return 0, false
	}
	sort.Ints(mins)
	n := len(mins)
	median := float64(mins[n/2])
	if n%2 == 0 {
		median = float64(mins[n/2-1]+mins[n/2]) / 2
	}
	return int(math.Round(median/5)) * 5, true
}

// usualDays are the weekdays he streamed on at least half the time over the
// last four weeks, or over as much of them as the archive reaches. Today is
// left out, since a day that is not over cannot count as a day off.
func usualDays(vods []pastBroadcast, now time.Time) []time.Weekday {
	if len(vods) == 0 {
		return nil
	}
	today := streamDay(now)
	from := today.AddDate(0, 0, -usualWeekDays)
	if oldest := streamDay(vods[len(vods)-1].start); oldest.After(from) {
		from = oldest
	}

	streamed := map[string]bool{}
	for _, v := range vods {
		streamed[dayKey(v.start)] = true
	}
	var seen, hits [7]int
	counted := 0
	for d := from; d.Before(today); d = d.AddDate(0, 0, 1) {
		counted++
		seen[d.Weekday()]++
		if streamed[d.Format("2006-01-02")] {
			hits[d.Weekday()]++
		}
	}
	if counted < 7 {
		return nil
	}

	var days []time.Weekday
	for _, wd := range []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday, time.Sunday} {
		if hits[wd] > 0 && hits[wd]*2 >= seen[wd] {
			days = append(days, wd)
		}
	}
	return days
}

// streamStrip is hours on air for each of the last fourteen days, oldest first,
// counted against the day each broadcast started on.
func streamStrip(vods []pastBroadcast, now time.Time) []StreamDay {
	hours := map[string]float64{}
	for _, v := range vods {
		hours[dayKey(v.start)] += v.length.Hours()
	}
	today := streamDay(now)
	out := make([]StreamDay, 0, stripDays)
	for i := stripDays - 1; i >= 0; i-- {
		d := today.AddDate(0, 0, -i)
		h := hours[d.Format("2006-01-02")]
		out = append(out, StreamDay{
			Day:   strings.ToUpper(d.Format("Mon")),
			Date:  strings.ToUpper(d.Format("2 Jan")),
			Hours: math.Round(h*10) / 10,
			Fill:  int(math.Round(math.Min(h/stripCeiling, 1) * 100)),
			Today: i == 0,
		})
	}
	return out
}

// nextBroadcast takes his own schedule first. A slot on a day he has already
// been on is the broadcast he did or is doing, so it is passed over.
func nextBroadcast(slots []slot, vods []pastBroadcast, usual int, known bool, days []time.Weekday, now time.Time) NextBroadcast {
	streamed := map[string]bool{}
	for _, v := range vods {
		streamed[dayKey(v.start)] = true
	}
	for _, s := range slots {
		if s.start.After(now) && !streamed[dayKey(s.start)] {
			return NextBroadcast{At: dayClock(s.start), Kind: "scheduled", Title: s.title}
		}
	}

	if !known || len(days) == 0 {
		return NextBroadcast{}
	}
	usualDay := map[time.Weekday]bool{}
	for _, d := range days {
		usualDay[d] = true
	}
	today := streamDay(now)
	clock := wallClock(usual)
	for i := 0; i <= 7; i++ {
		d := today.AddDate(0, 0, i)
		if streamed[d.Format("2006-01-02")] || !usualDay[d.Weekday()] {
			continue
		}
		at := time.Date(d.Year(), d.Month(), d.Day(), clock.Hour(), clock.Minute(), 0, 0, easternTime())
		if usual+int(streamDayStart/time.Minute) >= 24*60 {
			at = at.AddDate(0, 0, 1)
		}
		if at.After(now) {
			return NextBroadcast{At: dayClock(at), Kind: "guess"}
		}
	}
	return NextBroadcast{}
}

func dayClock(t time.Time) string {
	return strings.ToUpper(t.In(easternTime()).Format("Mon 3:04pm"))
}

func span(d time.Duration) string {
	m := int(d.Round(time.Minute) / time.Minute)
	if m < 60 {
		return fmt.Sprintf("%dM", m)
	}
	return fmt.Sprintf("%dH %dM", m/60, m%60)
}
