package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// History is the questions and the answers, which is the one thing the cache
// deliberately does not hold.
//
// It is a second database rather than two more tables, for two reasons that are
// both Isaac's requirements rather than tidiness. Deleting every question has
// to be possible without throwing away the page archive, which is a personal
// library of public articles and the expensive thing to rebuild. And a separate
// file is one line to exclude from restic, which matters because a row deleted
// here is still in whatever snapshot already went to B2. Deletion is forward
// only, so the honest way to keep something out of a backup is to have never
// put it in one.
//
// secure_delete is on so a deleted question is zeroed rather than left in the
// free pages of the file, which is the difference between deleting a row and
// deleting the text.
type History struct {
	db *sql.DB
}

const historySchema = `
CREATE TABLE IF NOT EXISTS answers (
  id          INTEGER PRIMARY KEY,
  asked_at    INTEGER NOT NULL,
  question    TEXT NOT NULL,
  standalone  TEXT NOT NULL DEFAULT '',
  shape       TEXT NOT NULL DEFAULT '',
  skill       TEXT NOT NULL DEFAULT '',
  answer      TEXT NOT NULL,
  queries     TEXT NOT NULL DEFAULT '[]',
  sources     TEXT NOT NULL DEFAULT '[]',
  warnings    TEXT NOT NULL DEFAULT '[]',
  support     REAL NOT NULL DEFAULT 0,
  checked     INTEGER NOT NULL DEFAULT 0,
  retried     INTEGER NOT NULL DEFAULT 0,
  elapsed_ms  INTEGER NOT NULL DEFAULT 0,

  -- What produced it. Without these a month of thumbs cannot be read, since
  -- there is no telling which of them are about a prompt that has since been
  -- rewritten or a quant that has since been swapped.
  model       TEXT NOT NULL DEFAULT '',
  prompts     TEXT NOT NULL DEFAULT '',
  sampling    TEXT NOT NULL DEFAULT '',
  build       TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS answers_asked ON answers(asked_at DESC);

-- One verdict per answer, so changing your mind replaces it rather than
-- stacking. What is wanted here is the current opinion, not its history.
CREATE TABLE IF NOT EXISTS feedback (
  answer_id INTEGER PRIMARY KEY REFERENCES answers(id) ON DELETE CASCADE,
  rated_at  INTEGER NOT NULL,
  verdict   INTEGER NOT NULL,
  reason    TEXT NOT NULL DEFAULT '',
  note      TEXT NOT NULL DEFAULT ''
);

-- Domain reputation survives a history wipe on purpose. It holds no question
-- text and nothing that could be read back as one, and deleting a month of
-- questions for privacy should not also delete what the system learned from
-- answering them.
CREATE TABLE IF NOT EXISTS domains (
  site TEXT PRIMARY KEY,
  good INTEGER NOT NULL DEFAULT 0,
  bad  INTEGER NOT NULL DEFAULT 0
);
`

func OpenHistory(dataDir string) (*History, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "history.db")+
		"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=secure_delete(1)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(historySchema); err != nil {
		return nil, err
	}
	return &History{db: db}, nil
}

func (h *History) Close() error { return h.db.Close() }

// Entry is one logged answer, with its verdict when it has one.
type Entry struct {
	ID       int64
	Asked    time.Time
	Question string
	Shape    string
	Skill    string
	Answer   string
	Queries  []string
	Sources  []Source
	Warnings []string
	Support  float64
	Checked  int
	Retried  bool
	Elapsed  string
	Model    string
	Prompts  string
	Sampling string
	Verdict  int
	Reason   string
	Note     string
}

// Body renders the stored markdown for the history page. The HTML is not
// stored beside it, since it is derived and storing both means keeping them in
// step forever.
func (e Entry) Body() template.HTML { return template.HTML(renderMarkdown(e.Answer)) }

// Rated is whether a verdict has been given, since zero means unrated.
func (e Entry) Rated() bool { return e.Verdict != 0 }

// Good and Bad read better in a template than comparing to a number.
func (e Entry) Good() bool { return e.Verdict > 0 }
func (e Entry) Bad() bool  { return e.Verdict < 0 }

// Log writes an answer. Incognito questions never reach here at all, which is
// why there is no flag for it on the row: an unlogged question leaves nothing
// to mark, and a row that said "this one was private" would itself be a record
// that a private question was asked.
func (h *History) Log(a *Answer, stamp Stamp) (int64, error) {
	res, err := h.db.Exec(`
		INSERT INTO answers
		  (asked_at, question, standalone, shape, skill, answer, queries, sources,
		   warnings, support, checked, retried, elapsed_ms, model, prompts, sampling, build)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		time.Now().Unix(), a.Query, a.Standalone, string(a.Shape), a.Skill, a.Text,
		asJSON(a.Queries), asJSON(a.Sources), asJSON(a.Warnings),
		a.Support, countChecked(a.Citations), a.Retried, millis(a.Elapsed),
		stamp.Model, stamp.Prompts, stamp.Sampling, stamp.Build)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Rate records a verdict and moves the domains behind that answer with it. The
// reputation is deliberately a count of answers rather than of sources, so a
// page cited once in a good answer counts once.
func (h *History) Rate(id int64, verdict int, reason, note string) error {
	if verdict > 0 {
		verdict = 1
	} else if verdict < 0 {
		verdict = -1
	}
	var raw string
	if err := h.db.QueryRow(`SELECT sources FROM answers WHERE id = ?`, id).Scan(&raw); err != nil {
		return err
	}
	var prev int
	h.db.QueryRow(`SELECT verdict FROM feedback WHERE answer_id = ?`, id).Scan(&prev)

	tx, err := h.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if verdict == 0 {
		if _, err := tx.Exec(`DELETE FROM feedback WHERE answer_id = ?`, id); err != nil {
			return err
		}
	} else if _, err := tx.Exec(`
		INSERT INTO feedback (answer_id, rated_at, verdict, reason, note)
		VALUES (?,?,?,?,?)
		ON CONFLICT(answer_id) DO UPDATE SET
		  rated_at = excluded.rated_at, verdict = excluded.verdict,
		  reason = excluded.reason, note = excluded.note`,
		id, time.Now().Unix(), verdict, reason, note); err != nil {
		return err
	}

	// Undo whatever the previous verdict did before applying the new one, so
	// changing your mind does not leave both counted.
	for _, site := range sitesOf(raw) {
		if _, err := tx.Exec(`INSERT INTO domains (site) VALUES (?) ON CONFLICT(site) DO NOTHING`, site); err != nil {
			return err
		}
		if err := bump(tx, site, prev, -1); err != nil {
			return err
		}
		if err := bump(tx, site, verdict, 1); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func bump(tx *sql.Tx, site string, verdict, by int) error {
	switch {
	case verdict > 0:
		_, err := tx.Exec(`UPDATE domains SET good = MAX(0, good + ?) WHERE site = ?`, by, site)
		return err
	case verdict < 0:
		_, err := tx.Exec(`UPDATE domains SET bad = MAX(0, bad + ?) WHERE site = ?`, by, site)
		return err
	}
	return nil
}

// List returns the newest first. only may be "down", "up" or "" for everything,
// since the reason to open this page is usually to find what went wrong.
func (h *History) List(limit, offset int, only string) ([]Entry, error) {
	where := ""
	switch only {
	case "down":
		where = "WHERE f.verdict < 0"
	case "up":
		where = "WHERE f.verdict > 0"
	case "unrated":
		where = "WHERE f.verdict IS NULL"
	}
	rows, err := h.db.Query(`
		SELECT a.id, a.asked_at, a.question, a.shape, a.skill, a.answer,
		       a.queries, a.sources, a.warnings, a.support, a.checked, a.retried,
		       a.elapsed_ms, a.model, a.prompts, a.sampling,
		       COALESCE(f.verdict, 0), COALESCE(f.reason, ''), COALESCE(f.note, '')
		FROM answers a LEFT JOIN feedback f ON f.answer_id = a.id
		`+where+`
		ORDER BY a.asked_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Entry
	for rows.Next() {
		var (
			e                       Entry
			asked, ms               int64
			queries, sources, warns string
		)
		if err := rows.Scan(&e.ID, &asked, &e.Question, &e.Shape, &e.Skill, &e.Answer,
			&queries, &sources, &warns, &e.Support, &e.Checked, &e.Retried,
			&ms, &e.Model, &e.Prompts, &e.Sampling,
			&e.Verdict, &e.Reason, &e.Note); err != nil {
			return nil, err
		}
		e.Asked = time.Unix(asked, 0)
		e.Elapsed = (time.Duration(ms) * time.Millisecond).Round(100 * time.Millisecond).String()
		json.Unmarshal([]byte(queries), &e.Queries)
		json.Unmarshal([]byte(sources), &e.Sources)
		json.Unmarshal([]byte(warns), &e.Warnings)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (h *History) Count() (total, rated int) {
	h.db.QueryRow(`SELECT COUNT(*) FROM answers`).Scan(&total)
	h.db.QueryRow(`SELECT COUNT(*) FROM feedback`).Scan(&rated)
	return total, rated
}

func (h *History) Delete(id int64) error {
	if _, err := h.db.Exec(`DELETE FROM answers WHERE id = ?`, id); err != nil {
		return err
	}
	return h.vacuum()
}

// DeleteAll leaves the domain counts standing. They are what the site learned
// rather than what was asked, and rebuilding them would need the questions
// that are being deleted.
func (h *History) DeleteAll() error {
	if _, err := h.db.Exec(`DELETE FROM answers`); err != nil {
		return err
	}
	return h.vacuum()
}

// vacuum is what actually removes the text rather than the row, and it takes
// all three steps.
//
// secure_delete zeroes a deleted row where it lay and VACUUM rewrites the file
// without it, but in WAL mode both of those write through the log, so the
// question is still sitting in history.db-wal afterwards. Deleting a row and
// then finding it in a text search of the directory is the whole failure this
// page exists to prevent, so the checkpoint truncates the log as well. Tested
// by reading the files, since that is the only way to know.
func (h *History) vacuum() error {
	if _, err := h.db.Exec(`VACUUM`); err != nil {
		return err
	}
	_, err := h.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	return err
}

// Reputation is the score per domain, for ranking. Laplace smoothed, so one
// bad answer does not bury a site and a domain nobody has judged sits at the
// neutral 0.5 rather than at zero.
func (h *History) Reputation() map[string]float64 {
	rows, err := h.db.Query(`SELECT site, good, bad FROM domains`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := map[string]float64{}
	for rows.Next() {
		var site string
		var good, bad int
		if err := rows.Scan(&site, &good, &bad); err != nil {
			return out
		}
		out[site] = float64(good+1) / float64(good+bad+2)
	}
	return out
}

// Stamp is what produced an answer, recorded beside it. A thumb is only
// readable next month if it says which model and which prompts it was about.
type Stamp struct {
	Model    string
	Prompts  string
	Sampling string
	Build    string
}

// promptVersion hashes every instruction the model is given, so an edit to any
// contract shows up as a different version without anyone remembering to bump
// one. Sampling is separate, since changing a temperature is not changing a
// prompt and the two are worth telling apart when reading the thumbs back.
func promptVersion() string {
	var parts []string
	for _, s := range shapeEnum {
		parts = append(parts, string(s), contractFor(s).Instruction, contractFor(s).Reminder)
	}
	parts = append(parts, houseStyle, answerFirst, datesArePast, datesAreFuture)
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])[:12]
}

func samplingVersion() string {
	return fmt.Sprintf("exact %.2f/%.2f/%d prose %.2f/%.2f/%d/%.2f/%.2f",
		exact.Temperature, exact.TopP, exact.TopK,
		prose.Temperature, prose.TopP, prose.TopK, prose.MinP, prose.PresencePenalty)
}

func asJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// sitesOf pulls the domains out of a stored sources blob.
func sitesOf(raw string) []string {
	var srcs []Source
	if json.Unmarshal([]byte(raw), &srcs) != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, s := range srcs {
		host := hostname(s.URL)
		if host == "" || seen[host] {
			continue
		}
		seen[host] = true
		out = append(out, host)
	}
	return out
}

func millis(elapsed string) int64 {
	d, err := time.ParseDuration(elapsed)
	if err != nil {
		return 0
	}
	return d.Milliseconds()
}
