package main

// The database holds only what git cannot: push tokens, description and topics,
// and mirror state. Refs, commits, trees, sizes and dates are never copied here,
// since a cache of something git can answer is a second source of truth.

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"

	_ "modernc.org/sqlite"
)

const schema = `
-- One row per repository this site knows about, whether it arrived by push or
-- by mirror. The row is metadata only: delete it and the repository still
-- serves, it just loses its description.
CREATE TABLE IF NOT EXISTS repos (
    name          TEXT PRIMARY KEY,
    description   TEXT    NOT NULL DEFAULT '',
    topics        TEXT    NOT NULL DEFAULT '[]',
    homepage      TEXT    NOT NULL DEFAULT '',
    -- A mirror is pulled from upstream and refuses pushes. A pushed repo is
    -- the opposite. The column is what the wire checks before receive-pack.
    mirror        INTEGER NOT NULL DEFAULT 0,
    upstream      TEXT    NOT NULL DEFAULT '',
    -- Set when upstream reports the repository archived, and shown as a
    -- badge. Twenty of the twenty-one repositories on the account are
    -- archived, so this is the common case rather than an exception.
    archived      INTEGER NOT NULL DEFAULT 0,
    -- The point of the mirror half: a repository that vanished upstream but
    -- still lives here. Loud on the index, because it is the only copy.
    upstream_gone INTEGER NOT NULL DEFAULT 0,
    last_sync     INTEGER NOT NULL DEFAULT 0,
    last_sync_err TEXT    NOT NULL DEFAULT '',
    created       INTEGER NOT NULL DEFAULT 0,
    hidden        INTEGER NOT NULL DEFAULT 0
);

-- Push credentials. The token itself is never stored, only an Argon2id hash of
-- it, so a copy of this database does not let anybody push.
--
-- salt is per token rather than global. A single site-wide salt would mean two
-- tokens with the same value hash identically, which leaks nothing useful here
-- with one operator but costs nothing to do properly.
CREATE TABLE IF NOT EXISTS tokens (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    label     TEXT    NOT NULL,
    -- The first eight characters of the token, stored in clear. Not a
    -- secret and not enough to authenticate with; it is what lets the UI
    -- show "which of these three is the one on my laptop" without keeping
    -- the token itself.
    prefix    TEXT    NOT NULL,
    hash      BLOB    NOT NULL,
    salt      BLOB    NOT NULL,
    created   INTEGER NOT NULL,
    last_used INTEGER NOT NULL DEFAULT 0,
    revoked   INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS tokens_prefix ON tokens(prefix) WHERE revoked = 0;

-- What the mirror lane pulls from. Rows rather than a constant, because which
-- account or repository to back up is an operational decision that changes, and
-- changing it should not need a rebuild.
--
-- kind is 'account' (everything owned by that login) or 'repo' (one named
-- repository). name is empty for an account. The unique constraint is what
-- makes adding the same source twice a no-op rather than a duplicate fetch.
CREATE TABLE IF NOT EXISTS mirror_sources (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    kind    TEXT    NOT NULL,
    owner   TEXT    NOT NULL,
    name    TEXT    NOT NULL DEFAULT '',
    created INTEGER NOT NULL,
    UNIQUE(kind, owner, name)
);

-- Small key/value store for facts about this installation rather than about a
-- repository. It exists for the seed marker below and is the right home for the
-- next flag of that shape.
CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`

// DB wraps the connection. One writer, so no pool tuning beyond the pragmas.
type DB struct {
	sql *sql.DB
}

// OpenDB opens or creates the database beside the repositories.
func OpenDB(dir string) (*DB, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	path := filepath.Join(dir, "repos.db")

	// WAL so a mirror sync writing state does not block a page read, and a
	// busy_timeout because a push-to-create and a sync tick can land together.
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"

	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if _, err := conn.Exec(schema); err != nil {
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &DB{sql: conn}, nil
}

func (d *DB) Close() error { return d.sql.Close() }

// RepoMeta is the row, with topics already decoded.
type RepoMeta struct {
	Name         string
	Description  string
	Topics       []string
	Homepage     string
	Mirror       bool
	Upstream     string
	Archived     bool
	UpstreamGone bool
	LastSync     time.Time
	LastSyncErr  string
	Created      time.Time
	Hidden       bool
}

// Repo reads one row. No row is not an error; it is a repository pushed and
// never described.
func (d *DB) Repo(name string) (RepoMeta, error) {
	row := d.sql.QueryRow(`
        SELECT name, description, topics, homepage, mirror, upstream,
               archived, upstream_gone, last_sync, last_sync_err, created, hidden
        FROM repos WHERE name = ?`, name)
	return scanRepo(row)
}

type scanner interface{ Scan(...any) error }

func scanRepo(row scanner) (RepoMeta, error) {
	var (
		m                              RepoMeta
		topics                         string
		mirror, archived, gone, hidden int
		lastSync, created              int64
	)
	err := row.Scan(&m.Name, &m.Description, &topics, &m.Homepage, &mirror,
		&m.Upstream, &archived, &gone, &lastSync, &m.LastSyncErr, &created, &hidden)
	if err != nil {
		return RepoMeta{}, err
	}
	_ = json.Unmarshal([]byte(topics), &m.Topics)
	m.Mirror = mirror == 1
	m.Archived = archived == 1
	m.UpstreamGone = gone == 1
	m.Hidden = hidden == 1
	if lastSync > 0 {
		m.LastSync = time.Unix(lastSync, 0).UTC()
	}
	if created > 0 {
		m.Created = time.Unix(created, 0).UTC()
	}
	return m, nil
}

// AllRepos reads every row keyed by name, so the index joins against the on-disk
// list in one pass.
func (d *DB) AllRepos() (map[string]RepoMeta, error) {
	rows, err := d.sql.Query(`
        SELECT name, description, topics, homepage, mirror, upstream,
               archived, upstream_gone, last_sync, last_sync_err, created, hidden
        FROM repos`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]RepoMeta)
	for rows.Next() {
		m, err := scanRepo(rows)
		if err != nil {
			return nil, err
		}
		out[m.Name] = m
	}
	return out, rows.Err()
}

// EnsureRepo creates the metadata row if it is missing.
func (d *DB) EnsureRepo(name string) error {
	_, err := d.sql.Exec(`
        INSERT INTO repos (name, created) VALUES (?, ?)
        ON CONFLICT(name) DO NOTHING`, name, time.Now().Unix())
	return err
}

// SetDescription updates what the UI can edit. Topics are a JSON array, not a
// join table; nothing queries across them that a LIKE cannot answer.
func (d *DB) SetDescription(name, description string, topics []string, homepage string) error {
	if err := d.EnsureRepo(name); err != nil {
		return err
	}
	blob, err := json.Marshal(topics)
	if err != nil {
		return err
	}
	_, err = d.sql.Exec(`
        UPDATE repos SET description = ?, topics = ?, homepage = ? WHERE name = ?`,
		description, string(blob), homepage, name)
	return err
}

// SetHidden keeps a repository on disk but off the index. There is no delete here.
func (d *DB) SetHidden(name string, hidden bool) error {
	if err := d.EnsureRepo(name); err != nil {
		return err
	}
	v := 0
	if hidden {
		v = 1
	}
	_, err := d.sql.Exec(`UPDATE repos SET hidden = ? WHERE name = ?`, v, name)
	return err
}

// MarkMirror records that a repository is a mirror of an upstream URL.
func (d *DB) MarkMirror(name, upstream string, archived bool) error {
	if err := d.EnsureRepo(name); err != nil {
		return err
	}
	a := 0
	if archived {
		a = 1
	}
	_, err := d.sql.Exec(`
        UPDATE repos SET mirror = 1, upstream = ?, archived = ? WHERE name = ?`,
		upstream, a, name)
	return err
}

// RecordSync writes the outcome of one mirror fetch.
func (d *DB) RecordSync(name string, err error) error {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	_, dberr := d.sql.Exec(`
        UPDATE repos SET last_sync = ?, last_sync_err = ? WHERE name = ?`,
		time.Now().Unix(), msg, name)
	return dberr
}

// MarkUpstreamGone marks a repository whose upstream is gone, so the index can
// say this is the only copy left.
func (d *DB) MarkUpstreamGone(name string, gone bool) error {
	v := 0
	if gone {
		v = 1
	}
	_, err := d.sql.Exec(`UPDATE repos SET upstream_gone = ? WHERE name = ?`, v, name)
	return err
}

// Token is one credential, without the secret.
type Token struct {
	ID       int64
	Label    string
	Prefix   string
	Created  time.Time
	LastUsed time.Time
}

// Argon2id parameters, modest because what is hashed is a 256 bit random value
// rather than a human password. The hash is here so a leaked database is not a
// credential, not to withstand a dictionary attack.
const (
	argonTime    = 1
	argonMemory  = 64 * 1024
	argonThreads = 4
	argonKeyLen  = 32
	saltLen      = 16
	// 32 bytes is 43 base64url characters, so the 8 stored in clear give nothing away.
	tokenBytes = 32
)

// CreateToken mints a push credential and returns it once; the database keeps
// only a hash, so there is no way to read it back.
func (d *DB) CreateToken(label string) (string, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		label = "unnamed"
	}

	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)

	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	hash := argon2.IDKey([]byte(token), salt, argonTime, argonMemory, argonThreads, argonKeyLen)

	_, err := d.sql.Exec(`
        INSERT INTO tokens (label, prefix, hash, salt, created)
        VALUES (?, ?, ?, ?, ?)`,
		label, token[:8], hash, salt, time.Now().Unix())
	if err != nil {
		return "", err
	}
	return token, nil
}

var errNoToken = errors.New("no such token")

// VerifyToken checks a credential and returns its label. Every prefix candidate
// is compared in constant time, so a colliding prefix gives nothing away by timing.
func (d *DB) VerifyToken(token string) (string, error) {
	if len(token) < 8 {
		return "", errNoToken
	}

	rows, err := d.sql.Query(`
        SELECT id, label, hash, salt FROM tokens
        WHERE prefix = ? AND revoked = 0`, token[:8])
	if err != nil {
		return "", err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id         int64
			label      string
			hash, salt []byte
		)
		if err := rows.Scan(&id, &label, &hash, &salt); err != nil {
			return "", err
		}
		candidate := argon2.IDKey([]byte(token), salt,
			argonTime, argonMemory, argonThreads, argonKeyLen)
		if subtle.ConstantTimeCompare(candidate, hash) == 1 {
			// Best effort: a push must not fail because this write did.
			_, _ = d.sql.Exec(`UPDATE tokens SET last_used = ? WHERE id = ?`,
				time.Now().Unix(), id)
			return label, nil
		}
	}
	return "", errNoToken
}

// Tokens lists the live credentials for the settings page.
func (d *DB) Tokens() ([]Token, error) {
	rows, err := d.sql.Query(`
        SELECT id, label, prefix, created, last_used FROM tokens
        WHERE revoked = 0 ORDER BY created DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Token
	for rows.Next() {
		var (
			t                 Token
			created, lastUsed int64
		)
		if err := rows.Scan(&t.ID, &t.Label, &t.Prefix, &created, &lastUsed); err != nil {
			return nil, err
		}
		t.Created = time.Unix(created, 0).UTC()
		if lastUsed > 0 {
			t.LastUsed = time.Unix(lastUsed, 0).UTC()
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// RevokeToken is one UPDATE, effective on the next request.
func (d *DB) RevokeToken(id int64) error {
	_, err := d.sql.Exec(`UPDATE tokens SET revoked = 1 WHERE id = ?`, id)
	return err
}

// HasTokens reports whether any credential exists, so a fresh install can be told
// to mint one.
func (d *DB) HasTokens() bool {
	var n int
	if err := d.sql.QueryRow(`SELECT COUNT(*) FROM tokens WHERE revoked = 0`).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

// MirrorSource is one thing the mirror lane pulls from: a whole GitHub account,
// or a single repository on one.
type MirrorSource struct {
	ID      int64
	Kind    string // "account" or "repo"
	Owner   string
	Name    string // empty for an account
	Created time.Time
}

// Label is "owner" for an account and "owner/name" for a single repository.
func (m MirrorSource) Label() string {
	if m.Kind == sourceRepo {
		return m.Owner + "/" + m.Name
	}
	return m.Owner
}

func (m MirrorSource) URL() string { return "https://github.com/" + m.Label() }

const (
	sourceAccount = "account"
	sourceRepo    = "repo"
)

// ghNamePart is GitHub's charset for a login or repository name. These values are
// interpolated into an API URL, so they are checked at the form.
var ghNamePart = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ParseMirrorSource accepts "owner" or "owner/repo"; the slash is the whole grammar.
func ParseMirrorSource(input string) (MirrorSource, error) {
	in := strings.TrimSpace(input)
	// A pasted GitHub URL is accepted rather than rejected.
	in = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(in,
		"https://github.com/"), "github.com/"), ".git")
	// Only a trailing slash is forgiven; a leading one means the owner is missing.
	in = strings.TrimSuffix(in, "/")

	if in == "" {
		return MirrorSource{}, errors.New("enter an account or an owner/repository")
	}
	owner, name, hasSlash := strings.Cut(in, "/")
	if !ghNamePart.MatchString(owner) || len(owner) > 100 {
		return MirrorSource{}, fmt.Errorf("%q is not a GitHub account name", owner)
	}
	if !hasSlash {
		return MirrorSource{Kind: sourceAccount, Owner: owner}, nil
	}
	if !ghNamePart.MatchString(name) || len(name) > 100 || strings.Contains(name, "/") {
		return MirrorSource{}, fmt.Errorf("%q is not a repository name", name)
	}
	return MirrorSource{Kind: sourceRepo, Owner: owner, Name: name}, nil
}

// MirrorSources lists every configured source, accounts first.
func (d *DB) MirrorSources() ([]MirrorSource, error) {
	rows, err := d.sql.Query(`
        SELECT id, kind, owner, name, created FROM mirror_sources
        ORDER BY kind DESC, owner, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []MirrorSource
	for rows.Next() {
		var m MirrorSource
		var created int64
		if err := rows.Scan(&m.ID, &m.Kind, &m.Owner, &m.Name, &created); err != nil {
			return nil, err
		}
		m.Created = time.Unix(created, 0).UTC()
		out = append(out, m)
	}
	return out, rows.Err()
}

// AddMirrorSource stores a source. Adding one twice succeeds and changes nothing.
func (d *DB) AddMirrorSource(m MirrorSource) error {
	_, err := d.sql.Exec(`
        INSERT INTO mirror_sources (kind, owner, name, created) VALUES (?, ?, ?, ?)
        ON CONFLICT(kind, owner, name) DO NOTHING`,
		m.Kind, m.Owner, m.Name, time.Now().Unix())
	return err
}

// DeleteMirrorSource stops syncing a source. The repositories it brought in stay
// on disk; this site is a backup.
func (d *DB) DeleteMirrorSource(id int64) error {
	_, err := d.sql.Exec(`DELETE FROM mirror_sources WHERE id = ?`, id)
	return err
}

// SeedMirrorSources adds owner as a source on first run. Guarded by a marker
// rather than by an empty table, so deleting the last source keeps it deleted.
func (d *DB) SeedMirrorSources(owner string) error {
	var seeded string
	err := d.sql.QueryRow(`SELECT value FROM settings WHERE key = 'mirror_sources_seeded'`).Scan(&seeded)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err := d.AddMirrorSource(MirrorSource{Kind: sourceAccount, Owner: owner}); err != nil {
		return err
	}
	_, err = d.sql.Exec(
		`INSERT INTO settings (key, value) VALUES ('mirror_sources_seeded', ?)`,
		time.Now().UTC().Format(time.RFC3339))
	return err
}
