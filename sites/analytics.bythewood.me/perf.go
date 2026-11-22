package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// perfPayload is the data of a perf event. A metric the browser couldn't
// measure is absent, and a beacon sent after the first carries errors only.
type perfPayload struct {
	URL      string     `json:"url"`
	LCP      *float64   `json:"lcp"`
	INP      *float64   `json:"inp"`
	CLS      *float64   `json:"cls"`
	FCP      *float64   `json:"fcp"`
	TTFB     *float64   `json:"ttfb"`
	Load     *float64   `json:"load"`
	LCPEl    string     `json:"lcp_el"`
	CLSEl    string     `json:"cls_el"`
	Nav      string     `json:"nav"`
	BF       string     `json:"bf"`
	Overflow int        `json:"overflow"`
	BigImg   string     `json:"big_img"`
	Errors   []perfFail `json:"errors"`
}

type perfFail struct {
	Kind    string `json:"k"`
	Message string `json:"m"`
	Source  string `json:"s"`
}

const (
	maxPerfErrors = 10
	maxPerfField  = 300
	// A timing past ten minutes is a tab left open or a clock gone wrong, and
	// would drag a percentile on a quiet site.
	maxPerfMS = 10 * 60 * 1000
)

var perfKinds = map[string]bool{
	"js": true, "promise": true, "resource": true, "csp": true,
	"deprecation": true, "intervention": true,
}

// collectPerf files a perf event into vitals and page_errors instead of
// events, so it never shows up as a custom event card.
func (s *site) collectPerf(ctx context.Context, r *http.Request, propertyID uuid.UUID, raw json.RawMessage) {
	var p perfPayload
	if json.Unmarshal(raw, &p) != nil {
		return
	}
	ua := s.ua.Parse(clampRunes(r.Header.Get("User-Agent"), maxFieldLen))
	if ua.IsBot {
		return
	}
	now := time.Now().UnixMilli()
	url := clampRunes(p.URL, maxFieldLen)

	ms := func(v *float64) any {
		if v == nil || math.IsNaN(*v) || *v < 0 || *v > maxPerfMS {
			return nil
		}
		return int64(math.Round(*v))
	}
	var cls any
	if p.CLS != nil && *p.CLS >= 0 && *p.CLS < 100 {
		cls = *p.CLS
	}
	lcp, inp, fcp, ttfb, load := ms(p.LCP), ms(p.INP), ms(p.FCP), ms(p.TTFB), ms(p.Load)
	if lcp != nil || inp != nil || cls != nil || fcp != nil || ttfb != nil || load != nil {
		_, err := s.db.ExecContext(ctx, `INSERT INTO vitals (
		    property_id, created_at, url, device, browser, nav, lcp_ms, inp_ms, cls,
		    fcp_ms, ttfb_ms, load_ms, lcp_el, cls_el, bf_reasons, overflow, big_img
		  ) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			propertyID[:], now, nullString(url), nullString(ua.Device), nullString(ua.Browser),
			nullString(clampRunes(p.Nav, 20)), lcp, inp, cls, fcp, ttfb, load,
			nullString(clampRunes(p.LCPEl, maxPerfField)), nullString(clampRunes(p.CLSEl, maxPerfField)),
			nullString(clampRunes(p.BF, maxPerfField)), min(max(p.Overflow, 0), 1),
			nullString(clampRunes(p.BigImg, maxPerfField)))
		if err != nil {
			slog.Info("collect: insert vitals", slog.Any("err", err))
		}
	}

	for i, e := range p.Errors {
		if i >= maxPerfErrors {
			break
		}
		if !perfKinds[e.Kind] || e.Message == "" {
			continue
		}
		_, err := s.db.ExecContext(ctx, `INSERT INTO page_errors (
		    property_id, created_at, url, kind, message, source, browser
		  ) VALUES (?,?,?,?,?,?,?)`,
			propertyID[:], now, nullString(url), e.Kind, clampRunes(e.Message, maxPerfField),
			nullString(clampRunes(e.Source, maxPerfField)), nullString(ua.Browser))
		if err != nil {
			slog.Info("collect: insert page error", slog.Any("err", err))
		}
	}
}

// vitalSpec is one metric's column and the good and poor lines Google's Core
// Web Vitals use. Load has no published line, so it's never coloured.
type vitalSpec struct {
	Name, Col  string
	Good, Poor float64
	Tip        string
}

var vitalSpecs = []vitalSpec{
	{"LCP", "lcp_ms", 2500, 4000, "Largest contentful paint, when the main content showed up."},
	{"INP", "inp_ms", 200, 500, "Interaction to next paint, how long a tap or key took to show a response. Only visits with an interaction have one."},
	{"CLS", "cls", 0.1, 0.25, "Cumulative layout shift, how much the page jumped around while loading."},
	{"FCP", "fcp_ms", 1800, 3000, "First contentful paint, when anything at all was drawn."},
	{"TTFB", "ttfb_ms", 800, 1800, "Time to first byte, the network and the server together."},
	{"Load", "load_ms", 0, 0, "When the load event finished, every image, script and font in."},
}

// Vital is one p75 tile.
type Vital struct {
	Name  string
	Value string
	Class string
	Good  string
	Tip   string
	N     int
	P75   *float64
}

// PageVital is one page's LCP p75.
type PageVital struct {
	URL   string
	Value string
	Class string
	Pct   int64
	N     int
}

// PageError is one distinct error, counted over the window.
type PageError struct {
	Kind    string
	Message string
	Source  string
	Count   int64
	Pages   int64
	Last    string
	New     bool
}

// RealUsers is the performance and page health half of a dashboard.
type RealUsers struct {
	Samples     int
	Vitals      []Vital
	Slowest     []PageVital
	LCPElements []LabelCount
	CLSElements []LabelCount
	Errors      []PageError
	ErrorTotal  int64
	Overflow    []LabelCount
	BigImages   []LabelCount
	BFReasons   []LabelCount
}

// p75 is the nearest-rank percentile, so it's always a value a visitor saw.
func p75(v []float64) float64 {
	sort.Float64s(v)
	i := int(math.Ceil(0.75*float64(len(v)))) - 1
	return v[max(i, 0)]
}

func classify(spec vitalSpec, v float64) string {
	switch {
	case spec.Good == 0:
		return ""
	case v <= spec.Good:
		return "ok"
	case v <= spec.Poor:
		return "warn"
	}
	return "bad"
}

func formatVital(spec vitalSpec, v float64) string {
	switch {
	case spec.Col == "cls":
		return strconv.FormatFloat(v, 'f', 2, 64)
	case v >= 1000:
		return strconv.FormatFloat(v/1000, 'f', 1, 64) + " s"
	}
	return fmt.Sprintf("%d ms", int64(v))
}

// readRealUsers computes every p75 in Go, since a window holds one row per
// page view and that's small enough to sort.
func readRealUsers(ctx context.Context, db querier, id uuid.UUID, startMS, endMS int64, withErrors bool) RealUsers {
	var ru RealUsers
	rows, err := db.QueryContext(ctx, `SELECT url, lcp_ms, inp_ms, cls, fcp_ms, ttfb_ms, load_ms,
	    lcp_el, cls_el, bf_reasons, overflow, big_img
	  FROM vitals WHERE property_id = ? AND created_at BETWEEN ? AND ?`, id[:], startMS, endMS)
	if err != nil {
		slog.Info("real users", slog.Any("err", err))
		return ru
	}
	cols := make([][]float64, len(vitalSpecs))
	byPage := map[string][]float64{}
	lcpEls, clsEls, overflow, bigImg, bf := map[string]int64{}, map[string]int64{}, map[string]int64{}, map[string]int64{}, map[string]int64{}
	for rows.Next() {
		var url, lcpEl, clsEl, bfr, img *string
		vals := make([]*float64, len(vitalSpecs))
		var over int64
		if err := rows.Scan(&url, &vals[0], &vals[1], &vals[2], &vals[3], &vals[4], &vals[5],
			&lcpEl, &clsEl, &bfr, &over, &img); err != nil {
			continue
		}
		ru.Samples++
		for i, v := range vals {
			if v != nil {
				cols[i] = append(cols[i], *v)
			}
		}
		u := deref(url)
		if vals[0] != nil && u != "" {
			byPage[u] = append(byPage[u], *vals[0])
		}
		if lcpEl != nil {
			lcpEls[*lcpEl]++
		}
		if clsEl != nil && vals[2] != nil && *vals[2] > 0.05 {
			clsEls[*clsEl]++
		}
		if over != 0 && u != "" {
			overflow[u]++
		}
		if img != nil {
			bigImg[*img]++
		}
		if bfr != nil {
			bf[*bfr]++
		}
	}
	rows.Close()

	for i, spec := range vitalSpecs {
		v := Vital{Name: spec.Name, Tip: spec.Tip, N: len(cols[i]), Value: "none"}
		if spec.Good > 0 {
			v.Good = "good ≤ " + formatVital(spec, spec.Good)
		} else {
			v.Good = "no threshold"
		}
		if len(cols[i]) > 0 {
			p := p75(cols[i])
			v.P75 = &p
			v.Value = formatVital(spec, p)
			v.Class = classify(spec, p)
		}
		ru.Vitals = append(ru.Vitals, v)
	}

	// Three samples, or one slow visit on a quiet page tops the list.
	var worst float64
	for url, v := range byPage {
		if len(v) < 3 {
			continue
		}
		p := p75(v)
		worst = math.Max(worst, p)
		ru.Slowest = append(ru.Slowest, PageVital{URL: url, Value: formatVital(vitalSpecs[0], p),
			Class: classify(vitalSpecs[0], p), N: len(v), Pct: int64(p)})
	}
	sort.Slice(ru.Slowest, func(i, j int) bool {
		if ru.Slowest[i].Pct != ru.Slowest[j].Pct {
			return ru.Slowest[i].Pct > ru.Slowest[j].Pct
		}
		return ru.Slowest[i].URL < ru.Slowest[j].URL
	})
	if len(ru.Slowest) > 8 {
		ru.Slowest = ru.Slowest[:8]
	}
	for i := range ru.Slowest {
		ru.Slowest[i].Pct = int64(math.Round(float64(ru.Slowest[i].Pct) / math.Max(worst, 1) * 100))
	}

	ru.LCPElements = topOf(lcpEls, 5)
	ru.CLSElements = topOf(clsEls, 5)
	ru.Overflow = topOf(overflow, 5)
	ru.BigImages = topOf(bigImg, 5)
	ru.BFReasons = topOf(bf, 5)

	if withErrors {
		ru.Errors, ru.ErrorTotal = readPageErrors(ctx, db, id, startMS, endMS, 12)
	}
	return ru
}

// readPageErrors groups errors by what they said and where, newest first among
// equals. New means first seen in the last day.
func readPageErrors(ctx context.Context, db querier, id uuid.UUID, startMS, endMS int64, limit int) ([]PageError, int64) {
	rows, err := db.QueryContext(ctx, `SELECT kind, message, COALESCE(source, ''), COUNT(*),
	    COUNT(DISTINCT url), MAX(created_at),
	    (SELECT MIN(created_at) FROM page_errors f WHERE f.property_id = e.property_id
	       AND f.kind = e.kind AND f.message = e.message AND COALESCE(f.source, '') = COALESCE(e.source, ''))
	  FROM page_errors e WHERE property_id = ? AND created_at BETWEEN ? AND ?
	  GROUP BY kind, message, COALESCE(source, '')
	  ORDER BY COUNT(*) DESC, MAX(created_at) DESC`, id[:], startMS, endMS)
	if err != nil {
		slog.Info("page errors", slog.Any("err", err))
		return nil, 0
	}
	defer rows.Close()
	var out []PageError
	var total int64
	dayAgo := time.Now().Add(-24 * time.Hour).UnixMilli()
	for rows.Next() {
		var e PageError
		var last, first int64
		if err := rows.Scan(&e.Kind, &e.Message, &e.Source, &e.Count, &e.Pages, &last, &first); err != nil {
			continue
		}
		total += e.Count
		if len(out) >= limit {
			continue
		}
		e.Last = time.UnixMilli(last).In(eastern).Format("Jan 2")
		e.New = first >= dayAgo
		out = append(out, e)
	}
	return out, total
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// bridgeVitals answers status over the bridge with one site's p75s and error
// counts. Caddy refuses /bridge on the public hostname, so only a container
// on orchard-edge reaches it.
func (s *site) bridgeVitals(w http.ResponseWriter, r *http.Request) {
	host := r.URL.Query().Get("host")
	var raw []byte
	if err := s.db.QueryRowContext(r.Context(), "SELECT id FROM properties WHERE name = ?", host).Scan(&raw); err != nil {
		http.Error(w, "no such property", http.StatusNotFound)
		return
	}
	id, err := uuid.FromBytes(raw)
	if err != nil {
		http.Error(w, "bad property", http.StatusInternalServerError)
		return
	}
	now := time.Now()
	week := now.Add(-7 * 24 * time.Hour).UnixMilli()
	ru := readRealUsers(r.Context(), s.db, id, week, now.UnixMilli(), false)
	errs, weekErrs := readPageErrors(r.Context(), s.db, id, week, now.UnixMilli(), 5)
	_, dayErrs := readPageErrors(r.Context(), s.db, id, now.Add(-24*time.Hour).UnixMilli(), now.UnixMilli(), 0)

	type metric struct {
		Name  string   `json:"name"`
		P75   *float64 `json:"p75"`
		Value string   `json:"value"`
		Class string   `json:"class"`
		N     int      `json:"n"`
	}
	type topErr struct {
		Kind    string `json:"kind"`
		Message string `json:"message"`
		Source  string `json:"source"`
		Count   int64  `json:"count"`
	}
	out := struct {
		Property  string   `json:"property"`
		Dashboard string   `json:"dashboard"`
		Days      int      `json:"days"`
		Samples   int      `json:"samples"`
		Metrics   []metric `json:"metrics"`
		Errors24h int64    `json:"errors_24h"`
		Errors7d  int64    `json:"errors_7d"`
		TopErrors []topErr `json:"top_errors"`
	}{Property: host, Dashboard: baseURL + "/" + id.String(), Days: 7, Samples: ru.Samples,
		Errors24h: dayErrs, Errors7d: weekErrs, Metrics: []metric{}, TopErrors: []topErr{}}
	for _, v := range ru.Vitals {
		out.Metrics = append(out.Metrics, metric{v.Name, v.P75, v.Value, v.Class, v.N})
	}
	for _, e := range errs {
		out.TopErrors = append(out.TopErrors, topErr{e.Kind, e.Message, e.Source, e.Count})
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(out)
}
