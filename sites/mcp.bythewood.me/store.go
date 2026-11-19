package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// A grant is one connector you allowed, holding the auth session it reads
// with. Tokens are only ever stored hashed, and the session is what auth
// handed over, since it has to be sent on to each tool.
const schema = `
CREATE TABLE IF NOT EXISTS grants (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    client_id   TEXT    NOT NULL,
    client_name TEXT    NOT NULL,
    session     TEXT    NOT NULL,
    resource    TEXT    NOT NULL,
    created     INTEGER NOT NULL,
    last_used   INTEGER NOT NULL DEFAULT 0,
    revoked     INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS tokens (
    hash     BLOB    PRIMARY KEY,
    grant_id INTEGER NOT NULL REFERENCES grants(id),
    kind     TEXT    NOT NULL,
    expires  INTEGER NOT NULL
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS tokens_grant ON tokens(grant_id);
`

const (
	accessTTL  = time.Hour
	refreshTTL = 60 * 24 * time.Hour
)

type Store struct{ db *sql.DB }

func OpenStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

type Grant struct {
	ID         int64
	ClientID   string
	ClientName string
	Session    string
	Resource   string
	Created    time.Time
	LastUsed   time.Time
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func tokenHash(t string) []byte {
	h := sha256.Sum256([]byte(t))
	return h[:]
}

func (s *Store) AddGrant(clientID, name, session, resource string) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO grants (client_id, client_name, session, resource, created) VALUES (?, ?, ?, ?, ?)`,
		clientID, name, session, resource, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Issue makes a fresh access and refresh token pair for a grant.
func (s *Store) Issue(grant int64) (access, refresh string, err error) {
	access, refresh = randomToken(), randomToken()
	now := time.Now()
	tx, err := s.db.Begin()
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`DELETE FROM tokens WHERE expires < ?`, now.Unix()); err != nil {
		return "", "", err
	}
	if _, err = tx.Exec(`INSERT INTO tokens (hash, grant_id, kind, expires) VALUES (?, ?, 'access', ?), (?, ?, 'refresh', ?)`,
		tokenHash(access), grant, now.Add(accessTTL).Unix(), tokenHash(refresh), grant, now.Add(refreshTTL).Unix()); err != nil {
		return "", "", err
	}
	return access, refresh, tx.Commit()
}

var errNoToken = errors.New("no such token")

// Lookup finds the live grant behind a token of the given kind.
func (s *Store) Lookup(token, kind string) (Grant, error) {
	var g Grant
	var created, used int64
	err := s.db.QueryRow(`SELECT g.id, g.client_id, g.client_name, g.session, g.resource, g.created, g.last_used
		FROM tokens t JOIN grants g ON g.id = t.grant_id
		WHERE t.hash = ? AND t.kind = ? AND t.expires > ? AND g.revoked = 0`,
		tokenHash(token), kind, time.Now().Unix()).Scan(&g.ID, &g.ClientID, &g.ClientName, &g.Session, &g.Resource, &created, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return g, errNoToken
	}
	g.Created, g.LastUsed = time.Unix(created, 0), time.Unix(used, 0)
	return g, err
}

// Spend removes a refresh token once it's been used, since every refresh hands
// out a new one.
func (s *Store) Spend(token string) error {
	_, err := s.db.Exec(`DELETE FROM tokens WHERE hash = ?`, tokenHash(token))
	return err
}

func (s *Store) Touch(grant int64) {
	_, _ = s.db.Exec(`UPDATE grants SET last_used = ? WHERE id = ? AND last_used < ?`, time.Now().Unix(), grant, time.Now().Add(-time.Minute).Unix())
}

// Revoke ends a grant and every token it holds, for when auth says its
// session is gone.
func (s *Store) Revoke(grant int64) {
	_, _ = s.db.Exec(`UPDATE grants SET revoked = 1 WHERE id = ?`, grant)
	_, _ = s.db.Exec(`DELETE FROM tokens WHERE grant_id = ?`, grant)
}

func (s *Store) Grants() ([]Grant, error) {
	rows, err := s.db.Query(`SELECT id, client_id, client_name, resource, created, last_used FROM grants WHERE revoked = 0 ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Grant
	for rows.Next() {
		var g Grant
		var created, used int64
		if rows.Scan(&g.ID, &g.ClientID, &g.ClientName, &g.Resource, &created, &used) == nil {
			g.Created, g.LastUsed = time.Unix(created, 0), time.Unix(used, 0)
			out = append(out, g)
		}
	}
	return out, rows.Err()
}
