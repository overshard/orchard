// What Claude costs, kept per call in a ledger with no prompt in it, so an
// incognito call still counts against the month and the call log can age out
// without losing the money.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"

	"llm.bythewood.me/ntfy"
)

const spendSchema = `
CREATE TABLE IF NOT EXISTS spend (
  id      INTEGER PRIMARY KEY,
  key_id  INTEGER NOT NULL DEFAULT 0,
  micros  INTEGER NOT NULL,
  at      INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS spend_at ON spend(at);
-- One row per alert sent, so each one goes out once per month or week.
CREATE TABLE IF NOT EXISTS spend_alerts (
  kind TEXT PRIMARY KEY,
  at   INTEGER NOT NULL
);
`

// Haiku 5.5 per million tokens, in millionths of a dollar. A prompt over 100K
// tokens bills the whole call at the second card.
const (
	inMicros      = 100_000
	outMicros     = 500_000
	longInMicros  = 500_000
	longOutMicros = 2_500_000
	longPrompt    = 100_000
	searchMicros  = 10_000
)

// costMicros prices one API response. Cache writes are 1.25 times the input
// rate and cache reads a tenth of it.
func costMicros(u anthropic.Usage) int64 {
	in, out := int64(inMicros), int64(outMicros)
	if u.InputTokens+u.CacheCreationInputTokens+u.CacheReadInputTokens > longPrompt {
		in, out = longInMicros, longOutMicros
	}
	tok := u.InputTokens*in + u.CacheCreationInputTokens*in*5/4 + u.CacheReadInputTokens*in/10 + u.OutputTokens*out
	return tok/1_000_000 + u.ServerToolUse.WebSearchRequests*searchMicros
}

func (s *Store) LogSpend(keyID, micros int64) {
	if micros <= 0 {
		return
	}
	if _, err := s.db.Exec(`INSERT INTO spend (key_id, micros, at) VALUES (?,?,?)`,
		keyID, micros, time.Now().Unix()); err != nil {
		slog.Error("logging spend failed", "err", err)
	}
}

func (s *Store) SpentSince(t time.Time) (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COALESCE(SUM(micros),0) FROM spend WHERE at >= ?`, t.Unix()).Scan(&n)
	return n, err
}

// alertOnce records kind and says whether it was new.
func (s *Store) alertOnce(kind string) bool {
	r, err := s.db.Exec(`INSERT OR IGNORE INTO spend_alerts (kind, at) VALUES (?,?)`, kind, time.Now().Unix())
	if err != nil {
		slog.Error("recording a spend alert", "err", err)
		return false
	}
	n, _ := r.RowsAffected()
	return n == 1
}

// The credit renews with the calendar month, read in Isaac's time zone.
var budgetZone = mustZone("America/New_York")

func mustZone(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}

// Month is where the spend stands against the budget.
type Month struct {
	Spent     float64
	Budget    float64
	Projected float64
	Start     time.Time
	Elapsed   float64
}

func monthOf(now time.Time) (start, end time.Time) {
	n := now.In(budgetZone)
	start = time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, budgetZone)
	return start, start.AddDate(0, 1, 0)
}

func (s *site) month(now time.Time) (Month, error) {
	start, end := monthOf(now)
	micros, err := s.store.SpentSince(start)
	if err != nil {
		return Month{}, err
	}
	m := Month{Spent: float64(micros) / 1e6, Budget: s.budget, Start: start,
		Elapsed: float64(now.Sub(start)) / float64(end.Sub(start))}
	if m.Elapsed > 0 {
		m.Projected = m.Spent / m.Elapsed
	}
	return m, nil
}

// The pace alert waits until the month has enough behind it, since a busy first
// morning projects to a fortune.
const (
	paceAfter = 3 * 24 * time.Hour
	checkLoop = 30 * time.Minute
)

// watchSpend checks the month on a timer and says so over ntfy when it runs
// hot, crosses a share of the budget, and once a week either way.
func (s *site) watchSpend(ctx context.Context, n *ntfy.Client) {
	t := time.NewTicker(checkLoop)
	defer t.Stop()
	for {
		for _, m := range s.spendAlerts(time.Now()) {
			if err := n.Publish(ctx, m); err != nil {
				slog.Error("sending a spend alert", "err", err, "title", m.Title)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// spendAlerts decides what to send now, recording each so it goes out once.
func (s *site) spendAlerts(now time.Time) []ntfy.Message {
	m, err := s.month(now)
	if err != nil || m.Budget <= 0 {
		return nil
	}
	month := m.Start.Format("2006-01")
	line := fmt.Sprintf("$%.2f of $%.0f spent this month, on pace for $%.2f.", m.Spent, m.Budget, m.Projected)
	var out []ntfy.Message
	// Highest first, and a step that fires marks the ones under it, so going
	// straight to 85% sends one alert and not two.
	steps := []struct {
		share    float64
		kind     string
		priority string
	}{{1, "over", "urgent"}, {0.8, "80", "high"}, {0.5, "50", "default"}}
	for i, step := range steps {
		if m.Spent < m.Budget*step.share {
			continue
		}
		if s.store.alertOnce(month + "-" + step.kind) {
			out = append(out, ntfy.Message{Title: fmt.Sprintf("llm has spent %.0f%% of the month's budget", 100*m.Spent/m.Budget),
				Body: line, Priority: step.priority, Tags: "money_with_wings", Link: "https://llm.bythewood.me", Label: "Open llm"})
		}
		for _, lower := range steps[i+1:] {
			s.store.alertOnce(month + "-" + lower.kind)
		}
		break
	}
	if m.Projected > m.Budget && now.Sub(m.Start) >= paceAfter && s.store.alertOnce(month+"-pace") {
		out = append(out, ntfy.Message{Title: "llm is running hot", Body: line, Priority: "high",
			Tags: "fire", Link: "https://llm.bythewood.me", Label: "Open llm"})
	}
	// A Monday morning line either way, so quiet means the watch is still on.
	local := now.In(budgetZone)
	if local.Weekday() == time.Monday && local.Hour() >= 9 {
		y, w := local.ISOWeek()
		if s.store.alertOnce(fmt.Sprintf("week-%d-%02d", y, w)) {
			title := "llm spend is on track"
			if m.Projected > m.Budget {
				title = "llm spend is running hot"
			}
			out = append(out, ntfy.Message{Title: title, Body: line, Priority: "low", Tags: "chart_with_upwards_trend"})
		}
	}
	return out
}
