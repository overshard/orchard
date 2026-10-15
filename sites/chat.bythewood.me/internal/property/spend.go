package property

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// A model that misreads a street name will spell it six ways in one turn, each a
// cold lookup of dozens of requests, and only a ceiling bounds the total. Six an
// hour stops a loop and twenty five a day is more house hunting than anybody does.
const (
	coldPerHour = 6
	coldPerDay  = 25
)

const spendSchema = `
-- One row per address this went out and looked up, which is what the ceiling
-- counts. In the database rather than in memory because a restart handing back a
-- fresh allowance is how a limit becomes a suggestion, and a deploy is a restart.
CREATE TABLE IF NOT EXISTS assessments (
    key        TEXT NOT NULL,
    started_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS assessments_at ON assessments(started_at);
`

// ErrSpent is distinct so a tool can say what's left and when instead of reading
// as a failure.
type ErrSpent struct {
	Window string
	Done   int
	Ceil   int
	Until  time.Time
}

func (e ErrSpent) Error() string {
	return fmt.Sprintf(
		"%d addresses have already been looked up this %s, which is the ceiling of %d. "+
			"It clears around %s. Anything looked up before still answers straight away, "+
			"so ask about one of those, and do not try this address again until then",
		e.Done, e.Window, e.Ceil, e.Until.UTC().Format("15:04 UTC"))
}

func (e *Engine) spend(ctx context.Context, key string) error {
	e.spendMu.Lock()
	defer e.spendMu.Unlock()

	now := time.Now()
	hour, err := e.countSince(ctx, now.Add(-time.Hour))
	if err != nil {
		return err
	}
	if hour >= coldPerHour {
		return ErrSpent{Window: "hour", Done: hour, Ceil: coldPerHour, Until: now.Add(time.Hour)}
	}
	day, err := e.countSince(ctx, now.Add(-24*time.Hour))
	if err != nil {
		return err
	}
	if day >= coldPerDay {
		return ErrSpent{Window: "day", Done: day, Ceil: coldPerDay, Until: now.Add(24 * time.Hour)}
	}

	// Still counted, so incognito can't spend past the ceiling, but not named.
	if incognito(ctx) {
		key = ""
	}
	if _, err := e.db.ExecContext(ctx,
		`INSERT INTO assessments (key, started_at) VALUES (?,?)`, key, now.Unix()); err != nil {
		return err
	}
	// Nothing past a day is ever read.
	_, _ = e.db.ExecContext(ctx,
		`DELETE FROM assessments WHERE started_at < ?`, now.Add(-48*time.Hour).Unix())
	return nil
}

func (e *Engine) countSince(ctx context.Context, since time.Time) (int, error) {
	var n int
	err := e.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM assessments WHERE started_at >= ?`, since.Unix()).Scan(&n)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return n, err
}

// Spend is what's left, for a tool that wants to say so before it's refused.
func (e *Engine) Spend(ctx context.Context) (hourLeft, dayLeft int) {
	now := time.Now()
	hour, _ := e.countSince(ctx, now.Add(-time.Hour))
	day, _ := e.countSince(ctx, now.Add(-24*time.Hour))
	return max0(coldPerHour - hour), max0(coldPerDay - day)
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}
