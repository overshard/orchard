package main

import (
	"fmt"
	"strconv"
	"time"
)

// modeFor is how long watching only or off can run before gate goes back to
// enforcing by itself. There's no forever, so a forgotten switch ends.
var modeFor = map[string]time.Duration{"1h": time.Hour, "4h": 4 * time.Hour, "1d": 24 * time.Hour}

// modeSetting is the mode and when it runs out, putting gate back to
// enforcing in the database once the time has passed. A watch or off with no
// end, from before modes ended, gets a day from now.
func (s *site) modeSetting(now time.Time) (string, int64) {
	mode := s.store.Setting("mode")
	if mode != modeWatch && mode != modeOff {
		return modeOn, 0
	}
	until, _ := strconv.ParseInt(s.store.Setting("mode_until"), 10, 64)
	if until == 0 {
		until = now.Add(24 * time.Hour).Unix()
		_ = s.store.SetSetting("mode_until", strconv.FormatInt(until, 10))
	}
	if now.Unix() >= until {
		_ = s.store.SetSetting("mode", modeOn)
		_ = s.store.SetSetting("mode_until", "0")
		s.store.Change("gate", "gate", "mode on, "+modeName(mode)+" ran out")
		return modeOn, 0
	}
	return mode, until
}

func modeName(m string) string {
	switch m {
	case modeWatch:
		return "watching only"
	case modeOff:
		return "off"
	}
	return "enforcing"
}

// left is a countdown the status line can show, like 3h 12m.
func left(until int64, now time.Time) string {
	d := time.Unix(until, 0).Sub(now)
	if d < time.Minute {
		return "under a minute"
	}
	h, m := int(d/time.Hour), int(d%time.Hour/time.Minute)
	if h == 0 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}
