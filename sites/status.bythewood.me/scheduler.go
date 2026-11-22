package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"
)

const (
	cycleInterval = 30 * time.Second

	// Separate pools, so a long crawl can't starve a three-minute ping, and one
	// audit at a time since each fetches a whole page's worth of files.
	fastWorkers  = 2
	slowWorkers  = 2
	auditWorkers = 1

	checkInterval = 3 * time.Minute
	auditInterval = 24 * time.Hour
	crawlInterval = 7 * 24 * time.Hour

	// Watchdog cutoffs, both past the work's own deadline, so a slow but living
	// job records its own result instead of being declared dead underneath it.
	crawlWedgeAfter = 15 * time.Minute
	auditWedgeAfter = 5 * time.Minute

	cleanupInterval = 24 * time.Hour
	checkRetention  = 3 * 24 * time.Hour
	rollupInterval  = 10 * time.Minute
)

// Scheduler owns the background work.
type Scheduler struct {
	db       *sql.DB
	notifier *Notifier

	// Buffered channels as semaphores, a full one blocks the goroutine that wants
	// to work until a slot frees up.
	fast  chan struct{}
	slow  chan struct{}
	audit chan struct{}

	// The loop and every job it started, so shutdown can wait for their writes.
	jobs sync.WaitGroup
}

func NewScheduler(db *sql.DB, notifier *Notifier) *Scheduler {
	return &Scheduler{
		db:       db,
		notifier: notifier,
		fast:     make(chan struct{}, fastWorkers),
		slow:     make(chan struct{}, slowWorkers),
		audit:    make(chan struct{}, auditWorkers),
	}
}

// ResetOnBoot clears rows left queued or running by a previous process, whose
// goroutines are gone, without it a property interrupted by a deploy sticks.
func (s *Scheduler) ResetOnBoot(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx,
		"UPDATE properties SET crawl_state = 'idle' WHERE crawl_state IN ('queued', 'running')"); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		"UPDATE properties SET audit_state = 'idle' WHERE audit_state IN ('queued', 'running')")
	return err
}

// Start runs the loop in the background until ctx is cancelled.
func (s *Scheduler) Start(ctx context.Context) {
	s.jobs.Go(func() { s.run(ctx) })
}

// Wait returns once the loop and every job it started have stopped.
func (s *Scheduler) Wait() { s.jobs.Wait() }

func (s *Scheduler) run(ctx context.Context) {
	ticker := time.NewTicker(cycleInterval)
	defer ticker.Stop()

	var lastCleanup, lastRollup time.Time

	if err := backfillIncidents(ctx, s.db); err != nil {
		slog.Error("backfill incidents", slog.Any("err", err), slog.String("component", "scheduler"))
	}

	for {
		s.cycle(ctx, &lastCleanup)
		if time.Since(lastRollup) >= rollupInterval {
			if err := rollupDays(ctx, s.db); err != nil {
				slog.Error("rollup days", slog.Any("err", err), slog.String("component", "scheduler"))
			}
			lastRollup = time.Now()
		}

		select {
		case <-ctx.Done():
			slog.Info("stopping", slog.String("component", "scheduler"))
			return
		case <-ticker.C:
		}
	}
}

func (s *Scheduler) cycle(ctx context.Context, lastCleanup *time.Time) {
	// Each step logs and continues instead of returning, so a failure
	// enqueuing Audit doesn't also skip the uptime checks.
	if err := s.enqueueChecks(ctx); err != nil {
		slog.Info("enqueue checks", slog.Any("err", err), slog.String("component", "scheduler"))
	}
	if err := s.enqueueAudit(ctx); err != nil {
		slog.Info("enqueue audit", slog.Any("err", err), slog.String("component", "scheduler"))
	}
	if err := s.enqueueCrawls(ctx); err != nil {
		slog.Info("enqueue crawls", slog.Any("err", err), slog.String("component", "scheduler"))
	}
	if err := s.resetWedged(ctx); err != nil {
		slog.Info("reset wedged", slog.Any("err", err), slog.String("component", "scheduler"))
	}
	if err := s.maybeCleanup(ctx, lastCleanup); err != nil {
		slog.Info("cleanup", slog.Any("err", err), slog.String("component", "scheduler"))
	}
}

// due returns the properties matching a condition, all columns loaded.
func (s *Scheduler) due(ctx context.Context, where string, args ...any) ([]*Property, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+propertyColumns+" FROM properties WHERE "+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Property
	for rows.Next() {
		p, err := scanProperty(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// enqueueChecks starts the HTTP probes that are due. The next run time is
// written before the work starts, so the next tick does not probe it again.
func (s *Scheduler) enqueueChecks(ctx context.Context) error {
	now := nowMS()
	props, err := s.due(ctx,
		"last_run_at IS NULL OR next_run_at IS NULL OR next_run_at <= ?", now)
	if err != nil {
		return err
	}

	for _, p := range props {
		if _, err := s.db.ExecContext(ctx,
			"UPDATE properties SET next_run_at = ?, last_run_at = ?, updated_at = ? WHERE id = ?",
			next3MinBoundary(), now, now, p.ID[:]); err != nil {
			slog.Info("claim check", slog.Any("url", p.URL), slog.Any("err", err), slog.String("component", "scheduler"))
			continue
		}

		s.jobs.Go(func() {
			select {
			case s.fast <- struct{}{}:
				defer func() { <-s.fast }()
			case <-ctx.Done():
				return
			}
			if err := processCheck(ctx, s.db, s.notifier, p); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("check failed", slog.Any("url", p.URL), slog.Any("err", err), slog.String("component", "scheduler"))
			}
		})
	}
	return nil
}

func (s *Scheduler) enqueueAudit(ctx context.Context) error {
	now := nowMS()
	props, err := s.due(ctx,
		`(last_audit_run_at IS NULL OR next_audit_run_at IS NULL
		  OR next_audit_run_at <= ?)
		 AND audit_state NOT IN ('queued', 'running')`, now)
	if err != nil {
		return err
	}

	for _, p := range props {
		next := now + auditInterval.Milliseconds()
		if _, err := s.db.ExecContext(ctx,
			`UPDATE properties SET next_audit_run_at = ?, last_audit_run_at = ?,
			  audit_state = 'queued', updated_at = ? WHERE id = ?`,
			next, now, now, p.ID[:]); err != nil {
			slog.Info("claim audit", slog.Any("url", p.URL), slog.Any("err", err), slog.String("component", "scheduler"))
			continue
		}

		s.jobs.Go(func() {
			select {
			case s.audit <- struct{}{}:
				defer func() { <-s.audit }()
			case <-ctx.Done():
				return
			}
			s.runAuditFor(ctx, p)
		})
	}
	return nil
}

func (s *Scheduler) runAuditFor(ctx context.Context, p *Property) {
	slog.Info("audit", slog.Any("url", p.URL), slog.String("component", "scheduler"))
	started := time.Now()

	now := nowMS()
	if _, err := s.db.ExecContext(ctx,
		`UPDATE properties SET audit_state = 'running', audit_started_at = ?,
		  updated_at = ? WHERE id = ?`, now, now, p.ID[:]); err != nil {
		slog.Info("mark audit running", slog.Any("url", p.URL), slog.Any("err", err), slog.String("component", "scheduler"))
	}

	fail := func(err error) {
		slog.Error("audit failed", slog.Any("url", p.URL), slog.Any("err", err), slog.String("component", "scheduler"))
		if _, dbErr := s.db.ExecContext(ctx,
			`UPDATE properties SET audit_state = 'idle', last_audit_error = ?,
			  last_audit_duration_ms = ?, updated_at = ? WHERE id = ?`,
			err.Error(), elapsedMS(started), nowMS(), p.ID[:]); dbErr != nil {
			slog.Error("record audit error", slog.Any("url", p.URL), slog.Any("err", dbErr), slog.String("component", "scheduler"))
		}
	}

	scores, details, err := runAudit(ctx, newCrawlClient(), p.URL)
	if err != nil {
		fail(err)
		return
	}
	scoresJSON, err := json.Marshal(scores)
	if err != nil {
		fail(err)
		return
	}
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		fail(err)
		return
	}

	if _, err := s.db.ExecContext(ctx,
		`UPDATE properties SET audit_scores = ?, audit_details = ?,
		  last_audit_success_at = ?, last_audit_error = NULL,
		  last_audit_duration_ms = ?, audit_state = 'idle', updated_at = ?
		 WHERE id = ?`,
		string(scoresJSON), string(detailsJSON), nowMS(), elapsedMS(started), nowMS(), p.ID[:]); err != nil {
		slog.Info("store audit", slog.Any("url", p.URL), slog.Any("err", err), slog.String("component", "scheduler"))
	}
}

func (s *Scheduler) enqueueCrawls(ctx context.Context) error {
	now := nowMS()
	props, err := s.due(ctx,
		`(last_run_at_crawler IS NULL OR next_run_at_crawler IS NULL
		  OR next_run_at_crawler <= ?)
		 AND crawl_state NOT IN ('queued', 'running')`, now)
	if err != nil {
		return err
	}

	for _, p := range props {
		next := now + crawlInterval.Milliseconds()
		if _, err := s.db.ExecContext(ctx,
			`UPDATE properties SET next_run_at_crawler = ?, last_run_at_crawler = ?,
			  crawl_state = 'queued', updated_at = ? WHERE id = ?`,
			next, now, now, p.ID[:]); err != nil {
			slog.Info("claim crawl", slog.Any("url", p.URL), slog.Any("err", err), slog.String("component", "scheduler"))
			continue
		}

		s.jobs.Go(func() {
			select {
			case s.slow <- struct{}{}:
				defer func() { <-s.slow }()
			case <-ctx.Done():
				return
			}
			s.runCrawlFor(ctx, p)
		})
	}
	return nil
}

func (s *Scheduler) runCrawlFor(ctx context.Context, p *Property) {
	slog.Info("crawling", slog.Any("url", p.URL), slog.String("component", "scheduler"))
	started := time.Now()

	now := nowMS()
	if _, err := s.db.ExecContext(ctx,
		`UPDATE properties SET crawl_state = 'running', crawl_started_at = ?,
		  last_crawl_pages_count = 0, updated_at = ? WHERE id = ?`,
		now, now, p.ID[:]); err != nil {
		slog.Info("mark crawl running", slog.Any("url", p.URL), slog.Any("err", err), slog.String("component", "scheduler"))
	}

	// Synchronous instead of a goroutine per update, which would leave detached
	// writers contending for SQLite's single write lock on one column.
	progress := func(pages int) {
		if _, err := s.db.ExecContext(ctx,
			"UPDATE properties SET last_crawl_pages_count = ? WHERE id = ?",
			int64(pages), p.ID[:]); err != nil {
			slog.Info("crawl progress", slog.Any("url", p.URL), slog.Any("err", err), slog.String("component", "scheduler"))
		}
	}

	insights, err := RunSEOSpider(ctx, p.URL, progress)
	if err != nil {
		slog.Error("crawl failed", slog.Any("url", p.URL), slog.Any("err", err), slog.String("component", "scheduler"))
		if _, dbErr := s.db.ExecContext(ctx,
			`UPDATE properties SET crawl_state = 'idle', last_crawl_error = ?,
			  last_crawl_duration_ms = ?, updated_at = ? WHERE id = ?`,
			err.Error(), elapsedMS(started), nowMS(), p.ID[:]); dbErr != nil {
			slog.Error("record crawl error", slog.Any("url", p.URL), slog.Any("err", dbErr), slog.String("component", "scheduler"))
		}
		return
	}

	encoded, err := json.Marshal(insights)
	if err != nil {
		slog.Info("encode insights", slog.Any("url", p.URL), slog.Any("err", err), slog.String("component", "scheduler"))
		encoded = []byte("[]")
	}

	if _, err := s.db.ExecContext(ctx,
		`UPDATE properties SET crawler_insights = ?, crawl_state = 'idle',
		  last_crawl_success_at = ?, last_crawl_error = NULL,
		  last_crawl_duration_ms = ?, updated_at = ? WHERE id = ?`,
		string(encoded), nowMS(), elapsedMS(started), nowMS(), p.ID[:]); err != nil {
		slog.Info("store insights", slog.Any("url", p.URL), slog.Any("err", err), slog.String("component", "scheduler"))
	}
}

// resetWedged is the watchdog, the thresholds are the cutoff constants above.
func (s *Scheduler) resetWedged(ctx context.Context) error {
	now := nowMS()

	if _, err := s.db.ExecContext(ctx,
		`UPDATE properties SET crawl_state = 'idle',
		   last_crawl_error = 'Crawl timed out or was interrupted'
		 WHERE crawl_state = 'running' AND crawl_started_at IS NOT NULL
		   AND crawl_started_at < ?`,
		now-crawlWedgeAfter.Milliseconds()); err != nil {
		return err
	}

	_, err := s.db.ExecContext(ctx,
		`UPDATE properties SET audit_state = 'idle',
		   last_audit_error = 'Audit run timed out or was interrupted'
		 WHERE audit_state = 'running' AND audit_started_at IS NOT NULL
		   AND audit_started_at < ?`,
		now-auditWedgeAfter.Milliseconds())
	return err
}

// maybeCleanup deletes checks past the retention window, once a day. The timer
// is in memory, so it deletes everything past the cutoff and catches up.
func (s *Scheduler) maybeCleanup(ctx context.Context, last *time.Time) error {
	if !last.IsZero() && time.Since(*last) < cleanupInterval {
		return nil
	}

	result, err := s.db.ExecContext(ctx,
		"DELETE FROM checks WHERE created_at < ?", nowMS()-checkRetention.Milliseconds())
	if err != nil {
		return err
	}
	*last = time.Now()

	if n, err := result.RowsAffected(); err == nil && n > 0 {
		slog.Info("deleted old checks", slog.Any("count", n), slog.Any("older_than", checkRetention), slog.String("component", "scheduler"))
	}
	return nil
}

// elapsedMS is in whole milliseconds, the unit every *_duration_ms column uses.
func elapsedMS(started time.Time) int64 {
	return time.Since(started).Milliseconds()
}
