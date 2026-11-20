package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"logging.bythewood.me/suite"
)

// HistSeg is one coloured block of a histogram bar, in viewBox units.
type HistSeg struct {
	Y, H  float64
	Class string
}

type HistBar struct {
	X, W   float64
	Segs   []HistSeg
	Href   string
	Tip    string
	In     bool
	Unread bool
}

const histH = 100.0

// HistBars lays the histogram out for the template, which has no arithmetic.
func (v *SearchView) HistBars() []HistBar {
	top := float64(v.HistMax())
	out := make([]HistBar, len(v.Hist))
	for i, b := range v.Hist {
		bar := HistBar{X: float64(i) * 10, W: 10, Href: b.Href, Tip: b.Tip, In: b.In, Unread: b.Unread}
		y := histH
		for _, s := range []struct {
			n   int64
			cls string
		}{{b.Err, "e"}, {b.Warn, "w"}, {b.Rest, "i"}} {
			if s.n == 0 {
				continue
			}
			h := float64(s.n) / top * (histH - 4)
			if h < 1.2 {
				h = 1.2
			}
			y -= h
			bar.Segs = append(bar.Segs, HistSeg{Y: y, H: h, Class: s.cls})
		}
		out[i] = bar
	}
	return out
}

func (v *SearchView) HistWidth() int { return len(v.Hist) * 10 }

func (s *site) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	v := newSearchView(q, time.Now().UTC())

	if q.Get("report") == "md" {
		ctx, cancel := context.WithTimeout(r.Context(), searchBudget)
		defer cancel()
		rows := recentSearchRows(ctx, s.db, v)
		v.HasMore = len(rows) > searchPerPage
		if v.HasMore {
			rows = rows[:searchPerPage]
		}
		for _, row := range rows {
			v.Results = append(v.Results, Line{RecordRow: row})
		}
		writeSearchMarkdown(w, v)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), searchBudget)
	defer cancel()

	if id, err := strconv.ParseInt(q.Get("context"), 10, 64); err == nil && id > 0 {
		target, rows := contextRows(ctx, s.db, id)
		if target != nil {
			v.Context = target
			v.CtxBack = v.href(v.Q, v.zoomFrom(), v.zoomTo(), v.View)
			v.Results = v.lines(rows)
			for i := range v.Results {
				v.Results[i].Target = v.Results[i].ID == id
			}
		}
	} else {
		select {
		case searchSlots <- struct{}{}:
			runSearch(ctx, s.db, v)
			<-searchSlots
		case <-ctx.Done():
			v.Slow = true
		}
	}
	v.finish()

	data := s.page(r, "Search", "Search every raw log line inside the retention window.")
	data.PageScript = s.dashScript
	data.PageStyles = s.dashStyles
	data.Search = v
	data.Suite.Status = searchStatus(v)
	s.renderer.Render(w, http.StatusOK, "search.html", data)
}

func recentSearchRows(ctx context.Context, db *sql.DB, v *SearchView) []RecordRow {
	var mu sync.Mutex
	return searchLines(ctx, db, v, &mu, searchPerPage+1, (v.Page-1)*searchPerPage)
}

// newSearchView reads the window, the query and the mode out of the URL. The
// old form fields fold into the query, so every state has one canonical URL.
func newSearchView(q url.Values, now time.Time) *SearchView {
	key, name, start, end := resolveWindow(q)
	if minStart := hourFloor(now.Add(-rawRetention).UnixMilli()); start < minStart {
		start = minStart
		if key != "custom" {
			key, name = "30d", "Last 30 days"
		}
	}
	terms := append(legacyTerms(q), parseQuery(q.Get("q"))...)
	v := &SearchView{
		Q:         joinTerms(terms),
		terms:     terms,
		RangeKey:  key,
		RangeName: name,
		StartMS:   start,
		EndMS:     end,
		FromMS:    start,
		ToMS:      end,
		BucketMS:  bucketFor(end - start),
		View:      "lines",
		PerPage:   searchPerPage,
		Retained:  int(rawRetention / (24 * time.Hour)),
	}
	if q.Get("view") == "patterns" {
		v.View = "patterns"
	}
	v.Page, _ = strconv.Atoi(q.Get("page"))
	v.Page = max(1, min(v.Page, maxSearchPage))

	// A zoom snaps out to whole buckets, so the bars and the counts agree.
	from, errF := strconv.ParseInt(q.Get("from"), 10, 64)
	to, errT := strconv.ParseInt(q.Get("to"), 10, 64)
	if errF == nil && errT == nil && from < to {
		from = max(from, start)
		to = min(to, end)
		from = start + (from-start)/v.BucketMS*v.BucketMS
		to = min(end, start+(to-start+v.BucketMS-1)/v.BucketMS*v.BucketMS)
		if from < to && (from > start || to < end) {
			v.FromMS, v.ToMS, v.Zoomed = from, to, true
		}
	}

	n := int((end - start + v.BucketMS - 1) / v.BucketMS)
	v.Hist = make([]HistBucket, max(n, 1))
	layout := "15:04"
	if end-start > 25*60*60*1000 {
		layout = "Jan 2 15:04"
	}
	for i := range v.Hist {
		at := start + int64(i)*v.BucketMS
		v.Hist[i].Label = time.UnixMilli(at).UTC().Format(layout)
		v.Hist[i].In = at >= v.FromMS && at < v.ToMS
		v.Hist[i].Href = v.href(v.Q, at, min(end, at+v.BucketMS), v.View)
	}
	return v
}

// finish fills the links and labels once the counts are in.
func (v *SearchView) finish() {
	for _, k := range searchRanges {
		rv := *v
		rv.RangeKey = k
		v.Ranges = append(v.Ranges, RangeLink{Key: k, On: k == v.RangeKey, Href: rv.href(v.Q, 0, 0, v.View)})
	}
	for i, t := range v.terms {
		v.Chips = append(v.Chips, Chip{Label: t.String(), Neg: t.Neg, Href: v.withTerms(without(v.terms, i))})
	}
	v.UnzoomHref = v.href(v.Q, 0, 0, v.View)
	if v.Zoomed {
		layout := stampLayout(v.EndMS - v.StartMS)
		v.Chips = append(v.Chips, Chip{
			Label: "time " + time.UnixMilli(v.FromMS).UTC().Format(layout) + " to " + time.UnixMilli(v.ToMS).UTC().Format(layout),
			Href:  v.UnzoomHref,
		})
	}
	v.ClearHref = v.href("", 0, 0, v.View)
	v.LinesHref = v.href(v.Q, v.zoomFrom(), v.zoomTo(), "lines")
	v.PatternsHref = v.href(v.Q, v.zoomFrom(), v.zoomTo(), "patterns")
	v.BaseHref = v.href(v.Q, 0, 0, v.View)
	page := v.href(v.Q, v.zoomFrom(), v.zoomTo(), v.View) + "&page="
	v.PrevHref = page + strconv.Itoa(v.Page-1)
	v.NextHref = page + strconv.Itoa(v.Page+1)
	v.MdHref = v.href(v.Q, v.zoomFrom(), v.zoomTo(), "") + "&report=md"
	v.TailHref = "/search/tail?" + url.Values{"q": {v.Q}}.Encode()

	for i := range v.Hist {
		b := &v.Hist[i]
		end := b.Label
		if i+1 < len(v.Hist) {
			end = v.Hist[i+1].Label
		} else {
			end = "now"
		}
		b.Tip = fmt.Sprintf("%s to %s\n%s lines", b.Label, end, formatNum(b.Total()))
		if b.Err > 0 {
			b.Tip += fmt.Sprintf("\n%s errors", formatNum(b.Err))
		}
		if b.Warn > 0 {
			b.Tip += fmt.Sprintf("\n%s warns", formatNum(b.Warn))
		}
		if b.Total() > v.Peak.Total() {
			v.Peak = *b
		}
	}
	if n := len(v.Hist); n > 1 {
		for _, i := range []int{0, n / 4, n / 2, 3 * n / 4} {
			v.HistTicks = append(v.HistTicks, v.Hist[i].Label)
		}
		v.HistTicks = append(v.HistTicks, "now")
	}
}

// BucketName is the bar width in words.
func (v *SearchView) BucketName() string {
	d := time.Duration(v.BucketMS) * time.Millisecond
	if d >= time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}

func searchStatus(v *SearchView) []suite.Seg {
	out := []suite.Seg{{Text: v.RangeName}, {Text: formatNum(v.Matches) + " matching"}}
	if v.Errors > 0 {
		out = append(out, suite.Seg{Text: formatNum(v.Errors) + " errors", Kind: "bad"})
	}
	if v.Slow {
		out = append(out, suite.Seg{Text: "some panels ran out of time", Kind: "warn"})
	}
	return out
}

// searchHeadline is the page title, written from the counts.
func searchHeadline(v *SearchView) template.HTML {
	if v.Context != nil {
		return template.HTML(fmt.Sprintf(`<em>%d lines</em> either side, from %s.`, contextLines, template.HTMLEscapeString(v.Context.Source)))
	}
	if v.Matches == 0 {
		return template.HTML(`<em>Nothing</em> matches.`)
	}
	unit := "lines"
	if v.Matches == 1 {
		unit = "line"
	}
	verb := "match"
	if v.Matches == 1 {
		verb = "matches"
	}
	s := fmt.Sprintf(`<em>%s %s</em> %s`, formatNum(v.Matches), unit, verb)
	if v.Errors > 0 {
		s += fmt.Sprintf(`, <em class="bad">%s %s</em>`, formatNum(v.Errors), map[bool]string{true: "error", false: "errors"}[v.Errors == 1])
	}
	if len(v.TopSrc) > 0 && v.Sources > 1 {
		s += ", most from " + template.HTMLEscapeString(v.TopSrc[0].Label)
	}
	return template.HTML(s + ".")
}

// tail answers a live tail's poll with the matching lines newer than after.
// A short poll instead of a held stream, so nothing here outlives a request.
func (s *site) tail(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	q := r.URL.Query()
	terms := parseQuery(q.Get("q"))
	after, _ := strconv.ParseInt(q.Get("after"), 10, 64)

	var maxID int64
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) FROM records`).Scan(&maxID); err != nil {
		queryFailed("tail max id", err)
		http.Error(w, "tail failed", http.StatusServiceUnavailable)
		return
	}

	cl, args := termClauses(terms, "")
	where := strings.Join(append([]string{"id > ?", "id <= ?"}, cl...), " AND ")
	first := after <= 0
	if first || after < maxID-tailLookback {
		after = max(0, maxID-tailLookback)
	}
	order, limit := "id", tailBatch
	if first {
		order, limit = "id DESC", tailFirstLines
	}
	// NOT INDEXED keeps the planner on the rowid range, since an index on a
	// filtered column would walk every row that column has ever held.
	rows, err := s.db.QueryContext(ctx, `SELECT `+recordCols+` FROM records NOT INDEXED
		WHERE `+where+` ORDER BY `+order+` LIMIT ?`, append(append([]any{after, maxID}, args...), limit)...)
	if err != nil {
		if !quiet(err) {
			queryFailed("tail", err)
		}
		http.Error(w, "tail failed", http.StatusServiceUnavailable)
		return
	}
	recs, err := scanRecords(rows, "tail")
	rows.Close()
	if err != nil && !quiet(err) {
		queryFailed("tail rows", err)
	}
	if first {
		for i, j := 0, len(recs)-1; i < j; i, j = i+1, j-1 {
			recs[i], recs[j] = recs[j], recs[i]
		}
	}

	next := maxID
	if !first && len(recs) == limit {
		next = recs[len(recs)-1].ID
	}

	v := &SearchView{Q: joinTerms(terms), terms: terms, RangeKey: "1h", View: "lines"}
	var buf bytes.Buffer
	for _, l := range v.lines(recs) {
		if err := s.lineTmpl.ExecuteTemplate(&buf, "logLine", l); err != nil {
			queryFailed("tail render", err)
			break
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"after": next, "n": len(recs), "html": buf.String()})
}

// parseLineTemplate is the line partial on its own, for the tail's fragments.
func parseLineTemplate(templates fs.FS) (*template.Template, error) {
	return template.New("lines").Funcs(suite.FuncsWith(templateFuncs)).ParseFS(suite.With(templates), "partials.html", "suite.html")
}
