package main

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"sync"
	"time"
)

// pathKeys bounds the path facet's map, since scanners make paths close to
// unbounded and only the top few are ever shown.
const pathKeys = 20000

// runSearch counts every panel from one pass over the matches, since each scan
// of a month of rows costs seconds. The lines come back by id afterwards and
// only a facet with a chosen value needs a query of its own.
func runSearch(ctx context.Context, db *sql.DB, v *SearchView) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	// Counted off the ts index, since the rollups also hold caddy lines that
	// never get a raw row and the matches beside this are raw.
	wg.Go(func() {
		var n int64
		err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM records WHERE ts >= ? AND ts < ?`,
			v.StartMS, v.EndMS).Scan(&n)
		searchFailed(v, &mu, "search scanned", err)
		mu.Lock()
		v.Scanned = n
		mu.Unlock()
	})

	ids := scanMatches(ctx, db, v, &mu)
	if v.View != "patterns" && len(ids) > 0 {
		v.Results = v.lines(linesByID(ctx, db, v, &mu, ids))
	}

	// A chosen value hides the field's others, so its facet asks again
	// without it, over the same rows the pass reached.
	from := v.FromMS
	if v.Capped && v.CapMS > from {
		from = v.CapMS
	}
	for i := range v.Facets {
		f := v.Facets[i].Field
		if !hasInclude(v.terms, f) {
			continue
		}
		wg.Go(func() {
			vals := facetValues(ctx, db, v, &mu, f, from)
			mu.Lock()
			v.Facets[i].Values = vals
			mu.Unlock()
		})
	}
	wg.Wait()

	for i := range v.Facets {
		v.decorateFacet(&v.Facets[i])
	}
	v.Pages = (v.Matches + searchPerPage - 1) / searchPerPage
}

type facetCounts struct {
	source, level, status, component, path map[string]int64
}

// idSlack covers a record that arrived a little after its own timestamp,
// since the scan walks ids and only filters on ts.
const idSlack = int64(10 * 60 * 1000)

// idRange turns the window into the ids that can hold it. ids follow arrival
// and ts is clamped near arrival, so a walk over ids reads the table in order
// instead of jumping from the ts index to every row.
func idRange(ctx context.Context, db *sql.DB, start, end int64) (lo, hi int64, err error) {
	err = db.QueryRowContext(ctx, `SELECT COALESCE((SELECT id FROM records WHERE ts >= ? ORDER BY ts LIMIT 1), 0)`, start-idSlack).Scan(&lo)
	if err != nil {
		return 0, 0, err
	}
	err = db.QueryRowContext(ctx, `SELECT COALESCE((SELECT id - 1 FROM records WHERE ts >= ? ORDER BY ts LIMIT 1), (SELECT MAX(id) FROM records), 0)`, end+idSlack).Scan(&hi)
	return lo, hi, err
}

const (
	scanChunk = 50000
	// scanBudget stops the walk early on a slow disk, and the page says how
	// far back it got.
	scanBudget = 7 * time.Second
)

type scanState struct {
	fc              facetCounts
	groups          map[string]*Pattern
	durs            []float64
	ids             []int64
	read            int
	inZoom, patRows int
	offset          int
	first, last     int64
}

// scanMatches is the pass, newest first in chunks of ids. It returns the ids
// of the page of lines asked for.
func scanMatches(ctx context.Context, db *sql.DB, v *SearchView, mu *sync.Mutex) []int64 {
	lo, hi, err := idRange(ctx, db, v.StartMS, v.EndMS)
	if err != nil {
		searchFailed(v, mu, "search id range", err)
		return nil
	}
	st := &scanState{
		fc:     facetCounts{map[string]int64{}, map[string]int64{}, map[string]int64{}, map[string]int64{}, map[string]int64{}},
		groups: map[string]*Pattern{},
		offset: (v.Page - 1) * searchPerPage,
	}
	w, args := searchWhere(v.StartMS, v.EndMS, v.terms, "")
	began := time.Now()
	for top := hi; top >= lo && lo > 0; top -= scanChunk {
		bottom := max(lo, top-scanChunk+1)
		if st.read >= scanCap || time.Since(began) > scanBudget {
			v.Capped = true
			var at int64
			_ = db.QueryRowContext(ctx, `SELECT ts FROM records WHERE id > ? ORDER BY id LIMIT 1`, top).Scan(&at)
			v.CapMS = at
			break
		}
		// NOT INDEXED keeps it on the rowid range, which is the whole idea.
		rows, err := db.QueryContext(ctx, `
			SELECT id, ts, level, source, status, component, method, path, msg, duration_ms
			FROM records NOT INDEXED WHERE id BETWEEN ? AND ? AND `+w+`
			ORDER BY id DESC`, append([]any{bottom, top}, args...)...)
		if err != nil {
			searchFailed(v, mu, "search scan", err)
			break
		}
		st.chunk(rows, v)
		err = rows.Err()
		rows.Close()
		if err != nil {
			searchFailed(v, mu, "search scan rows", err)
			break
		}
	}
	fc, first, last, ids := st.fc, st.first, st.last, st.ids

	v.HasMore = len(ids) > searchPerPage
	if v.HasMore {
		ids = ids[:searchPerPage]
	}
	if v.Capped && v.CapMS > 0 {
		v.CapAt = time.UnixMilli(v.CapMS).UTC().Format(stampLayout(v.EndMS - v.StartMS))
		for i := range v.Hist {
			if v.StartMS+int64(i+1)*v.BucketMS <= v.CapMS {
				v.Hist[i].Unread = true
			}
		}
	}
	layout := stampLayout(v.EndMS - v.StartMS)
	if first > 0 {
		v.First = time.UnixMilli(first).UTC().Format(layout)
		v.Last = time.UnixMilli(last).UTC().Format(layout)
	}
	v.Sources = int64(len(fc.source))
	v.TopSrc = topOf(fc.source, 3)
	v.Latency = percentiles(st.durs)
	for _, f := range []struct {
		name string
		m    map[string]int64
	}{{"source", fc.source}, {"level", fc.level}, {"status", fc.status}, {"path", fc.path}, {"component", fc.component}} {
		facet := Facet{Name: f.name, Field: f.name}
		for _, lc := range topOf(f.m, 6) {
			facet.Values = append(facet.Values, FacetValue{Value: lc.Label, Count: lc.Count})
		}
		v.Facets = append(v.Facets, facet)
	}
	v.finishPatterns(st.groups, st.patRows)
	return ids
}

func (st *scanState) chunk(rows *sql.Rows, v *SearchView) {
	n := len(v.Hist)
	for rows.Next() {
		var id, ts, status int64
		var level, source, component, method, path, msg string
		var dur float64
		if err := rows.Scan(&id, &ts, &level, &source, &status, &component, &method, &path, &msg, &dur); err != nil {
			queryFailed("search scan row", err)
			return
		}
		st.read++
		if b := int((ts - v.StartMS) / v.BucketMS); b >= 0 && b < n {
			h := &v.Hist[b]
			switch level {
			case "ERROR":
				h.Err++
			case "WARN":
				h.Warn++
			default:
				h.Rest++
			}
			if status >= 500 {
				h.S5xx++
			} else if status >= 400 {
				h.S4xx++
			}
		}
		if ts < v.FromMS || ts >= v.ToMS {
			continue
		}

		if st.inZoom >= st.offset && st.inZoom < st.offset+searchPerPage+1 {
			st.ids = append(st.ids, id)
		}
		st.inZoom++
		if st.last == 0 || ts > st.last {
			st.last = ts
		}
		if st.first == 0 || ts < st.first {
			st.first = ts
		}
		v.Matches++
		switch level {
		case "ERROR":
			v.Errors++
		case "WARN":
			v.Warns++
		}
		fc := st.fc
		if status > 0 {
			v.Requests++
			fc.status[strconv.FormatInt(status, 10)]++
			if status >= 500 {
				v.S5xx++
			} else if status >= 400 {
				v.S4xx++
			}
		}
		fc.source[source]++
		fc.level[level]++
		if component != "" {
			fc.component[component]++
		}
		if path != "" {
			if _, ok := fc.path[path]; ok || len(fc.path) < pathKeys {
				fc.path[path]++
			}
		}
		if dur > 0 {
			st.durs = append(st.durs, dur)
		}
		if st.patRows < patternRowCap {
			st.patRows++
			addPattern(st.groups, v, ts, level, source, status, method, path, msg)
		}
	}
}

// percentiles is nearest rank, the same answer CUME_DIST gives in SQL.
func percentiles(durs []float64) Latency {
	l := Latency{Count: int64(len(durs))}
	if len(durs) == 0 {
		return l
	}
	slices.Sort(durs)
	at := func(p float64) float64 {
		i := int(p*float64(len(durs))+0.999999) - 1
		return durs[max(0, min(i, len(durs)-1))]
	}
	var sum float64
	for _, d := range durs {
		sum += d
	}
	l.Mean = sum / float64(len(durs))
	l.Max = durs[len(durs)-1]
	l.P50, l.P95, l.P99 = at(0.50), at(0.95), at(0.99)
	return l
}

func addPattern(groups map[string]*Pattern, v *SearchView, ts int64, level, source string, status int64, method, path, msg string) {
	isReq := msg == "request" && status > 0
	var shape string
	if isReq {
		shape = fmt.Sprintf("%d %s %s", status, method, maskPath(path))
	} else {
		shape = maskMessage(msg)
	}
	key := level + "\x00" + source + "\x00" + shape
	g, ok := groups[key]
	if !ok {
		if len(groups) >= patternShapes {
			return
		}
		g = &Pattern{Shape: shape, Level: level, Source: source, Spark: make([]float64, len(v.Hist)), first: ts, last: ts}
		g.filter = []term{
			{Field: "source", Values: []string{source}},
			{Field: "level", Values: []string{level}},
		}
		if isReq {
			g.filter = append(g.filter,
				term{Field: "status", Values: []string{strconv.FormatInt(status, 10)}},
				term{Field: "method", Values: []string{method}},
				term{Field: "path", Values: []string{wildcard(maskPath(path))}})
		} else {
			g.filter = append(g.filter, term{Field: "msg", Values: []string{wildcard(shape)}})
		}
		groups[key] = g
	}
	g.Count++
	g.first = min(g.first, ts)
	g.last = max(g.last, ts)
	if b := int((ts - v.StartMS) / v.BucketMS); b >= 0 && b < len(g.Spark) {
		g.Spark[b]++
	}
}

func (v *SearchView) finishPatterns(groups map[string]*Pattern, read int) {
	out := make([]Pattern, 0, len(groups))
	var total int64
	for _, g := range groups {
		total += g.Count
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Shape < out[j].Shape
	})
	if len(out) > 40 {
		out = out[:40]
	}
	n := len(v.Hist)
	fromB := int((v.FromMS - v.StartMS) / v.BucketMS)
	toB := min(n, int((v.ToMS-v.StartMS+v.BucketMS-1)/v.BucketMS))
	layout := stampLayout(v.EndMS - v.StartMS)
	for i := range out {
		p := &out[i]
		if total > 0 {
			p.Share = float64(p.Count) * 100 / float64(total)
		}
		p.First = time.UnixMilli(p.first).UTC().Format(layout)
		p.Last = time.UnixMilli(p.last).UTC().Format(layout)
		if fromB >= 0 && fromB < toB {
			p.Spark = p.Spark[fromB:toB]
		}
		p.Href = v.href(joinTerms(mergeTerms(v.terms, p.filter)), v.zoomFrom(), v.zoomTo(), "lines")
	}
	v.Patterns = out
	v.Shapes = len(groups)
	v.PatCapped = read >= patternRowCap
}

// linesByID fetches the page the pass picked, in the order it picked them.
func linesByID(ctx context.Context, db *sql.DB, v *SearchView, mu *sync.Mutex, ids []int64) []RecordRow {
	args := make([]any, len(ids))
	marks := make([]byte, 0, len(ids)*2)
	for i, id := range ids {
		args[i] = id
		if i > 0 {
			marks = append(marks, ',')
		}
		marks = append(marks, '?')
	}
	rows, err := db.QueryContext(ctx, `SELECT `+recordCols+` FROM records WHERE id IN (`+string(marks)+`)
		ORDER BY ts DESC, id DESC`, args...)
	if err != nil {
		searchFailed(v, mu, "search lines", err)
		return nil
	}
	defer rows.Close()
	out, err := scanRecords(rows, "search lines")
	searchFailed(v, mu, "search lines rows", err)
	return out
}
