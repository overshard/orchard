package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// The scheduler: one goroutine, waking every thirty seconds, deciding what is
// due and handing it to a worker pool.
//
// No cron, no queue and no second process. The binary that serves the dashboard
// is the binary that does the monitoring, which is what makes this app one file
// to deploy.

const (
	cycleInterval = 30 * time.Second

	// Two pools, so a nine-minute crawl cannot starve a three-minute ping.
	// With one shared pool, two sites crawling at once would stop every uptime
	// check on every property, and the dashboard would show a flat green line
	// while nothing was being measured.
	fastWorkers = 2
	slowWorkers = 2

	// checkInterval is read by next3MinBoundary, which aligns to it rather
	// than adding it to now, so every property is probed on the same cadence
	// regardless of when it was created.
	checkInterval      = 3 * time.Minute
	lighthouseInterval = 24 * time.Hour
	crawlInterval      = 7 * 24 * time.Hour

	// Watchdog cutoffs. Anything still "running" past these was interrupted by
	// the process being killed, the machine sleeping, or a subprocess wedging.
	// Without the reset the row stays running forever and that property is
	// never audited again.
	//
	// Both are past the work's own deadline (nine minutes for a crawl, three
	// for Lighthouse), so a slow but living job finishes and records its own
	// result rather than being declared dead underneath itself.
	crawlWedgeAfter      = 15 * time.Minute
	lighthouseWedgeAfter = 5 * time.Minute

	cleanupInterval = 24 * time.Hour
	// Three days of checks, at one every three minutes, is 1,440 rows per
	// property. The dashboard charts the most recent 31 and the uptime
	// percentages read all of them, so this is the window the numbers
	// describe.
	checkRetention = 3 * 24 * time.Hour
)

// Scheduler owns the background work.
type Scheduler struct {
	db       *sql.DB
	notifier *Notifier
	root     string

	// Buffered channels as semaphores. A send takes a slot and a receive
	// releases it, so a full channel blocks the goroutine that wanted to work
	// rather than spawning an unbounded number of them.
	fast chan struct{}
	slow chan struct{}
}

func NewScheduler(db *sql.DB, notifier *Notifier, root string) *Scheduler {
	return &Scheduler{
		db:       db,
		notifier: notifier,
		root:     root,
		fast:     make(chan struct{}, fastWorkers),
		slow:     make(chan struct{}, slowWorkers),
	}
}

// ResetOnBoot clears rows left queued or running by a previous process, whose
// goroutines are gone. Without this, every property interrupted by a deploy
// would be stuck permanently.
func (s *Scheduler) ResetOnBoot(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx,
		"UPDATE properties SET crawl_state = 'idle' WHERE crawl_state IN ('queued', 'running')"); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		"UPDATE properties SET lighthouse_state = 'idle' WHERE lighthouse_state IN ('queued', 'running')")
	return err
}

// Run loops until the context is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(cycleInterval)
	defer ticker.Stop()

	var lastCleanup time.Time

	for {
		s.cycle(ctx, &lastCleanup)

		select {
		case <-ctx.Done():
			slog.Info("stopping", slog.String("component", "scheduler"))
			return
		case <-ticker.C:
		}
	}
}

func (s *Scheduler) cycle(ctx context.Context, lastCleanup *time.Time) {
	// Each step logs and continues rather than returning, so a failure
	// enqueuing Lighthouse does not also skip the uptime checks.
	if err := s.enqueueChecks(ctx); err != nil {
		slog.Info(fmt.Sprintf("enqueue checks: %v", err), slog.String("component", "scheduler"))
	}
	if err := s.enqueueLighthouse(ctx); err != nil {
		slog.Info(fmt.Sprintf("enqueue lighthouse: %v", err), slog.String("component", "scheduler"))
	}
	if err := s.enqueueCrawls(ctx); err != nil {
		slog.Info(fmt.Sprintf("enqueue crawls: %v", err), slog.String("component", "scheduler"))
	}
	if err := s.resetWedged(ctx); err != nil {
		slog.Info(fmt.Sprintf("reset wedged: %v", err), slog.String("component", "scheduler"))
	}
	if err := s.maybeCleanup(ctx, lastCleanup); err != nil {
		slog.Info(fmt.Sprintf("cleanup: %v", err), slog.String("component", "scheduler"))
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
// written *before* the work starts, which stops the next tick from probing the
// same property thirty seconds later while the first probe is still in
// flight.
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
			slog.Info(fmt.Sprintf("claim check for %s: %v", p.URL, err), slog.String("component", "scheduler"))
			continue
		}

		go func(p *Property) {
			select {
			case s.fast <- struct{}{}:
				defer func() { <-s.fast }()
			case <-ctx.Done():
				return
			}
			slog.Info(fmt.Sprintf("checking %s", p.URL), slog.String("component", "scheduler"))
			if err := processCheck(ctx, s.db, s.notifier, p); err != nil {
				slog.Error(fmt.Sprintf("check failed for %s: %v", p.URL, err), slog.String("component", "scheduler"))
			}
		}(p)
	}
	return nil
}

func (s *Scheduler) enqueueLighthouse(ctx context.Context) error {
	now := nowMS()
	props, err := s.due(ctx,
		`(last_lighthouse_run_at IS NULL OR next_lighthouse_run_at IS NULL
		  OR next_lighthouse_run_at <= ?)
		 AND lighthouse_state NOT IN ('queued', 'running')`, now)
	if err != nil {
		return err
	}

	for _, p := range props {
		next := now + lighthouseInterval.Milliseconds()
		if _, err := s.db.ExecContext(ctx,
			`UPDATE properties SET next_lighthouse_run_at = ?, last_lighthouse_run_at = ?,
			  lighthouse_state = 'queued', updated_at = ? WHERE id = ?`,
			next, now, now, p.ID[:]); err != nil {
			slog.Info(fmt.Sprintf("claim lighthouse for %s: %v", p.URL, err), slog.String("component", "scheduler"))
			continue
		}

		go func(p *Property) {
			select {
			case s.slow <- struct{}{}:
				defer func() { <-s.slow }()
			case <-ctx.Done():
				return
			}
			s.runLighthouseFor(ctx, p)
		}(p)
	}
	return nil
}

func (s *Scheduler) runLighthouseFor(ctx context.Context, p *Property) {
	slog.Info(fmt.Sprintf("lighthouse %s", p.URL), slog.String("component", "scheduler"))
	started := time.Now()

	now := nowMS()
	if _, err := s.db.ExecContext(ctx,
		`UPDATE properties SET lighthouse_state = 'running', lighthouse_started_at = ?,
		  updated_at = ? WHERE id = ?`, now, now, p.ID[:]); err != nil {
		slog.Info(fmt.Sprintf("mark lighthouse running for %s: %v", p.URL, err), slog.String("component", "scheduler"))
	}

	fail := func(err error) {
		slog.Error(fmt.Sprintf("lighthouse failed for %s: %v", p.URL, err), slog.String("component", "scheduler"))
		if _, dbErr := s.db.ExecContext(ctx,
			`UPDATE properties SET lighthouse_state = 'idle', last_lighthouse_error = ?,
			  last_lighthouse_duration_ms = ?, updated_at = ? WHERE id = ?`,
			err.Error(), elapsedMS(started), nowMS(), p.ID[:]); dbErr != nil {
			slog.Error(fmt.Sprintf("record lighthouse error for %s: %v", p.URL, dbErr), slog.String("component", "scheduler"))
		}
	}

	report, err := runLighthouse(ctx, s.root, p.URL)
	if err != nil {
		fail(err)
		return
	}
	scores, err := parseScores(report)
	if err != nil {
		fail(err)
		return
	}

	scoresJSON, err := json.Marshal(scores)
	if err != nil {
		fail(err)
		return
	}
	// Details are best effort. The headline scores are what matters, and a
	// Lighthouse release that reshapes auditRefs should not throw away a good
	// audit. The template tests for "null".
	detailsJSON := []byte("null")
	if details := parseDetails(report); details != nil {
		if encoded, err := json.Marshal(details); err == nil {
			detailsJSON = encoded
		}
	}

	if _, err := s.db.ExecContext(ctx,
		`UPDATE properties SET lighthouse_scores = ?, lighthouse_details = ?,
		  last_lighthouse_success_at = ?, last_lighthouse_error = NULL,
		  last_lighthouse_duration_ms = ?, lighthouse_state = 'idle', updated_at = ?
		 WHERE id = ?`,
		string(scoresJSON), string(detailsJSON), nowMS(), elapsedMS(started), nowMS(), p.ID[:]); err != nil {
		slog.Info(fmt.Sprintf("store lighthouse for %s: %v", p.URL, err), slog.String("component", "scheduler"))
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
			slog.Info(fmt.Sprintf("claim crawl for %s: %v", p.URL, err), slog.String("component", "scheduler"))
			continue
		}

		go func(p *Property) {
			select {
			case s.slow <- struct{}{}:
				defer func() { <-s.slow }()
			case <-ctx.Done():
				return
			}
			s.runCrawlFor(ctx, p)
		}(p)
	}
	return nil
}

func (s *Scheduler) runCrawlFor(ctx context.Context, p *Property) {
	slog.Info(fmt.Sprintf("crawling %s", p.URL), slog.String("component", "scheduler"))
	started := time.Now()

	now := nowMS()
	if _, err := s.db.ExecContext(ctx,
		`UPDATE properties SET crawl_state = 'running', crawl_started_at = ?,
		  last_crawl_pages_count = 0, updated_at = ? WHERE id = ?`,
		now, now, p.ID[:]); err != nil {
		slog.Info(fmt.Sprintf("mark crawl running for %s: %v", p.URL, err), slog.String("component", "scheduler"))
	}

	// Progress writes drive the dashboard's progress bar during a crawl that
	// can legitimately run nine minutes. Synchronous rather than one goroutine
	// per update, which would put up to 125 detached writers per crawl in
	// contention for SQLite's single write lock on the same column. Called once
	// per batch of four pages, so at most 125 quick writes in series.
	progress := func(pages int) {
		if _, err := s.db.ExecContext(ctx,
			"UPDATE properties SET last_crawl_pages_count = ? WHERE id = ?",
			int64(pages), p.ID[:]); err != nil {
			slog.Info(fmt.Sprintf("crawl progress for %s: %v", p.URL, err), slog.String("component", "scheduler"))
		}
	}

	insights, err := RunSEOSpider(ctx, p.URL, progress)
	if err != nil {
		slog.Error(fmt.Sprintf("crawl failed for %s: %v", p.URL, err), slog.String("component", "scheduler"))
		if _, dbErr := s.db.ExecContext(ctx,
			`UPDATE properties SET crawl_state = 'idle', last_crawl_error = ?,
			  last_crawl_duration_ms = ?, updated_at = ? WHERE id = ?`,
			err.Error(), elapsedMS(started), nowMS(), p.ID[:]); dbErr != nil {
			slog.Error(fmt.Sprintf("record crawl error for %s: %v", p.URL, dbErr), slog.String("component", "scheduler"))
		}
		return
	}

	encoded, err := json.Marshal(insights)
	if err != nil {
		slog.Info(fmt.Sprintf("encode insights for %s: %v", p.URL, err), slog.String("component", "scheduler"))
		encoded = []byte("[]")
	}

	if _, err := s.db.ExecContext(ctx,
		`UPDATE properties SET crawler_insights = ?, crawl_state = 'idle',
		  last_crawl_success_at = ?, last_crawl_error = NULL,
		  last_crawl_duration_ms = ?, updated_at = ? WHERE id = ?`,
		string(encoded), nowMS(), elapsedMS(started), nowMS(), p.ID[:]); err != nil {
		slog.Info(fmt.Sprintf("store insights for %s: %v", p.URL, err), slog.String("component", "scheduler"))
	}
}

// resetWedged is the watchdog. See the cutoff constants above for why the
// thresholds are what they are.
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
		`UPDATE properties SET lighthouse_state = 'idle',
		   last_lighthouse_error = 'Lighthouse run timed out or was interrupted'
		 WHERE lighthouse_state = 'running' AND lighthouse_started_at IS NOT NULL
		   AND lighthouse_started_at < ?`,
		now-lighthouseWedgeAfter.Milliseconds())
	return err
}

// maybeCleanup deletes checks past the retention window, once a day.
//
// The timer is in memory rather than in the meta table, so a process restarting
// more than once a day never runs it. That is why this deletes everything past
// the cutoff rather than one day's worth: whenever it does run, it catches
// up.
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
		slog.Info(fmt.Sprintf("deleted %d checks older than %s", n, checkRetention), slog.String("component", "scheduler"))
	}
	return nil
}

// elapsedMS is how long ago something started, in whole milliseconds, which is
// the unit every *_duration_ms column stores.
func elapsedMS(started time.Time) int64 {
	return time.Since(started).Milliseconds()
}
