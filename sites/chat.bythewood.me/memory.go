// What the chat remembers about Isaac between conversations.
//
// One sentence facts and nothing longer. A memory that holds paragraphs is a
// second transcript, and the point of this is that a fact costs almost nothing
// to carry into every turn, so only the ones worth carrying are kept.
//
// Every write goes through the model under the rules below. There is no text
// box that edits a fact directly, which is deliberate: a fact typed by hand
// drifts out of the shape the retrieval expects, and a contradiction typed by
// hand leaves both versions in. Deleting is the one manual operation, because
// deciding something should be forgotten needs no judgement a model can add.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	// A fact longer than this is a paragraph wearing a full stop.
	maxFactChars = 200
	// How many facts a turn carries. Enough to be useful, few enough that a
	// wrong one is visible rather than buried.
	factsPerTurn = 6
)

const factSchema = `
CREATE TABLE IF NOT EXISTS facts (
  id         INTEGER PRIMARY KEY,
  fact       TEXT NOT NULL UNIQUE,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  used       INTEGER NOT NULL DEFAULT 0,
  last_used  INTEGER NOT NULL DEFAULT 0
);
`

type Fact struct {
	ID      int64     `json:"id"`
	Text    string    `json:"fact"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
	Used    int       `json:"used"`
}

func (s *Store) initFacts() error {
	_, err := s.db.Exec(factSchema)
	return err
}

func (s *Store) Facts() ([]Fact, error) {
	rows, err := s.db.Query(`SELECT id, fact, created_at, updated_at, used FROM facts ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Fact{}
	for rows.Next() {
		var f Fact
		var created, updated int64
		if err := rows.Scan(&f.ID, &f.Text, &created, &updated, &f.Used); err != nil {
			return nil, err
		}
		f.Created, f.Updated = time.Unix(created, 0), time.Unix(updated, 0)
		out = append(out, f)
	}
	return out, rows.Err()
}

// AddFact is idempotent on the text, so the model proposing the same fact twice
// touches the timestamp rather than filling the table with duplicates.
func (s *Store) AddFact(text string) (int64, error) {
	text = tidyFact(text)
	if text == "" {
		return 0, fmt.Errorf("an empty fact")
	}
	now := time.Now().Unix()
	r, err := s.db.Exec(`
		INSERT INTO facts(fact, created_at, updated_at) VALUES(?,?,?)
		ON CONFLICT(fact) DO UPDATE SET updated_at = excluded.updated_at`, text, now, now)
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

func (s *Store) ReplaceFact(id int64, text string) error {
	text = tidyFact(text)
	if text == "" {
		return fmt.Errorf("an empty fact")
	}
	_, err := s.db.Exec(`UPDATE facts SET fact=?, updated_at=? WHERE id=?`, text, time.Now().Unix(), id)
	return err
}

func (s *Store) DeleteFact(id int64) error {
	_, err := s.db.Exec(`DELETE FROM facts WHERE id=?`, id)
	return err
}

func (s *Store) ForgetEverything() error {
	_, err := s.db.Exec(`DELETE FROM facts`)
	return err
}

func (s *Store) markUsed(ids []int64) {
	if len(ids) == 0 {
		return
	}
	now := time.Now().Unix()
	for _, id := range ids {
		if _, err := s.db.Exec(`UPDATE facts SET used = used + 1, last_used = ? WHERE id = ?`, now, id); err != nil {
			slog.Debug("marking a fact used", "err", err)
			return
		}
	}
}

// Relevant scores every fact against the message in Go rather than in SQL.
//
// This is a few hundred rows at most, so a full scan costs less than the
// round trip to ask for a clever one, and it means the scoring is a function
// with a test rather than a query whose behaviour is the database's opinion.
func (s *Store) Relevant(message string, limit int) []Fact {
	all, err := s.Facts()
	if err != nil || len(all) == 0 {
		return nil
	}
	want := terms(message)
	if len(want) == 0 {
		return nil
	}

	type scored struct {
		f Fact
		n int
	}
	hits := make([]scored, 0, len(all))
	for _, f := range all {
		n := overlap(want, terms(f.Text))
		if n > 0 {
			hits = append(hits, scored{f, n})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].n != hits[j].n {
			return hits[i].n > hits[j].n
		}
		// A tie goes to the fact that has earned its place, then to the newer
		// one, so a stale duplicate loses to the one actually being used.
		if hits[i].f.Used != hits[j].f.Used {
			return hits[i].f.Used > hits[j].f.Used
		}
		return hits[i].f.Updated.After(hits[j].f.Updated)
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]Fact, 0, len(hits))
	ids := make([]int64, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.f)
		ids = append(ids, h.f.ID)
	}
	s.markUsed(ids)
	return out
}

// stopwords are the words that match everything and therefore mean nothing
// here. Without them "what is my plan for the weekend" matches every fact
// containing "my".
var stopwords = map[string]bool{
	"a": true, "about": true, "all": true, "am": true, "an": true, "and": true,
	"any": true, "are": true, "as": true, "at": true, "be": true, "been": true,
	"but": true, "by": true, "can": true, "did": true, "do": true, "does": true,
	"for": true, "from": true, "get": true, "had": true, "has": true, "have": true,
	"he": true, "her": true, "him": true, "his": true, "how": true, "i": true,
	"if": true, "in": true, "is": true, "it": true, "its": true, "just": true,
	"like": true, "me": true, "my": true, "no": true, "not": true, "of": true,
	"on": true, "or": true, "our": true, "out": true, "she": true, "so": true,
	"some": true, "than": true, "that": true, "the": true, "their": true,
	"them": true, "then": true, "there": true, "these": true, "they": true,
	"this": true, "to": true, "up": true, "was": true, "we": true, "were": true,
	"what": true, "when": true, "where": true, "which": true, "who": true,
	"why": true, "will": true, "with": true, "would": true, "you": true,
	"your": true,
}

func terms(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len(w) < 3 || stopwords[w] {
			continue
		}
		out[stem(w)] = true
	}
	return out
}

// stem is the smallest thing that makes retrieval work on real questions.
// Without it "camping" in a question never reaches "camps" in a fact, which was
// the first case tried and the first one that failed. It is not a real stemmer
// and does not need to be: over a few hundred short facts an occasional wrong
// pairing costs one irrelevant line in the prompt.
func stem(w string) string {
	switch {
	case len(w) > 5 && strings.HasSuffix(w, "ing"):
		w = strings.TrimSuffix(w, "ing")
	case len(w) > 4 && strings.HasSuffix(w, "ed"):
		w = strings.TrimSuffix(w, "ed")
	case len(w) > 3 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss"):
		w = strings.TrimSuffix(w, "s")
	}
	// "running" leaves "runn", so a doubled final consonant loses one.
	if n := len(w); n > 2 && w[n-1] == w[n-2] && !strings.ContainsRune("aeiou", rune(w[n-1])) {
		w = w[:n-1]
	}
	return w
}

func overlap(a, b map[string]bool) int {
	n := 0
	for w := range a {
		if b[w] {
			n++
		}
	}
	return n
}

func tidyFact(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	s = strings.Trim(s, "-*• ")
	if len(s) > maxFactChars {
		s = s[:maxFactChars]
	}
	return s
}

// ---------------------------------------------------------------- the rules

// factRules is the whole contract for writing to this. It is one string used by
// both the pass that runs after a turn and the box in the memory panel, so the
// two cannot disagree about what belongs here.
//
// The framing is positive on purpose. An earlier version listed what not to keep
// first and ended on "an empty list is the right answer most of the time", and a
// small model took that as permission to propose nothing every single time. The
// same conversation against this wording produces five usable facts.
const factRules = `You pull durable facts about Isaac out of a conversation and keep them for later.

Your job is to notice what was revealed about him. Read the exchange and write down each thing that will still be true in six months.

Worth keeping:
- who he is, his people, his home, his work
- his machines, his tools, what he runs and how
- his money: what he banks with, what he spends on, what he pays for regularly
- his preferences and habits, stated or clearly implied

Not worth keeping:
- what the weather is, what a page said, a price, a score, anything a tool looks up
- what he is doing this minute, today or this week
- passwords, keys, tokens, account numbers, anything secret
- facts about the world rather than about him

Each fact is one short sentence in the third person, starting with Isaac or with the thing it is about, specific enough to act on. "Isaac likes coffee" is too vague. "Isaac drinks his coffee black" is right. Carry the number when there is one: "Isaac spends about $40 a month on coffee" beats "Isaac spends money on coffee".

If a new fact contradicts one you already have, replace that one rather than adding a second.

Reply with JSON and nothing else:
{"changes":[{"op":"add","fact":"..."},{"op":"replace","id":3,"fact":"..."},{"op":"delete","id":7}]}`

// memChange is one proposed edit. Delete and replace carry the id of the fact
// they act on, which is why the existing facts are numbered in the prompt.
type memChange struct {
	Op   string `json:"op"`
	ID   int64  `json:"id,omitempty"`
	Fact string `json:"fact,omitempty"`
	Why  string `json:"why,omitempty"`
}

// Remember runs the extraction pass over one exchange and applies what comes
// back. It is called after the answer has been sent, so its cost is never in
// front of the reader, and every failure is logged and dropped rather than
// surfaced, because a memory that did not save is not worth an error message.
func (s *site) Remember(ctx context.Context, user, assistant string) {
	if strings.TrimSpace(user) == "" {
		return
	}
	existing, err := s.store.Facts()
	if err != nil {
		return
	}
	changes, err := s.proposeChanges(ctx, existing,
		fmt.Sprintf("The exchange:\n\nIsaac said:\n%s\n\nYou answered:\n%s", trim(user, 4000), trim(assistant, 2000)),
		"What did this reveal about Isaac that is worth keeping?")
	if err != nil {
		slog.Error("the memory pass failed", "err", err)
		return
	}
	// Logged at info even when nothing changed. A pass that silently proposes
	// an empty list every time looks exactly like one that is not running, and
	// telling those apart took a trip through the gateway's call log.
	applied := s.applyChanges(changes, existing)
	slog.Info("memory pass", "known", len(existing), "proposed", len(changes), "applied", len(applied))
}

// proposeChanges is the one place the model is asked to write memory. Both the
// automatic pass and the memory page go through it, so the rules apply to both.
func (s *site) proposeChanges(ctx context.Context, existing []Fact, situation, ask string) ([]memChange, error) {
	var known strings.Builder
	if len(existing) == 0 {
		known.WriteString("(nothing yet)")
	}
	for _, f := range existing {
		fmt.Fprintf(&known, "%d. %s\n", f.ID, f.Text)
	}

	user := fmt.Sprintf("Facts you already have:\n%s\n\n%s\n\n%s", known.String(), situation, ask)

	out, err := s.llm.Complete(ctx, []Message{
		{Role: RoleSystem, Content: factRules},
		{Role: RoleUser, Content: user},
	}, nil, 600)
	if err != nil {
		return nil, err
	}
	return parseChanges(out.Content), nil
}

// parseChanges is forgiving about the wrapping and strict about the contents. A
// small model puts JSON in a fence or writes a sentence before it, and neither
// is a reason to lose the edit.
func parseChanges(raw string) []memChange {
	raw = strings.TrimSpace(raw)
	if i := strings.Index(raw, "{"); i > 0 {
		raw = raw[i:]
	}
	if j := strings.LastIndex(raw, "}"); j >= 0 {
		raw = raw[:j+1]
	}
	var body struct {
		Changes []memChange `json:"changes"`
	}
	if json.Unmarshal([]byte(raw), &body) != nil {
		return nil
	}
	out := make([]memChange, 0, len(body.Changes))
	for _, c := range body.Changes {
		c.Op = strings.ToLower(strings.TrimSpace(c.Op))
		c.Fact = tidyFact(c.Fact)
		switch c.Op {
		case "add":
			if c.Fact != "" {
				out = append(out, c)
			}
		case "replace":
			if c.ID > 0 && c.Fact != "" {
				out = append(out, c)
			}
		case "delete":
			if c.ID > 0 {
				out = append(out, c)
			}
		}
	}
	return out
}

// applyChanges is the fence the model does not get to talk its way past. It
// refuses anything that looks like a credential whatever the rules said, and it
// will not act on an id that is not really there.
func (s *site) applyChanges(changes []memChange, existing []Fact) []memChange {
	known := map[int64]bool{}
	for _, f := range existing {
		known[f.ID] = true
	}
	applied := make([]memChange, 0, len(changes))
	for _, c := range changes {
		if c.Fact != "" && looksSecret(c.Fact) {
			slog.Warn("a proposed fact looked like a credential and was dropped")
			continue
		}
		var err error
		switch c.Op {
		case "add":
			_, err = s.store.AddFact(c.Fact)
		case "replace":
			if !known[c.ID] {
				continue
			}
			err = s.store.ReplaceFact(c.ID, c.Fact)
		case "delete":
			if !known[c.ID] {
				continue
			}
			err = s.store.DeleteFact(c.ID)
		}
		if err != nil {
			slog.Debug("applying a memory change", "op", c.Op, "err", err)
			continue
		}
		applied = append(applied, c)
	}
	return applied
}

// looksSecret is a last fence rather than the only one. The rules already say
// not to keep a credential, and a model that ignores them once must not be able
// to write one into a file that is read into every future turn.
func looksSecret(s string) bool {
	low := strings.ToLower(s)
	for _, w := range []string{"password", "passphrase", "api key", "apikey", "secret key",
		"private key", "token is", "bearer ", "orch-", "ssh-rsa", "begin private"} {
		if strings.Contains(low, w) {
			return true
		}
	}
	// A long unbroken run of key shaped characters is a credential whatever it
	// is called, and no real sentence about a person contains one.
	for _, field := range strings.Fields(s) {
		if len(field) >= 24 && !strings.ContainsAny(field, " .,;:!?") && looksRandom(field) {
			return true
		}
	}
	return false
}

func looksRandom(s string) bool {
	var digits, upper, lower int
	for _, r := range s {
		switch {
		case unicode.IsDigit(r):
			digits++
		case unicode.IsUpper(r):
			upper++
		case unicode.IsLower(r):
			lower++
		}
	}
	return digits > 0 && upper > 0 && lower > 0
}

// memoryBlock is what gets folded into the system prompt. It is left out
// entirely when nothing matched, rather than saying it knows nothing, which a
// model reads as an instruction to talk about not knowing things.
func memoryBlock(facts []Fact) string {
	if len(facts) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\nWhat you remember about Isaac, which may or may not bear on this question:\n")
	for _, f := range facts {
		fmt.Fprintf(&sb, "- %s\n", f.Text)
	}
	sb.WriteString("Use one only if it actually helps. Do not list them back at him or mention remembering.")
	return sb.String()
}
