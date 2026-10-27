package main

import (
	"context"
	"time"
)

// Source is one poller. Idle, when set, is the slower interval it drops to
// while nobody has the page open, and Primed means Prime already ran it, so
// its loop waits out an interval before the first run.
type Source struct {
	Name   string
	Every  time.Duration
	Idle   time.Duration
	Primed bool
	Run    func(s *Store, ctx context.Context, g *Guard)
}

// sources is every poller Run starts. The hub has one wake channel, so only
// one source can have an Idle interval.
var sources = []Source{
	// An empty page slows markets to a keep-warm poll instead of stopping it, so
	// the first visitor after a quiet night opens onto real numbers.
	{Name: "market", Every: marketWatched, Idle: marketIdle, Primed: true, Run: (*Store).refreshMarket},
	{Name: "onair", Every: onAirEvery, Run: (*Store).refreshOnAir},
	{Name: "broadcasts", Every: broadcastsEvery, Run: (*Store).refreshBroadcasts},
	{Name: "news", Every: newsEvery, Run: (*Store).refreshNews},
	{Name: "signal", Every: signalEvery, Primed: true, Run: (*Store).refreshSignal},
	{Name: "board", Every: boardEvery, Run: (*Store).refreshBoard},
	{Name: "earnings", Every: earningsEvery, Run: (*Store).refreshEarnings},
	{Name: "reports", Every: reportsEvery, Run: (*Store).refreshReports},
	{Name: "alerts", Every: alertsEvery, Run: (*Store).refreshAlerts},
	{Name: "air", Every: airEvery, Run: (*Store).refreshAir},
	{Name: "pollen", Every: pollenEvery, Run: (*Store).refreshPollen},
	{Name: "outdoors", Every: outdoorsEvery, Run: (*Store).refreshOutlook},
	{Name: "steam", Every: steamEvery, Run: (*Store).refreshSteam},
	{Name: "streaming", Every: streamingEvery, Run: (*Store).refreshStreaming},
	{Name: "weather", Every: weatherEvery, Run: (*Store).refreshWeather},
	{Name: "systems", Every: probeEvery, Run: (*Store).refreshSystems},
	{Name: "calendar", Every: calendarEvery, Run: (*Store).refreshCalendar},
}

func (src Source) interval(watching bool) time.Duration {
	if src.Idle > 0 && !watching {
		return src.Idle
	}
	return src.Every
}

// loop runs one source on its interval. A source with an Idle interval also
// wakes when the first browser of a quiet spell connects, or a visitor arriving
// just after an idle timer was set would watch a stale page for the whole of it.
func (s *Store) loop(ctx context.Context, g *Guard, src Source) {
	var wake <-chan struct{}
	if src.Idle > 0 {
		wake = s.hub.wake
	}

	last := time.Now()
	if !src.Primed {
		src.Run(s, ctx, g)
	}

	for {
		t := time.NewTimer(src.interval(s.hub.Watching() > 0))
		select {
		case <-ctx.Done():
			t.Stop()
			return

		case <-t.C:
			src.Run(s, ctx, g)
			last = time.Now()

		case <-wake:
			t.Stop()
			// A viewer arriving right after a fetch does not need another one,
			// and the hub has already replayed the current state to them.
			if time.Since(last) >= src.Every {
				src.Run(s, ctx, g)
				last = time.Now()
			}
		}
	}
}
