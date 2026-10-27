package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// Raw records age out and hourly rollups don't, so a graph can cover a year
// while the database stays a fixed size.

const (
	// Safe to change, the next sweep enforces whatever it says.
	rawRetention = 30 * 24 * time.Hour

	// Hourly instead of daily, so a deletion is always small and a machine
	// asleep most of the day still gets one in.
	sweepInterval = time.Hour

	// Rows per DELETE. SQLite holds a database-wide write lock for the whole
	// statement, so an unbounded delete would stall every ingest behind it.
	sweepChunk = 5000

	// A ceiling on one sweep, so a large backlog is worked off over hours.
	sweepMaxChunks = 200

	// Reclaiming is paced the same way, 1000 pages is about 4MB per step.
	vacuumPages    = 1000
	vacuumMaxSteps = 200

	// The pause between steps. SQLite's busy handler polls every 100ms once
	// it has waited a while, so a shorter gap can fall between two polls and
	// the writer waits out its whole busy_timeout behind a long sweep.
	sweepYield = 250 * time.Millisecond
)

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
			if _, err := s.db.ExecContext(ctx, "PRAGMA optimize"); err != nil {
				slog.Warn("pragma optimize failed", slog.String("component", "retention"), slog.Any("err", err))
			}
		}
	}
}

func (s *Sweeper) sweep(ctx context.Context) {
	cutoff := time.Now().Add(-rawRetention).UnixMilli()
	start := time.Now()

	deleted, err := s.deleteOlderThan(ctx, cutoff)
	if err != nil {
		slog.Error("retention sweep failed",
			slog.String("component", "retention"),
			slog.Any("err", err))
		return
	}
	if deleted == 0 {
		return
	}

	// Only auto_vacuum hands freed pages back, so this keeps the file bounded.
	// It's paced since a bare incremental_vacuum frees the whole freelist under
	// the write lock, and the count is formatted in since a PRAGMA takes no parameter.
	reclaimed := "reclaimed"
	for i := 0; i < vacuumMaxSteps; i++ {
		if _, err := s.db.ExecContext(ctx,
			fmt.Sprintf("PRAGMA incremental_vacuum(%d)", vacuumPages)); err != nil {
			reclaimed = fmt.Sprintf("incremental_vacuum failed: %v", err)
			break
		}

		var remaining int64
		if err := s.db.QueryRowContext(ctx, "PRAGMA freelist_count").Scan(&remaining); err != nil {
			reclaimed = fmt.Sprintf("freelist_count failed: %v", err)
			break
		}
		if remaining == 0 {
			break
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(sweepYield):
		}
	}

	slog.Info("retention sweep",
		slog.String("component", "retention"),
		slog.Int64("deleted", deleted),
		slog.String("cutoff", time.UnixMilli(cutoff).UTC().Format(time.RFC3339)),
		slog.String("reclaim", reclaimed),
		slog.Float64("ms", float64(time.Since(start).Microseconds())/1000))
}

// deleteOlderThan removes aged rows in bounded chunks. The subselect on id is
// there because LIMIT on DELETE is a SQLite compile-time option not enabled in
// every build.
func (s *Sweeper) deleteOlderThan(ctx context.Context, cutoff int64) (int64, error) {
	var total int64
	for i := 0; i < sweepMaxChunks; i++ {
		res, err := s.db.ExecContext(ctx, `
			DELETE FROM records
			WHERE id IN (SELECT id FROM records WHERE ts < ? ORDER BY id LIMIT ?)`,
			cutoff, sweepChunk)
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

		// Yield, or a tight loop starves the writer for the whole sweep.
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		case <-time.After(sweepYield):
		}
	}
	return total, nil
}
