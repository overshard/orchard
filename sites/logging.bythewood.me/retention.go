package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// Retention: the half of this site that keeps it from becoming the problem it
// was built to watch.
//
// Raw records age out, hourly rollups do not. That is the whole policy, and it
// is why the graphs can cover a year while the database stays a fixed size: a
// year of rollups for five sources is on the order of a hundred thousand rows,
// and a year of raw request logs is tens of millions.

const (
	// Thirty days of raw lines. Long enough that "what happened last month"
	// is answerable and short enough that the file stays small: at the
	// measured shape of this traffic, a few tens of thousands of records a
	// day at roughly 250 bytes a row, thirty days is a few hundred megabytes
	// including indexes.
	//
	// This is the number to change if it turns out to be wrong, and changing
	// it costs nothing: the next sweep enforces whatever it says.
	rawRetention = 30 * 24 * time.Hour

	// How often the sweeper wakes. Hourly rather than daily so a deletion is
	// always small, and so a machine that is asleep most of the day still
	// gets one in.
	sweepInterval = time.Hour

	// Rows per DELETE. SQLite takes a database-wide write lock for the whole
	// statement, so one unbounded delete of a month's backlog would stall
	// every ingest behind it for as long as it took. Chunks give the writer
	// its turn between them.
	sweepChunk = 5000

	// A ceiling on one sweep, so a very large backlog is worked off over
	// several hours instead of monopolizing the lock for one long one.
	sweepMaxChunks = 200

	// Reclaiming is paced the same way, and for the same reason. 1000 pages is
	// about 4MB per step at the default page size.
	vacuumPages    = 1000
	vacuumMaxSteps = 200
)

// Sweeper runs the retention pass.
type Sweeper struct {
	db *sql.DB
}

func NewSweeper(db *sql.DB) *Sweeper { return &Sweeper{db: db} }

// Run sweeps on a ticker until the context is cancelled. It sweeps once at
// startup too: a process that is restarted more often than the interval would
// otherwise never sweep at all, which is exactly the shape of a site that is
// deployed several times a day.
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

	// Reclaiming is a separate step, and without it the row count is bounded
	// while the file on disk is not: SQLite frees pages onto a freelist and
	// reuses them, but only auto_vacuum hands them back to the filesystem.
	// INCREMENTAL is set in the DSN, which means it has to have been set when
	// the file was created; this is the call that acts on it.
	//
	// In pages, in a paced loop, for the same reason the deletes above are
	// chunked. A bare `PRAGMA incremental_vacuum` frees the entire freelist
	// under one database-wide write lock: measured at 1.3 seconds to reclaim
	// 160MB, five times longer than any delete chunk, and it scales with the
	// backlog. Freeing a gigabyte would exceed busy_timeout(5000) and make the
	// writer discard a batch, which is a strange way for a retention pass to
	// lose today's logs.
	//
	// The page count is formatted in rather than bound: SQLite does not accept
	// a parameter in a PRAGMA. It is a constant in this file, never input.
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
		case <-time.After(50 * time.Millisecond):
		}
	}

	slog.Info("retention sweep",
		slog.String("component", "retention"),
		slog.Int64("deleted", deleted),
		slog.String("cutoff", time.UnixMilli(cutoff).UTC().Format(time.RFC3339)),
		slog.String("reclaim", reclaimed),
		slog.Float64("ms", float64(time.Since(start).Microseconds())/1000))
}

// deleteOlderThan removes aged rows in bounded chunks, stopping when a chunk
// comes back short.
//
// The subselect on id rather than a plain `DELETE ... WHERE ts < ?` is what
// makes the chunking honest: SQLite's LIMIT on DELETE is a compile-time option
// and is not enabled in every build, so relying on it would work here and fail
// on someone else's.
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

		// Yield between chunks. The writer holds the only other write path
		// and a tight loop here would starve it for the length of the sweep.
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return total, nil
}
