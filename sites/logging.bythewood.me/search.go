package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"html/template"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Search reads the raw table, so every query here is bounded by the window,
// a row cap or both, and the page as a whole by searchBudget.

const (
	searchPerPage = 100
	maxSearchPage = 50
	patternRowCap = 50000
	// scanCap bounds the one pass every panel is counted from, newest first.
	scanCap        = 300000
	patternShapes  = 5000
	contextLines   = 25
	tailFirstLines = 30
	tailBatch      = 200
	// A tail started or resumed only looks this far back by id, so a rare
	// query can't turn one poll into a scan of the whole table.
	tailLookback = 20000
	searchBudget = 12 * time.Second
)

// searchSlots caps the searches running at once. Each is a handful of scans
// over up to 30 days of rows on a 2 core box that is also taking ingest.
var searchSlots = make(chan struct{}, 2)

// searchRanges are the windows the raw table can answer.
var searchRanges = []string{"1h", "6h", "24h", "7d", "30d"}

// bucketFor keeps the histogram at roughly 30 to 85 bars. Each step allows an
// extra hour, since a named window's start is floored to the hour.
func bucketFor(span int64) int64 {
	const min = int64(60 * 1000)
	const hour = 60 * min
	switch {
	case span <= 2*hour:
		return 2 * min
	case span <= 7*hour:
		return 5 * min
	case span <= 25*hour:
		return 30 * min
	case span <= 7*24*hour+hour:
		return 3 * hour
	default:
		return 12 * hour
	}
}

type HistBucket struct {
	Label           string
	Err, Warn, Rest int64
	S5xx, S4xx      int64
	In              bool
	Unread          bool
	Href            string
	Tip             string
}

func (b HistBucket) Total() int64 { return b.Err + b.Warn + b.Rest }

type FacetValue struct {
	Value string
	Count int64
	On    bool
	Href  string
	Kind  string
}

type Facet struct {
	Name   string
	Field  string
	Values []FacetValue
}

type Chip struct {
	Label string
	Neg   bool
	Href  string
}

type Pattern struct {
	Shape  string
	Level  string
	Source string
	Count  int64
	Share  float64
	First  string
	Last   string
	Spark  []float64
	Href   string
	first  int64
	last   int64
	filter []term
}

type KV struct {
	Key   string
	Value string
	Plus  string
	Minus string
}

// Line is one record as the explorer shows it, with the pairs it opens into.
type Line struct {
	RecordRow
	Stamp   string
	KV      []KV
	AttrRun string
	Terms   []string
	Context string
	Target  bool
}

type RangeLink struct {
	Key  string
	Href string
	On   bool
}

type SearchView struct {
	Q         string
	terms     []term
	RangeKey  string
	RangeName string
	StartMS   int64
	EndMS     int64
	FromMS    int64
	ToMS      int64
	Zoomed    bool
	BucketMS  int64
	View      string

	Ranges       []RangeLink
	Chips        []Chip
	ClearHref    string
	UnzoomHref   string
	LinesHref    string
	PatternsHref string
	BaseHref     string
	PrevHref     string
	NextHref     string
	MdHref       string

	Matches  int64
	Errors   int64
	Warns    int64
	S5xx     int64
	S4xx     int64
	Requests int64
	Scanned  int64
	Sources  int64
	TopSrc   []LabelCount
	First    string
	Last     string
	Latency  Latency
	Peak     HistBucket

	Hist      []HistBucket
	HistTicks []string
	Facets    []Facet
	Patterns  []Pattern
	PatCapped bool
	Capped    bool
	CapMS     int64
	CapAt     string
	Shapes    int

	Results  []Line
	Page     int
	Pages    int64
	PerPage  int
	HasMore  bool
	Retained int

	Context  *RecordRow
	CtxBack  string
	Slow     bool
	TailHref string
}

func (v *SearchView) MatchSpark(kind string) []float64 {
	out := make([]float64, len(v.Hist))
	for i, b := range v.Hist {
		switch kind {
		case "errors":
			out[i] = float64(b.Err)
		case "warns":
			out[i] = float64(b.Warn)
		case "5xx":
			out[i] = float64(b.S5xx)
		case "4xx":
			out[i] = float64(b.S4xx)
		default:
			out[i] = float64(b.Total())
		}
	}
	return out
}

func (v *SearchView) HistLabels() []string {
	out := make([]string, len(v.Hist))
	for i, b := range v.Hist {
		out[i] = b.Label
	}
	return out
}

// HistMax is the tallest bar, for scaling.
func (v *SearchView) HistMax() int64 {
	var m int64 = 1
	for _, b := range v.Hist {
		if t := b.Total(); t > m {
			m = t
		}
	}
	return m
}

func (v *SearchView) TopPatternShare() float64 {
	if len(v.Patterns) == 0 {
		return 0
	}
	return v.Patterns[0].Share
}

// href is this search with the query, window and mode swapped as asked.
func (v *SearchView) href(q string, from, to int64, view string) string {
	vals := url.Values{}
	if q != "" {
		vals.Set("q", q)
	}
	vals.Set("range", v.RangeKey)
	if v.RangeKey == "custom" {
		vals.Set("start", time.UnixMilli(v.StartMS).UTC().Format("2006-01-02"))
		vals.Set("end", time.UnixMilli(v.EndMS-1).UTC().Format("2006-01-02"))
	}
	if from > 0 && to > 0 {
		vals.Set("from", strconv.FormatInt(from, 10))
		vals.Set("to", strconv.FormatInt(to, 10))
	}
	if view != "" && view != "lines" {
		vals.Set("view", view)
	}
	return "/search?" + vals.Encode()
}

func (v *SearchView) withTerms(ts []term) string {
	return v.href(joinTerms(ts), v.zoomFrom(), v.zoomTo(), v.View)
}

func (v *SearchView) zoomFrom() int64 {
	if v.Zoomed {
		return v.FromMS
	}
	return 0
}

func (v *SearchView) zoomTo() int64 {
	if v.Zoomed {
		return v.ToMS
	}
	return 0
}

// searchWhere is the window plus the query, skip as in termClauses.
func searchWhere(start, end int64, ts []term, skip string) (string, []any) {
	cl, args := termClauses(ts, skip)
	return strings.Join(append([]string{"ts >= ?", "ts < ?"}, cl...), " AND "),
		append([]any{start, end}, args...)
}

func quiet(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

// searchFailed is queryFailed, except that running out of budget is the page
// saying so and not an error.
func searchFailed(v *SearchView, mu *sync.Mutex, what string, err error) {
	if err == nil || err == sql.ErrNoRows {
		return
	}
	if quiet(err) {
		mu.Lock()
		v.Slow = true
		mu.Unlock()
		return
	}
	queryFailed(what, err)
}

func hasInclude(ts []term, field string) bool {
	for _, t := range ts {
		if t.Field == field && !t.Neg {
			return true
		}
	}
	return false
}

func topOf(m map[string]int64, n int) []LabelCount {
	out := make([]LabelCount, 0, len(m))
	for k, c := range m {
		out = append(out, LabelCount{Label: k, Count: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Label < out[j].Label
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// facetColumns is the allowlist for a facet's GROUP BY.
var facetColumns = map[string]string{
	"source": "source", "level": "level", "status": "status", "component": "component", "path": "path",
}

// facetValues counts one field over the matches with that field's own
// includes left out, so picking shop still shows what the other sources hold.
func facetValues(ctx context.Context, db *sql.DB, v *SearchView, mu *sync.Mutex, field string, from int64) []FacetValue {
	col, ok := facetColumns[field]
	if !ok {
		return nil
	}
	w, args := searchWhere(from, v.ToMS, v.terms, field)
	empty := "''"
	if col == "status" {
		empty = "0"
	}
	rows, err := db.QueryContext(ctx, `
		SELECT `+col+`, COUNT(*) AS n FROM records
		WHERE `+w+` AND `+col+` != `+empty+`
		GROUP BY `+col+` ORDER BY n DESC LIMIT 6`, args...)
	if err != nil {
		searchFailed(v, mu, "facet "+field, err)
		return nil
	}
	defer rows.Close()
	var out []FacetValue
	for rows.Next() {
		var fv FacetValue
		if err := rows.Scan(&fv.Value, &fv.Count); err != nil {
			queryFailed("facet scan", err)
			return out
		}
		out = append(out, fv)
	}
	searchFailed(v, mu, "facet rows", rows.Err())
	return out
}

func (v *SearchView) decorateFacet(f *Facet) {
	for i := range f.Values {
		fv := &f.Values[i]
		val := normalizeValue(f.Field, fv.Value)
		for _, t := range v.terms {
			if t.Field == f.Field && !t.Neg {
				for _, tv := range t.Values {
					if tv == val {
						fv.On = true
					}
				}
			}
		}
		fv.Href = v.withTerms(toggleValue(v.terms, f.Field, fv.Value, false))
		switch {
		case f.Field == "level" && fv.Value == "ERROR", f.Field == "status" && strings.HasPrefix(fv.Value, "5"):
			fv.Kind = "bad"
		case f.Field == "level" && fv.Value == "WARN", f.Field == "status" && strings.HasPrefix(fv.Value, "4"):
			fv.Kind = "warn"
		}
	}
}

const recordCols = `id, source, ts, level, msg, component, method, path, host,
		       status, duration_ms, ip, cf_ray, attrs`

func scanRecords(rows *sql.Rows, what string) ([]RecordRow, error) {
	var out []RecordRow
	for rows.Next() {
		var r RecordRow
		if err := rows.Scan(&r.ID, &r.Source, &r.TS, &r.Level, &r.Msg, &r.Component,
			&r.Method, &r.Path, &r.Host, &r.Status, &r.DurationMS, &r.IP, &r.CFRay, &r.Attrs); err != nil {
			return out, err
		}
		r.Time = time.UnixMilli(r.TS).UTC().Format("2006-01-02 15:04:05")
		out = append(out, r)
	}
	return out, rows.Err()
}

func searchLines(ctx context.Context, db *sql.DB, v *SearchView, mu *sync.Mutex, limit, offset int) []RecordRow {
	w, args := searchWhere(v.FromMS, v.ToMS, v.terms, "")
	rows, err := db.QueryContext(ctx, `SELECT `+recordCols+` FROM records WHERE `+w+`
		ORDER BY ts DESC, id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		searchFailed(v, mu, "search lines", err)
		return nil
	}
	defer rows.Close()
	out, err := scanRecords(rows, "search lines")
	searchFailed(v, mu, "search lines rows", err)
	return out
}

// contextRows is the record and 25 lines either side of it from the same
// source, newest first, read off records_source_ts.
func contextRows(ctx context.Context, db *sql.DB, id int64) (*RecordRow, []RecordRow) {
	rows, err := db.QueryContext(ctx, `SELECT `+recordCols+` FROM records WHERE id = ?`, id)
	if err != nil {
		queryFailed("context target", err)
		return nil, nil
	}
	target, err := scanRecords(rows, "context target")
	rows.Close()
	if err != nil || len(target) == 0 {
		queryFailed("context target", err)
		return nil, nil
	}
	t := target[0]

	var before, after []RecordRow
	rows, err = db.QueryContext(ctx, `SELECT `+recordCols+` FROM records
		WHERE source = ? AND (ts > ? OR (ts = ? AND id > ?))
		ORDER BY ts, id LIMIT ?`, t.Source, t.TS, t.TS, t.ID, contextLines)
	if err == nil {
		after, err = scanRecords(rows, "context after")
		rows.Close()
	}
	queryFailed("context after", err)
	rows, err = db.QueryContext(ctx, `SELECT `+recordCols+` FROM records
		WHERE source = ? AND (ts < ? OR (ts = ? AND id < ?))
		ORDER BY ts DESC, id DESC LIMIT ?`, t.Source, t.TS, t.TS, t.ID, contextLines)
	if err == nil {
		before, err = scanRecords(rows, "context before")
		rows.Close()
	}
	queryFailed("context before", err)

	out := make([]RecordRow, 0, len(before)+len(after)+1)
	for i := len(after) - 1; i >= 0; i-- {
		out = append(out, after[i])
	}
	out = append(out, t)
	out = append(out, before...)
	return &t, out
}

// mergeTerms adds each of add unless the query already has that field.
func mergeTerms(base, add []term) []term {
	out := append([]term{}, base...)
	for _, a := range add {
		if !hasInclude(base, a.Field) {
			out = append(out, a)
		}
	}
	return out
}

func wildcard(shape string) string {
	r := strings.NewReplacer("<n>", "*", "<id>", "*")
	return r.Replace(shape)
}

// maskPath folds the parts of a path that change per request, an id or a
// number, so /products/42 and /products/7 are one shape.
func maskPath(p string) string {
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	segs := strings.Split(p, "/")
	for i, s := range segs {
		if len(s) > 24 || strings.ContainsAny(s, "0123456789") {
			segs[i] = "<id>"
		}
	}
	return strings.Join(segs, "/")
}

// maskMessage folds numbers, hex ids and paths out of a message.
func maskMessage(m string) string {
	words := strings.Fields(m)
	for i, w := range words {
		switch {
		case strings.HasPrefix(w, "/"):
			words[i] = maskPath(w)
		case isHexID(w):
			words[i] = "<id>"
		default:
			words[i] = maskNumbers(w)
		}
	}
	return strings.Join(words, " ")
}

func isHexID(w string) bool {
	w = strings.ReplaceAll(w, "-", "")
	if len(w) < 8 {
		return false
	}
	digit := false
	for _, r := range w {
		switch {
		case r >= '0' && r <= '9':
			digit = true
		case r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return digit
}

func maskNumbers(w string) string {
	var b strings.Builder
	in := false
	for i := 0; i < len(w); i++ {
		c := w[i]
		isNum := c >= '0' && c <= '9' || (in && (c == '.' || c == ',') && i+1 < len(w) && w[i+1] >= '0' && w[i+1] <= '9')
		if isNum {
			if !in {
				b.WriteString("<n>")
				in = true
			}
			continue
		}
		in = false
		b.WriteByte(c)
	}
	return b.String()
}

func stampLayout(span int64) string {
	if span <= 25*60*60*1000 {
		return "15:04"
	}
	return "Jan 2 15:04"
}

// lines dresses rows for the template, with the pairs each opens into.
func (v *SearchView) lines(rows []RecordRow) []Line {
	hl := highlightTerms(v.terms)
	out := make([]Line, len(rows))
	for i, r := range rows {
		out[i] = buildLine(r, hl, func(field, value string, neg bool) string {
			return v.withTerms(toggleValue(v.terms, field, value, neg))
		}, func(id int64) string {
			return v.href(v.Q, 0, 0, "") + "&context=" + strconv.FormatInt(id, 10)
		})
	}
	return out
}

// dashLines is the same for a dashboard, where + and − start a new search.
func dashLines(rows []RecordRow, rangeKey string) []Line {
	out := make([]Line, len(rows))
	if rangeKey == "" || !inSearchRanges(rangeKey) {
		rangeKey = "24h"
	}
	link := func(q string) string {
		return "/search?" + url.Values{"q": {q}, "range": {rangeKey}}.Encode()
	}
	for i, r := range rows {
		out[i] = buildLine(r, nil, func(field, value string, neg bool) string {
			return link(toggleValue(nil, field, value, neg)[0].String())
		}, func(id int64) string {
			return link("") + "&context=" + strconv.FormatInt(id, 10)
		})
	}
	return out
}

func inSearchRanges(k string) bool {
	for _, r := range searchRanges {
		if r == k {
			return true
		}
	}
	return false
}

func buildLine(r RecordRow, hl []string, filterHref func(field, value string, neg bool) string, ctxHref func(int64) string) Line {
	l := Line{RecordRow: r, Terms: hl, Context: ctxHref(r.ID)}
	l.Stamp = time.UnixMilli(r.TS).UTC().Format("01-02 15:04:05.000")
	add := func(key, field, value string) {
		kv := KV{Key: key, Value: value}
		if field != "" && value != "" {
			kv.Plus = filterHref(field, value, false)
			kv.Minus = filterHref(field, value, true)
		}
		l.KV = append(l.KV, kv)
	}
	add("source", "source", r.Source)
	add("level", "level", r.Level)
	if r.Component != "" {
		add("component", "component", r.Component)
	}
	if r.Status > 0 {
		add("status", "status", strconv.FormatInt(r.Status, 10))
	}
	if r.Method != "" {
		add("method", "method", r.Method)
	}
	if r.Path != "" {
		add("path", "path", r.Path)
	}
	if r.Host != "" {
		add("host", "host", r.Host)
	}
	if r.DurationMS > 0 {
		add("duration", "", formatMS(r.DurationMS))
	}
	if r.IP != "" {
		add("ip", "ip", r.IP)
	}
	if r.CFRay != "" {
		add("cf_ray", "cf_ray", r.CFRay)
	}
	var run []string
	for _, p := range attrPairs(r.Attrs) {
		field := ""
		if p.scalar && attrKey.MatchString(p.key) {
			if _, clash := columnFields[strings.ToLower(p.key)]; !clash {
				field = p.key
			}
		}
		add(p.key, field, p.value)
		run = append(run, p.key+"="+p.value)
	}
	l.AttrRun = strings.Join(run, " ")
	return l
}

type attrPair struct {
	key, value string
	scalar     bool
}

// attrPairs reads the attrs bag in its stored order. A bag that won't parse
// comes back as one pair, so nothing is hidden.
func attrPairs(attrs string) []attrPair {
	if attrs == "" || attrs == "{}" {
		return nil
	}
	dec := json.NewDecoder(strings.NewReader(attrs))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return []attrPair{{key: "attrs", value: attrs}}
	}
	var out []attrPair
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		key, _ := tok.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			break
		}
		p := attrPair{key: key, value: string(raw), scalar: len(raw) > 0 && raw[0] != '{' && raw[0] != '['}
		if len(raw) > 0 && raw[0] == '"' {
			var s string
			if json.Unmarshal(raw, &s) == nil {
				p.value = s
			}
		}
		out = append(out, p)
	}
	return out
}

// mark escapes text and wraps each search word it holds in <mark>.
func mark(text string, terms []string) template.HTML {
	if len(terms) == 0 {
		return template.HTML(template.HTMLEscapeString(text))
	}
	lower := strings.ToLower(text)
	var b strings.Builder
	i := 0
	for i < len(text) {
		best, blen := -1, 0
		for _, t := range terms {
			if j := strings.Index(lower[i:], strings.ToLower(t)); j >= 0 && (best < 0 || j < best) {
				best, blen = j, len(t)
			}
		}
		if best < 0 {
			break
		}
		b.WriteString(template.HTMLEscapeString(text[i : i+best]))
		b.WriteString("<mark>" + template.HTMLEscapeString(text[i+best:i+best+blen]) + "</mark>")
		i += best + blen
	}
	b.WriteString(template.HTMLEscapeString(text[i:]))
	return template.HTML(b.String())
}

// searchFor links a dashboard value to the search for it, inside the window
// the dashboard was showing when the raw table can answer it.
func searchFor(rangeKey, field, value, extra string) string {
	if !inSearchRanges(rangeKey) {
		rangeKey = "24h"
	}
	q := toggleValue(nil, field, value, false)[0].String()
	if extra != "" {
		q += " " + extra
	}
	return "/search?" + url.Values{"q": {q}, "range": {rangeKey}}.Encode()
}
