package main

import (
	"database/sql"
	"strings"
	"time"
)

// The rain table keeps a month of which ten minute slots rained and how hard,
// so something can be made of rain that has already fallen.
const (
	rainSlotSecs = 600
	rainKept     = 30
)

func rainSlot(t time.Time) int64 {
	return t.Unix() / rainSlotSecs
}

// wetOf is 0 for a dry reading, 1 for drizzle, 2 for rain or showers and 3
// for thunder or a heavy chance of rain. Anything above 0 is a rainy day.
func wetOf(r *reading) int {
	if r == nil {
		return 0
	}
	cond := strings.ToLower(r.Cond)
	switch {
	case strings.Contains(cond, "thunder") || r.Rain >= 80:
		return 3
	case strings.Contains(cond, "rain") || strings.Contains(cond, "shower"):
		return 2
	case strings.Contains(cond, "drizzle"):
		return 1
	case r.Rain >= 60:
		return 2
	}
	return 0
}

// keepRain writes the slot it is now if the reading is fresh and wet. A slot
// is only ever written once, however many polls land in it.
func (s *store) keepRain() error {
	now := s.now()
	wet := wetOf(s.day.reading(now))
	if wet == 0 {
		return nil
	}
	return s.write(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO rain (slot, wet) VALUES (?, ?) ON CONFLICT (slot) DO NOTHING`,
			rainSlot(now), wet)
		return err
	})
}
