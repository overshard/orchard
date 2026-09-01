package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// Events age out after a year. The collector is a public unauthenticated POST
// and the collector id is in the page source of every tracked site, so without
// this the only bound on the table is how long somebody feels like posting.

const (
	// A year, so the dashboard's longest window still has every row behind it.
	eventRetention = 365 * 24 * time.Hour

	// Hourly rather than daily, so a deletion is always small and a machine
	// asleep most of the day still gets one in.
	sweepInterval = time.Hour

	// Rows per DELETE. SQLite holds a database-wide write lock for the whole
	// statement, so an unbounded delete would stall every collect behind it.
	sweepChunk = 5000

	// A ceiling on one table in one sweep, so a large backlog is worked off
	// over hours instead of in a single long lock.
	sweepMaxChunks = 200

	// Reclaiming is paced the same way, about 4MB per step.
	vacuumPages    = 1000
	vacuumMaxSteps = 200
)

// sweptTables are the two that grow with traffic. properties is operator sized
// and never swept.
var sweptTables = []string{"events", "bot_events"}

type Sweeper struct {
	db *sql.DB
}

func NewSweeper(db *sql.DB) *Sweeper { return &Sweeper{db: db} }

// Run sweeps on a ticker until ctx is cancelled, and once at startup, since a
// process restarted more often than the interval would otherwise never sweep.
func (s *Sweeper) Run(ctx context.Context) {
	s.sweep(ctx)

	t := time.NewTicker(sweepInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.sweep(ctx)
		}
	}
}

func (s *Sweeper) sweep(ctx context.Context) {
	cutoff := time.Now().Add(-eventRetention).UnixMilli()
	start := time.Now()

	var total int64
	for _, table := range sweptTables {
		n, err := s.deleteOlderThan(ctx, table, cutoff)
		total += n
		if err != nil {
			slog.Error("retention sweep failed",
				slog.String("component", "retention"),
				slog.String("table", table),
				slog.Any("err", err))
			return
		}
	}
	if total == 0 {
		return
	}

	slog.Info("retention sweep",
		slog.String("component", "retention"),
		slog.Int64("deleted", total),
		slog.String("cutoff", time.UnixMilli(cutoff).UTC().Format(time.RFC3339)),
		slog.String("reclaim", s.reclaim(ctx)),
		slog.Float64("ms", float64(time.Since(start).Microseconds())/1000))
}

// deleteOlderThan removes aged rows from one table in bounded chunks. The
// subselect on rowid is there because LIMIT on DELETE is a SQLite compile-time
// option not enabled in every build.
func (s *Sweeper) deleteOlderThan(ctx context.Context, table string, cutoff int64) (int64, error) {
	// table is one of sweptTables above and never comes from a request, which
	// is the only reason it can be formatted into the statement at all.
	stmt := fmt.Sprintf(`
		DELETE FROM %s
		WHERE rowid IN (SELECT rowid FROM %s WHERE created_at < ? ORDER BY rowid LIMIT ?)`,
		table, table)

	var total int64
	for i := 0; i < sweepMaxChunks; i++ {
		res, err := s.db.ExecContext(ctx, stmt, cutoff, sweepChunk)
		if err != nil {
			return total, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return total, err
		}
		total += n
		if n < sweepChunk {
			return total, nil
		}

		// Yield, or a tight loop starves the collector for the whole sweep.
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return total, nil
}

// reclaim hands freed pages back to the filesystem, and says what it did.
//
// Only a database created with auto_vacuum=INCREMENTAL can do this, and the
// pragma is ignored on a file that already exists, so a database from before
// that setting reports "not enabled" here. Deleting still bounds the row count
// and stops the file growing, the freed pages are just reused rather than
// returned. A one-off `VACUUM` converts an existing file if the space is wanted
// back sooner.
func (s *Sweeper) reclaim(ctx context.Context) string {
	var mode int
	if err := s.db.QueryRowContext(ctx, "PRAGMA auto_vacuum").Scan(&mode); err != nil {
		return fmt.Sprintf("auto_vacuum check failed: %v", err)
	}
	if mode != 2 {
		return "not enabled"
	}

	for i := 0; i < vacuumMaxSteps; i++ {
		// The page count is formatted in because SQLite takes no PRAGMA
		// parameter. A bare incremental_vacuum would free the whole freelist
		// under the write lock, hence the pacing.
		if _, err := s.db.ExecContext(ctx,
			fmt.Sprintf("PRAGMA incremental_vacuum(%d)", vacuumPages)); err != nil {
			return fmt.Sprintf("incremental_vacuum failed: %v", err)
		}

		var remaining int64
		if err := s.db.QueryRowContext(ctx, "PRAGMA freelist_count").Scan(&remaining); err != nil {
			return fmt.Sprintf("freelist_count failed: %v", err)
		}
		if remaining == 0 {
			break
		}

		select {
		case <-ctx.Done():
			return "cancelled"
		case <-time.After(50 * time.Millisecond):
		}
	}
	return "reclaimed"
}
