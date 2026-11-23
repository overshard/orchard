// What Claude costs, kept per call in a ledger with no prompt in it, so an
// incognito call still counts against the month and the call log can age out
// without losing the money.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
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

// costParts is one response's bill by what it paid for, the token parts in
// millionths of a micro so they add up to exactly what costMicros charges.
// Cache writes are 1.25 times the input rate and cache reads a tenth of it.
type costParts struct {
	In, CacheWrite, CacheRead, Out int64
	Search                         int64
}

func priceParts(u anthropic.Usage) costParts {
	in, out := int64(inMicros), int64(outMicros)
	if u.InputTokens+u.CacheCreationInputTokens+u.CacheReadInputTokens > longPrompt {
		in, out = longInMicros, longOutMicros
	}
	return costParts{In: u.InputTokens * in, CacheWrite: u.CacheCreationInputTokens * in * 5 / 4,
		CacheRead: u.CacheReadInputTokens * in / 10, Out: u.OutputTokens * out,
		Search: u.ServerToolUse.WebSearchRequests * searchMicros}
}

func (p costParts) micros() int64 {
	return (p.In+p.CacheWrite+p.CacheRead+p.Out)/1_000_000 + p.Search
}

// costMicros prices one API response.
func costMicros(u anthropic.Usage) int64 { return priceParts(u).micros() }

// tokenSplit is what a call's prompt was made of, summed over every response
// it took, which is what the bill counts.
type tokenSplit struct {
	InTok      int64 `json:"input_uncached"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
	Searches   int64 `json:"searches"`
}

func (t tokenSplit) recorded() bool { return t.InTok+t.CacheRead+t.CacheWrite > 0 }

// spendRow is one priced call in the ledger, with the call it paid for when
// that call was written down.
type spendRow struct {
	KeyID, CallID, Micros int64
	tokenSplit
	OutTok int64
}

func (s *Store) LogSpend(keyID, micros int64) {
	s.logSpend(spendRow{KeyID: keyID, Micros: micros})
}

func (s *Store) logSpend(r spendRow) {
	if r.Micros <= 0 {
		return
	}
	if _, err := s.db.Exec(`INSERT INTO spend (key_id, call_id, micros, in_tok, cache_read, cache_write, out_tok, searches, at)
		VALUES (?,?,?,?,?,?,?,?,?)`, r.KeyID, r.CallID, r.Micros, r.InTok, r.CacheRead, r.CacheWrite, r.OutTok, r.Searches,
		time.Now().Unix()); err != nil {
		slog.Error("logging spend failed", "err", err)
	}
}

// KeySpentSince is one key's share of the ledger, for its monthly cap.
func (s *Store) KeySpentSince(id int64, t time.Time) (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COALESCE(SUM(micros),0) FROM spend WHERE key_id=? AND at >= ?`, id, t.Unix()).Scan(&n)
	return n, err
}

// FirstSpend is when the ledger starts, zero when it is empty.
func (s *Store) FirstSpend() (time.Time, error) {
	var at sql.NullInt64
	if err := s.db.QueryRow(`SELECT MIN(at) FROM spend`).Scan(&at); err != nil || !at.Valid {
		return time.Time{}, err
	}
	return time.Unix(at.Int64, 0), nil
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
	// PerDay is the average daily spend over the last RateDays, at most a
	// week, and Next30 is that times 30.
	PerDay, RateDays, Next30 float64
}

const rateWindow = 7 * 24 * time.Hour

// RateLabel says which days Next30 was read from.
func (m Month) RateLabel() string {
	if d := math.Round(m.RateDays); d > 1 {
		return fmt.Sprintf("the last %.0f days' rate", d)
	}
	return "the last day's rate"
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
	if err := s.rate(&m, now); err != nil {
		return Month{}, err
	}
	return m, nil
}

// rate fills in the recent daily spend. A ledger younger than the window is
// averaged over the days it has, and never over less than one.
func (s *site) rate(m *Month, now time.Time) error {
	first, err := s.store.FirstSpend()
	if err != nil || first.IsZero() {
		return err
	}
	since := now.Add(-rateWindow)
	if first.After(since) {
		since = first
	}
	micros, err := s.store.SpentSince(since)
	if err != nil {
		return err
	}
	m.RateDays = max(1, now.Sub(since).Hours()/24)
	m.PerDay = float64(micros) / 1e6 / m.RateDays
	m.Next30 = 30 * m.PerDay
	return nil
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
			if err := n.Deliver(ctx, m); err != nil {
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
