package main

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// steamChart remembers which game held the top of the Steam chart each day, so
// the glance read can tell an ordinary day at the top from one where something
// has pushed the usual game off it. Steam publishes no chart history itself.
type steamChart struct {
	mu   sync.Mutex
	path string

	// New York day to the games seen at No. 1 that day, by how many polls.
	Days  map[string]map[string]int `json:"days"`
	Names map[string]string         `json:"names"`
}

const (
	chartKeep = 30
	// The window "usual" is judged over, and how much of it has to be on file
	// before anything is called unusual, so a fresh volume doesn't flag day one.
	chartWindow  = 14
	chartMinDays = 3
)

func openSteamChart(dataDir string) *steamChart {
	c := &steamChart{path: filepath.Join(dataDir, "steam.json"), Days: map[string]map[string]int{}, Names: map[string]string{}}
	if raw, err := os.ReadFile(c.path); err == nil {
		if err := json.Unmarshal(raw, c); err != nil {
			slog.Warn("steam chart unreadable", slog.String("component", "steam"), slog.Any("err", err))
		}
	}
	return c
}

// record counts this poll's No. 1 and returns the usual one over the days
// before today, empty until there's enough on file to say.
func (c *steamChart) record(games []Game, now time.Time) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(games) == 0 {
		return c.usual(now)
	}
	day := now.In(easternTime()).Format("2006-01-02")
	if c.Days[day] == nil {
		c.Days[day] = map[string]int{}
	}
	top := games[0]
	c.Days[day][top.URL]++
	c.Names[top.URL] = top.Name

	cutoff := now.In(easternTime()).AddDate(0, 0, -chartKeep).Format("2006-01-02")
	for d := range c.Days {
		if d < cutoff {
			delete(c.Days, d)
		}
	}
	c.save()
	return c.usual(now)
}

// usual is the game at No. 1 on the most of the last two weeks' days, not
// counting today, since today is what it's being compared against.
func (c *steamChart) usual(now time.Time) string {
	today := now.In(easternTime())
	held := map[string]int{}
	days := 0
	for i := 1; i <= chartWindow; i++ {
		polls := c.Days[today.AddDate(0, 0, -i).Format("2006-01-02")]
		if len(polls) == 0 {
			continue
		}
		days++
		best, most := "", 0
		for url, n := range polls {
			if n > most || n == most && url < best {
				best, most = url, n
			}
		}
		held[best]++
	}
	if days < chartMinDays {
		return ""
	}
	best, most := "", 0
	for url, n := range held {
		if n > most || n == most && url < best {
			best, most = url, n
		}
	}
	// A top that changes hands every few days has no usual to beat.
	if most*2 < days {
		return ""
	}
	return c.Names[best]
}

func (c *steamChart) save() {
	raw, err := json.Marshal(c)
	if err != nil {
		return
	}
	tmp := c.path + ".tmp"
	if err = os.WriteFile(tmp, raw, 0o644); err == nil {
		err = os.Rename(tmp, c.path)
	}
	if err != nil {
		slog.Warn("steam chart not saved", slog.String("component", "steam"), slog.Any("err", err))
	}
}
