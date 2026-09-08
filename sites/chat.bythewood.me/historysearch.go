package main

// Searching what was said in earlier conversations.
//
// A tool result dies with the turn that fetched it and only the answer
// survives, so a question about something settled last week had nothing to
// reach for. Isaac asked for this twice on 2026-09-08 and it is the other half
// of memory: memory keeps one sentence facts about him, and this keeps what was
// actually said.
//
// Scored in Go for the same reason memory is. The candidate rows come out of
// SQLite with a LIKE per term so the whole table never lands in memory, and the
// ranking is the part that has to be right, which is easier to test as a
// function than to argue about as a query.

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Hit is one earlier exchange that matched.
type Hit struct {
	ConvID   string    `json:"conversation_id"`
	Title    string    `json:"title"`
	When     time.Time `json:"when"`
	Question string    `json:"question"`
	Answer   string    `json:"answer"`
}

// searchExcerpt is how much of a message goes back to the model. Long enough to
// carry the substance of an exchange, short enough that ten of them do not take
// the window past the compaction threshold on their own.
const searchExcerpt = 700

// SearchHistory finds earlier exchanges matching a query. It returns whole
// exchanges rather than single messages, since an answer with no question in
// front of it reads as an assertion from nowhere.
func (s *Store) SearchHistory(query string, limit int) ([]Hit, error) {
	want := terms(query)
	if len(want) == 0 {
		return nil, fmt.Errorf("nothing to search for in %q", query)
	}
	if limit < 1 || limit > 25 {
		limit = 8
	}

	// One LIKE per term, ORed, which narrows to rows worth scoring without
	// pretending to be the ranking. SQLite has no index for this and does not
	// need one at the size this database ever reaches.
	var where []string
	var args []any
	for w := range want {
		where = append(where, "(m.content LIKE ? OR m.display LIKE ?)")
		args = append(args, "%"+w+"%", "%"+w+"%")
	}
	args = append(args, maxSearchRows)

	rows, err := s.db.Query(`
		SELECT m.id, m.conv_id, m.content, m.display, m.at, c.title
		FROM messages m JOIN conversations c ON c.id = m.conv_id
		WHERE `+strings.Join(where, " OR ")+`
		ORDER BY m.id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type row struct {
		id          int64
		conv        string
		text, title string
		at          time.Time
		score       int
	}
	var found []row
	for rows.Next() {
		var r row
		var content, display string
		var at int64
		if err := rows.Scan(&r.id, &r.conv, &content, &display, &at, &r.title); err != nil {
			return nil, err
		}
		r.text = display
		if r.text == "" {
			r.text = content
		}
		r.at = time.Unix(at, 0)
		r.score = overlap(want, terms(r.text))
		if r.score > 0 {
			found = append(found, r)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// One hit per conversation. Ten rows off one long thread is the same answer
	// ten times and crowds out every other conversation that matched.
	best := map[string]row{}
	for _, r := range found {
		if b, seen := best[r.conv]; !seen || r.score > b.score {
			best[r.conv] = r
		}
	}
	ranked := make([]row, 0, len(best))
	for _, r := range best {
		ranked = append(ranked, r)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		// A tie goes to the more recent exchange, since the later word on a
		// subject is usually the one that still holds.
		return ranked[i].at.After(ranked[j].at)
	})
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}

	out := make([]Hit, 0, len(ranked))
	for _, r := range ranked {
		h := Hit{ConvID: r.conv, Title: r.title, When: r.at}
		h.Question, h.Answer = s.exchangeAround(r.id)
		out = append(out, h)
	}
	return out, nil
}

// maxSearchRows caps what the LIKE hands back before anything is scored. A
// single common word against a long history would otherwise pull the whole
// table in to rank it.
const maxSearchRows = 400

// exchangeAround returns the question and the answer either side of a matching
// message, so a hit carries what was asked as well as what was said.
func (s *Store) exchangeAround(id int64) (question, answer string) {
	get := func(q string, args ...any) string {
		var content, display string
		if err := s.db.QueryRow(q, args...).Scan(&content, &display); err != nil {
			return ""
		}
		if display != "" {
			return trim(display, searchExcerpt)
		}
		return trim(content, searchExcerpt)
	}
	const prior = `SELECT content, display FROM messages
		WHERE conv_id=(SELECT conv_id FROM messages WHERE id=?) AND role=? AND id<=?
		ORDER BY id DESC LIMIT 1`
	const next = `SELECT content, display FROM messages
		WHERE conv_id=(SELECT conv_id FROM messages WHERE id=?) AND role=? AND id>=?
		ORDER BY id ASC LIMIT 1`

	// The same pair of queries covers both roles. A matching user message is
	// its own question and the reply after it is the answer, and a matching
	// assistant message is its own answer with the question before it.
	return get(prior, id, string(RoleUser), id), get(next, id, string(RoleAssistant), id)
}
