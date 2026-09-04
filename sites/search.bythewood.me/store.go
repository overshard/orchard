package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Store holds the page archive and the search result cache.
//
// It deliberately never stores a question. Pages and passages are an archive of
// public articles, so they are kept in full and indexed for search, while the
// only thing that has to persist per query is a lookup key, and that is an HMAC
// so the database cannot be read back as a history of what was asked.
type Store struct {
	db     *sql.DB
	secret []byte
}

const schema = `
CREATE TABLE IF NOT EXISTS pages (
  id         INTEGER PRIMARY KEY,
  url        TEXT NOT NULL UNIQUE,
  title      TEXT NOT NULL DEFAULT '',
  site       TEXT NOT NULL DEFAULT '',
  published  TEXT NOT NULL DEFAULT '',
  markdown   TEXT NOT NULL,
  fetched_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS passages (
  id      INTEGER PRIMARY KEY,
  page_id INTEGER NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
  ord     INTEGER NOT NULL,
  text    TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS passages_page ON passages(page_id);

CREATE VIRTUAL TABLE IF NOT EXISTS passages_fts USING fts5(
  text, content='passages', content_rowid='id', tokenize='porter unicode61'
);

CREATE TRIGGER IF NOT EXISTS passages_ai AFTER INSERT ON passages BEGIN
  INSERT INTO passages_fts(rowid, text) VALUES (new.id, new.text);
END;
CREATE TRIGGER IF NOT EXISTS passages_ad AFTER DELETE ON passages BEGIN
  INSERT INTO passages_fts(passages_fts, rowid, text) VALUES('delete', old.id, old.text);
END;

-- Outbound links found in a page's article body, so a cache hit still has the
-- candidates an entity link is resolved from.
CREATE TABLE IF NOT EXISTS links (
  id      INTEGER PRIMARY KEY,
  page_id INTEGER NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
  url     TEXT NOT NULL,
  text    TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS links_page ON links(page_id);

-- key is HMAC(secret, normalized query). The query text itself is never here.
CREATE TABLE IF NOT EXISTS serp (
  key        TEXT PRIMARY KEY,
  results    TEXT NOT NULL,
  fetched_at INTEGER NOT NULL
);
`

func OpenStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "search.db")+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	secret, err := loadSecret(dataDir)
	if err != nil {
		return nil, err
	}
	return &Store{db: db, secret: secret}, nil
}

// loadSecret keeps the HMAC key beside the database. It has to persist or every
// restart would miss every cached search, and it has to be secret or the keys
// could be checked against a dictionary of guessed queries.
func loadSecret(dataDir string) ([]byte, error) {
	path := filepath.Join(dataDir, "cache.key")
	if b, err := os.ReadFile(path); err == nil && len(b) >= 32 {
		return b, nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return nil, err
	}
	return b, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) key(query string) string {
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte(strings.ToLower(strings.Join(strings.Fields(query), " "))))
	return hex.EncodeToString(m.Sum(nil))
}

// CachedSERP returns a stored result list for a query, or nil past its TTL.
func (s *Store) CachedSERP(query string, ttl time.Duration) []Result {
	var blob string
	var at int64
	err := s.db.QueryRow(`SELECT results, fetched_at FROM serp WHERE key = ?`, s.key(query)).Scan(&blob, &at)
	if err != nil || time.Since(time.Unix(at, 0)) > ttl {
		return nil
	}
	var out []Result
	if json.Unmarshal([]byte(blob), &out) != nil {
		return nil
	}
	return out
}

func (s *Store) PutSERP(query string, results []Result) error {
	blob, err := json.Marshal(results)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO serp(key, results, fetched_at) VALUES(?,?,?)
		 ON CONFLICT(key) DO UPDATE SET results=excluded.results, fetched_at=excluded.fetched_at`,
		s.key(query), string(blob), time.Now().Unix())
	return err
}

// CachedPage returns a stored page if it was fetched inside the TTL. Articles
// do not change, so this can be generous.
func (s *Store) CachedPage(url string, ttl time.Duration) *Page {
	var p Page
	var at int64
	err := s.db.QueryRow(
		`SELECT url, title, site, published, markdown, fetched_at FROM pages WHERE url = ?`, url,
	).Scan(&p.URL, &p.Title, &p.Site, &p.Published, &p.Markdown, &at)
	if err != nil || time.Since(time.Unix(at, 0)) > ttl {
		return nil
	}
	return &p
}

// PutPage stores a page and replaces its passages.
func (s *Store) PutPage(p *Page) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	_, err = tx.Exec(
		`INSERT INTO pages(url, title, site, published, markdown, fetched_at) VALUES(?,?,?,?,?,?)
		 ON CONFLICT(url) DO UPDATE SET title=excluded.title, site=excluded.site,
		   published=excluded.published, markdown=excluded.markdown, fetched_at=excluded.fetched_at`,
		p.URL, p.Title, p.Site, p.Published, p.Markdown, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	var id int64
	if err := tx.QueryRow(`SELECT id FROM pages WHERE url = ?`, p.URL).Scan(&id); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`DELETE FROM passages WHERE page_id = ?`, id); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`DELETE FROM links WHERE page_id = ?`, id); err != nil {
		return 0, err
	}
	for _, l := range p.Links {
		if _, err := tx.Exec(`INSERT INTO links(page_id, url, text) VALUES(?,?,?)`, id, l.URL, l.Text); err != nil {
			return 0, err
		}
	}
	for i, text := range Chunk(p.Markdown) {
		if _, err := tx.Exec(`INSERT INTO passages(page_id, ord, text) VALUES(?,?,?)`, id, i, text); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

// PageLinks returns the outbound links stored for a page.
func (s *Store) PageLinks(pageID int64) []Link {
	rows, err := s.db.Query(`SELECT url, text FROM links WHERE page_id = ?`, pageID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Link
	for rows.Next() {
		var l Link
		if rows.Scan(&l.URL, &l.Text) == nil {
			out = append(out, l)
		}
	}
	return out
}

// LocalHit is a passage found in the archive rather than on the web.
type LocalHit struct {
	URL   string
	Title string
	Text  string
}

// SearchLocal queries the passage index. This is what lets a question reuse
// pages fetched for an unrelated question that shared none of its words.
func (s *Store) SearchLocal(query string, limit int) []LocalHit {
	rows, err := s.db.Query(`
		SELECT p.url, p.title, x.text
		FROM passages_fts f
		JOIN passages x ON x.id = f.rowid
		JOIN pages p ON p.id = x.page_id
		WHERE passages_fts MATCH ?
		ORDER BY bm25(passages_fts) LIMIT ?`, ftsQuery(query), limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []LocalHit
	for rows.Next() {
		var h LocalHit
		if rows.Scan(&h.URL, &h.Title, &h.Text) == nil {
			out = append(out, h)
		}
	}
	return out
}

// ftsQuery turns a plain question into an OR query of its content words. FTS5
// treats most punctuation as syntax, so anything not alphanumeric is dropped
// rather than escaped.
func ftsQuery(q string) string {
	var words []string
	for _, f := range strings.Fields(strings.ToLower(q)) {
		clean := strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, f)
		if len(clean) > 2 && !stopword[clean] {
			words = append(words, clean)
		}
	}
	if len(words) == 0 {
		return "\"\""
	}
	return strings.Join(words, " OR ")
}

var stopword = map[string]bool{
	"the": true, "and": true, "for": true, "are": true, "was": true, "what": true,
	"how": true, "why": true, "who": true, "does": true, "did": true, "can": true,
	"you": true, "with": true, "from": true, "that": true, "this": true, "has": true,
}

func (s *Store) Stats() (pages, passages int) {
	s.db.QueryRow(`SELECT count(*) FROM pages`).Scan(&pages)
	s.db.QueryRow(`SELECT count(*) FROM passages`).Scan(&passages)
	return
}

// Chunk splits markdown into passages a model can be handed one at a time.
// Paragraph boundaries are the split points, and short ones get merged so a
// passage is a claim rather than a heading.
func Chunk(md string) []string {
	const target = 1200
	var out []string
	var cur strings.Builder
	for _, para := range strings.Split(md, "\n\n") {
		p := strings.TrimSpace(para)
		if p == "" {
			continue
		}
		if cur.Len() > 0 && cur.Len()+len(p) > target {
			out = append(out, cur.String())
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteString("\n\n")
		}
		cur.WriteString(p)
	}
	if strings.TrimSpace(cur.String()) != "" {
		out = append(out, cur.String())
	}
	return out
}

// RankPassages orders one page's chunks by how well they answer the question.
//
// Taking the first chunks of a page is what made recipe answers useless: the
// ingredients and the method sit well down the document, behind the story about
// the author's trip to Oaxaca. bm25 over the chunks the page just produced puts
// them back on top.
func (s *Store) RankPassages(pageID int64, question string, limit int) []string {
	rows, err := s.db.Query(`
		SELECT x.text
		FROM passages_fts f
		JOIN passages x ON x.id = f.rowid
		WHERE passages_fts MATCH ? AND x.page_id = ?
		ORDER BY bm25(passages_fts) LIMIT ?`, ftsQuery(question), pageID, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if rows.Scan(&t) == nil {
			out = append(out, t)
		}
	}
	return out
}

// PageChunks returns a page's chunks in document order, which is the fallback
// when nothing matches the query terms.
func (s *Store) PageChunks(pageID int64, limit int) []string {
	rows, err := s.db.Query(`SELECT text FROM passages WHERE page_id = ? ORDER BY ord LIMIT ?`, pageID, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if rows.Scan(&t) == nil {
			out = append(out, t)
		}
	}
	return out
}

// PageID resolves a stored page's row id, needed to rank its chunks after a
// cache hit skipped the insert.
func (s *Store) PageID(url string) int64 {
	var id int64
	s.db.QueryRow(`SELECT id FROM pages WHERE url = ?`, url).Scan(&id)
	return id
}
