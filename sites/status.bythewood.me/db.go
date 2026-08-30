package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// schema is IF NOT EXISTS throughout: pointing this at an existing database has
// to be a no-op, since the property UUIDs in it are the addresses of live public
// status pages.
const schema = `
CREATE TABLE IF NOT EXISTS properties (
    id                          BLOB PRIMARY KEY,
    url                         TEXT NOT NULL,
    is_public                   INTEGER NOT NULL DEFAULT 0,
    is_protected                INTEGER NOT NULL DEFAULT 0,

    last_run_at                 INTEGER,
    next_run_at                 INTEGER,

    last_run_at_crawler         INTEGER,
    next_run_at_crawler         INTEGER,
    crawler_insights            TEXT,
    crawl_state                 TEXT NOT NULL DEFAULT 'idle',
    crawl_started_at            INTEGER,
    last_crawl_success_at       INTEGER,
    last_crawl_error            TEXT,
    last_crawl_duration_ms      INTEGER,
    last_crawl_pages_count      INTEGER,

    lighthouse_scores           TEXT,
    lighthouse_details          TEXT,
    last_lighthouse_run_at      INTEGER,
    last_lighthouse_success_at  INTEGER,
    last_lighthouse_error       TEXT,
    last_lighthouse_duration_ms INTEGER,
    next_lighthouse_run_at      INTEGER,
    lighthouse_state            TEXT NOT NULL DEFAULT 'idle',
    lighthouse_started_at       INTEGER,

    alert_state                 TEXT NOT NULL DEFAULT 'up',
    last_alert_sent             INTEGER,

    created_at                  INTEGER NOT NULL,
    updated_at                  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS properties_url ON properties(url);

CREATE TABLE IF NOT EXISTS checks (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    property_id  BLOB NOT NULL REFERENCES properties(id) ON DELETE CASCADE,
    status_code  INTEGER NOT NULL,
    response_ms  INTEGER NOT NULL DEFAULT 0,
    headers      TEXT NOT NULL DEFAULT '{}',
    created_at   INTEGER NOT NULL,
    dns_ms       INTEGER,
    tcp_ms       INTEGER,
    tls_ms       INTEGER,
    ttfb_ms      INTEGER
);
CREATE INDEX IF NOT EXISTS checks_created_at       ON checks(created_at);
CREATE INDEX IF NOT EXISTS checks_property_created ON checks(property_id, created_at DESC);

CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`

// phaseColumns are in the CREATE TABLE above as well. SQLite has no ADD COLUMN
// IF NOT EXISTS, and CREATE TABLE IF NOT EXISTS does not reconcile columns on a
// table that already exists, so ensurePhaseColumns adds them by hand.
var phaseColumns = []addedColumn{
	{"dns_ms", "INTEGER"},
	{"tcp_ms", "INTEGER"},
	{"tls_ms", "INTEGER"},
	{"ttfb_ms", "INTEGER"},
	{"cf_cache_status", "TEXT"},
	{"age", "INTEGER"},
}

type addedColumn struct{ name, typ string }

// openDB opens the SQLite database and applies the schema. The pragmas go in the
// DSN because they are per connection and database/sql opens connections lazily,
// so setting them once after Open would reach only one of them.
func openDB(path string) (*sql.DB, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create data dir: %w", err)
		}
	}

	dsn := path +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=foreign_keys(ON)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// SQLite takes a database-wide write lock, so a pool larger than one buys
	// nothing for writers. It is for readers, which WAL runs during a write.
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	db.SetConnMaxLifetime(time.Hour)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if err := ensurePhaseColumns(db); err != nil {
		return nil, err
	}
	return db, nil
}

func ensurePhaseColumns(db *sql.DB) error {
	rows, err := db.Query("PRAGMA table_info(checks)")
	if err != nil {
		return fmt.Errorf("inspect checks table: %w", err)
	}
	defer rows.Close()

	have := map[string]bool{}
	for rows.Next() {
		var (
			cid        int
			name, typ  string
			notNull    int
			defaultVal any
			pk         int
		)
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultVal, &pk); err != nil {
			return fmt.Errorf("inspect checks table: %w", err)
		}
		have[name] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("inspect checks table: %w", err)
	}

	for _, col := range phaseColumns {
		if have[col.name] {
			continue
		}
		if _, err := db.Exec("ALTER TABLE checks ADD COLUMN " + col.name + " " + col.typ); err != nil {
			return fmt.Errorf("add checks.%s: %w", col.name, err)
		}
	}
	return nil
}

func nowMS() int64 { return time.Now().UnixMilli() }

// Property is one tracked URL. Every nullable column is a pointer rather than a
// sql.NullInt64, so templates and JSON can test it with a plain nil check.
type Property struct {
	ID          uuid.UUID
	URL         string
	IsPublic    bool
	IsProtected bool

	LastRunAt *int64
	NextRunAt *int64

	LastRunAtCrawler    *int64
	NextRunAtCrawler    *int64
	CrawlerInsights     *string
	CrawlState          string
	CrawlStartedAt      *int64
	LastCrawlSuccessAt  *int64
	LastCrawlError      *string
	LastCrawlDurationMS *int64
	LastCrawlPagesCount *int64

	LighthouseScores         *string
	LighthouseDetails        *string
	LastLighthouseRunAt      *int64
	LastLighthouseSuccessAt  *int64
	LastLighthouseError      *string
	LastLighthouseDurationMS *int64
	NextLighthouseRunAt      *int64
	LighthouseState          string
	LighthouseStartedAt      *int64

	AlertState    string
	LastAlertSent *int64

	CreatedAt int64
	UpdatedAt int64
}

// Name is the hostname without a leading www. Parsed rather than split on "/",
// because the URL is operator input and need not be well formed.
func (p *Property) Name() string {
	u, err := parseHTTPURL(p.URL)
	if err != nil || u.Hostname() == "" {
		return p.URL
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}

const propertyColumns = `id, url, is_public, is_protected,
	last_run_at, next_run_at,
	last_run_at_crawler, next_run_at_crawler, crawler_insights, crawl_state,
	crawl_started_at, last_crawl_success_at, last_crawl_error,
	last_crawl_duration_ms, last_crawl_pages_count,
	lighthouse_scores, lighthouse_details, last_lighthouse_run_at,
	last_lighthouse_success_at, last_lighthouse_error,
	last_lighthouse_duration_ms, next_lighthouse_run_at, lighthouse_state,
	lighthouse_started_at,
	alert_state, last_alert_sent, created_at, updated_at`

func scanProperty(scan func(...any) error) (*Property, error) {
	var (
		p     Property
		idRaw []byte
		pub   int64
		prot  int64
	)
	err := scan(
		&idRaw, &p.URL, &pub, &prot,
		&p.LastRunAt, &p.NextRunAt,
		&p.LastRunAtCrawler, &p.NextRunAtCrawler, &p.CrawlerInsights, &p.CrawlState,
		&p.CrawlStartedAt, &p.LastCrawlSuccessAt, &p.LastCrawlError,
		&p.LastCrawlDurationMS, &p.LastCrawlPagesCount,
		&p.LighthouseScores, &p.LighthouseDetails, &p.LastLighthouseRunAt,
		&p.LastLighthouseSuccessAt, &p.LastLighthouseError,
		&p.LastLighthouseDurationMS, &p.NextLighthouseRunAt, &p.LighthouseState,
		&p.LighthouseStartedAt,
		&p.AlertState, &p.LastAlertSent, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	id, err := uuid.FromBytes(idRaw)
	if err != nil {
		return nil, fmt.Errorf("property id is not a uuid: %w", err)
	}
	p.ID = id
	p.IsPublic = pub != 0
	p.IsProtected = prot != 0
	return &p, nil
}

// listProperties returns every property, optionally filtered by a substring of
// the URL.
func listProperties(ctx context.Context, db *sql.DB, search string) ([]*Property, error) {
	query := "SELECT " + propertyColumns + " FROM properties ORDER BY url"
	args := []any{}
	if search != "" {
		query = "SELECT " + propertyColumns + " FROM properties WHERE url LIKE ? ORDER BY url"
		args = append(args, "%"+search+"%")
	}

	rows, err := db.QueryContext(ctx, query, args...)
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

// getProperty returns the property with this id, or nil when there is none.
func getProperty(ctx context.Context, db *sql.DB, id uuid.UUID) (*Property, error) {
	row := db.QueryRowContext(ctx,
		"SELECT "+propertyColumns+" FROM properties WHERE id = ?", id[:])
	p, err := scanProperty(row.Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return p, err
}

func createProperty(ctx context.Context, db *sql.DB, rawURL string) (uuid.UUID, error) {
	id := uuid.New()
	now := nowMS()
	_, err := db.ExecContext(ctx,
		"INSERT INTO properties (id, url, created_at, updated_at) VALUES (?, ?, ?, ?)",
		id[:], rawURL, now, now)
	return id, err
}

// deleteProperty removes a property and, by foreign key, its checks. Protected
// properties are excluded in the statement itself, so no route can miss it.
func deleteProperty(ctx context.Context, db *sql.DB, id uuid.UUID) error {
	_, err := db.ExecContext(ctx,
		"DELETE FROM properties WHERE id = ? AND is_protected = 0", id[:])
	return err
}

// togglePublic flips the public flag and reports the new value. found is false
// with a nil error when there is no such property.
func togglePublic(ctx context.Context, db *sql.DB, id uuid.UUID) (isPublic, found bool, err error) {
	var current int64
	err = db.QueryRowContext(ctx,
		"SELECT is_public FROM properties WHERE id = ?", id[:]).Scan(&current)
	if err == sql.ErrNoRows {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}

	next := int64(1)
	if current != 0 {
		next = 0
	}
	if _, err := db.ExecContext(ctx,
		"UPDATE properties SET is_public = ?, updated_at = ? WHERE id = ?",
		next, nowMS(), id[:]); err != nil {
		return false, true, err
	}
	return next == 1, true, nil
}

// Check is one HTTP probe. The four phase timings are nullable, so the chart
// skips nulls; response_ms is the canonical total.
type Check struct {
	StatusCode int64
	ResponseMS int64
	Headers    string
	CreatedAt  int64
	DNSMS      *int64
	TCPMS      *int64
	TLSMS      *int64
	TTFBMS     *int64
}

func recentChecks(ctx context.Context, db *sql.DB, id uuid.UUID, limit int) ([]Check, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT status_code, response_ms, headers, created_at, dns_ms, tcp_ms, tls_ms, ttfb_ms
		 FROM checks WHERE property_id = ? ORDER BY created_at DESC LIMIT ?`, id[:], limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Check
	for rows.Next() {
		var c Check
		if err := rows.Scan(&c.StatusCode, &c.ResponseMS, &c.Headers, &c.CreatedAt,
			&c.DNSMS, &c.TCPMS, &c.TLSMS, &c.TTFBMS); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// StatusCount is one bar of the status-code chart.
type StatusCount struct {
	Code  int64
	Count int64
}

func countStatusCodes(ctx context.Context, db *sql.DB, id uuid.UUID) ([]StatusCount, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT status_code, COUNT(*) FROM checks WHERE property_id = ?
		 GROUP BY status_code ORDER BY status_code`, id[:])
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []StatusCount
	for rows.Next() {
		var s StatusCount
		if err := rows.Scan(&s.Code, &s.Count); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func countChecks(ctx context.Context, db *sql.DB, id uuid.UUID) (int64, error) {
	var n int64
	err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM checks WHERE property_id = ?", id[:]).Scan(&n)
	return n, err
}

// countUptime returns the all-time up and down counts. The COALESCE is required:
// SUM over zero rows is NULL, so a never-checked property would fail the scan.
func countUptime(ctx context.Context, db *sql.DB, id uuid.UUID) (up, down int64, err error) {
	err = db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(CASE WHEN status_code = 200 THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN status_code <> 200 THEN 1 ELSE 0 END), 0)
		 FROM checks WHERE property_id = ?`, id[:]).Scan(&up, &down)
	return up, down, err
}
