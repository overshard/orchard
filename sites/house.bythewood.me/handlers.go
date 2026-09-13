package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// PageData is the half of the template data every page needs. There is no public
// half of this site, so it carries no social card and no canonical: nothing here
// is ever indexed or shared.
type PageData struct {
	Title       string
	Description string
	Path        string
	Year        int
	SiteName    string
	SourceURL   string
	Staging     bool
	Script      string
	Styles      []string

	Config   Config
	Counts   Counts
	Counties []string
	LastRun  Run
	HaveRun  bool
	Guards   []GuardStatus
	Error    string
}

type gridPage struct {
	PageData
	Cards   []Card
	Query   Query
	View    string
	Sorts   []option
	Views   []viewTab
	Summary gridSummary
}

type option struct {
	Value string
	Label string
	On    bool
}

type viewTab struct {
	Key   string
	Label string
	Count int
	On    bool
}

// gridSummary is the one line above the grid. Four numbers, because a count on
// its own does not say whether today is worth a phone call.
type gridSummary struct {
	Shown       int
	BestScore   float64
	UnderTarget int
	NewToday    int
}

type detailPage struct {
	PageData
	Card    Card
	DPA     Monthly
	Base    Monthly
	Groups  []factorGroup
	Running bool
	Missing []SourceState
}

// factorGroup is the band a set of factors renders under, so the report says
// whose life the rows below describe rather than presenting thirteen equal lines.
type factorGroup struct {
	Key     string
	Label   string
	Note    string
	Factors []FactorScore
	Points  float64
	Weight  float64
}

// Share is the band's points as a fraction of what it could have scored, which is
// what makes five bands of different weights comparable at a glance.
func (g factorGroup) Share() float64 {
	if g.Weight <= 0 {
		return 0
	}
	return g.Points / g.Weight
}

// Unmeasured reports that nothing in this band could be looked up, so its bar is
// not a zero. A band sitting empty next to four full ones reads as the worst
// possible result rather than as no result.
func (g factorGroup) Unmeasured() bool {
	for _, f := range g.Factors {
		if !f.Unknown {
			return false
		}
	}
	return len(g.Factors) > 0
}

func groupFactors(cfg Config, b Breakdown) []factorGroup {
	byKey := map[string]*factorGroup{}
	var out []factorGroup

	for _, g := range forOrder {
		out = append(out, factorGroup{Key: g, Label: cfg.GroupLabel(g), Note: cfg.GroupNote(g)})
	}
	for i := range out {
		byKey[out[i].Key] = &out[i]
	}

	for _, f := range b.Factors {
		g, ok := byKey[f.Group]
		if !ok {
			continue
		}
		g.Factors = append(g.Factors, f)
		g.Points += f.Points
		g.Weight += f.Weight
	}

	// A band with nothing in it is a heading over a gap.
	var kept []factorGroup
	for _, g := range out {
		if len(g.Factors) > 0 {
			kept = append(kept, g)
		}
	}
	return kept
}

func (s *site) page(r *http.Request, title, description string) PageData {
	ctx := r.Context()
	counts, err := ViewCounts(ctx, s.db)
	if err != nil {
		slog.Error("counts failed", slog.Any("err", err))
	}
	counties, err := Counties(ctx, s.db)
	if err != nil {
		slog.Error("counties failed", slog.Any("err", err))
	}
	run, haveRun := LastRun(ctx, s.db)
	guards, err := guardStatuses(s.db)
	if err != nil {
		slog.Error("guard status failed", slog.Any("err", err))
	}

	return PageData{
		Title:       title,
		Description: description,
		Path:        r.URL.Path,
		Year:        time.Now().Year(),
		SiteName:    siteName,
		SourceURL:   sourceURL,
		Staging:     Staging,
		// Read per render rather than captured at boot, so a dev rebuild that
		// renames the bundle reaches the next page load.
		Script:   s.assets.Script(appEntry),
		Styles:   s.assets.Styles(appEntry),
		Config:   s.cfg,
		Counts:   counts,
		Counties: counties,
		LastRun:  run,
		HaveRun:  haveRun,
		Guards:   guards,
	}
}

var sortOptions = []option{
	{Value: "score", Label: "Best score"},
	{Value: "monthly", Label: "Cheapest monthly"},
	{Value: "detour", Label: "Shortest detour"},
	{Value: "price", Label: "Lowest price"},
	{Value: "acres", Label: "Most land"},
	{Value: "newest", Label: "Newest"},
	{Value: "oldest", Label: "Longest listed"},
}

// grid is the dashboard. New is the landing view rather than the best scoring
// one, because being first to a listing is the thing a dashboard can do that a
// phone call to an agent cannot.
func (s *site) grid(w http.ResponseWriter, r *http.Request) {
	q := Query{
		View:     firstOf(r.URL.Query().Get("view"), "new"),
		Sort:     r.URL.Query().Get("sort"),
		MinBeds:  num(r.URL.Query().Get("beds")),
		MinAcres: num(r.URL.Query().Get("acres")),
		MaxPrice: int(num(r.URL.Query().Get("max"))),
		County:   r.URL.Query().Get("county"),
		Rated:    r.URL.Query().Get("rated"),
	}

	cards, err := Cards(r.Context(), s.db, q)
	if err != nil {
		slog.Error("grid query failed", slog.Any("err", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	data := s.page(r, viewTitle(q.View), "House search")
	data.Error = r.URL.Query().Get("error")
	page := gridPage{
		PageData: data,
		Cards:    cards,
		Query:    q,
		View:     q.View,
		Summary:  summarize(cards, s.cfg),
	}
	for _, o := range sortOptions {
		o.On = o.Value == q.Sort
		page.Sorts = append(page.Sorts, o)
	}
	page.Views = []viewTab{
		{"new", "New", data.Counts.New, q.View == "new"},
		{"grid", "All", data.Counts.Grid, q.View == "grid"},
		{"drops", "Price cuts", data.Counts.Drops, q.View == "drops"},
		{"stretch", "Stretch", data.Counts.Stretch, q.View == "stretch"},
		{"filtered", "Filtered out", data.Counts.Filtered, q.View == "filtered"},
	}

	s.renderer.Render(w, http.StatusOK, "grid.html", page)
}

func summarize(cards []Card, cfg Config) gridSummary {
	var g gridSummary
	g.Shown = len(cards)
	today := time.Now().Add(-24 * time.Hour).Unix()
	for _, c := range cards {
		if c.Score > g.BestScore {
			g.BestScore = c.Score
		}
		if c.MonthlyTotal > 0 && c.MonthlyTotal <= cfg.Money.MonthlyTarget {
			g.UnderTarget++
		}
		if c.FirstSeen > today {
			g.NewToday++
		}
	}
	return g
}

func viewTitle(view string) string {
	switch view {
	case "new":
		return "New this week"
	case "drops":
		return "Price cuts"
	case "stretch":
		return "Stretch zone"
	case "filtered":
		return "Filtered out"
	default:
		return "Every listing"
	}
}

func (s *site) detail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}

	card, err := OneCard(r.Context(), s.db, id)
	if err == sql.ErrNoRows {
		s.notFound(w, r)
		return
	}
	if err != nil {
		slog.Error("detail query failed", slog.Any("err", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	s.nameDrives(card.Drives)

	page := detailPage{
		PageData: s.page(r, card.Address, "Listing detail"),
		Card:     card,
		// The same inputs the card's figure was computed from, or the tile and the
		// table below it disagree by whatever the listing's own tax bill differs
		// from the county rate.
		Base:    s.cfg.Money.Estimate(card.Price, card.TaxAnnual, card.HOAMonthly, card.County, card.City, false),
		DPA:     s.cfg.Money.Estimate(card.Price, card.TaxAnnual, card.HOAMonthly, card.County, card.City, true),
		Groups:  groupFactors(s.cfg, card.Breakdown),
		Running: s.refresher.Running(id),
	}
	// A run can finish with a source that never answered, and saying so beats a
	// report that quietly scores a missing fact as a middling one.
	if !page.Running {
		if states, err := Progress(r.Context(), s.db, card.Lat, card.Lon, 0); err == nil {
			for _, st := range states {
				if !st.Done {
					page.Missing = append(page.Missing, st)
				}
			}
		}
	}
	s.renderer.Render(w, http.StatusOK, "detail.html", page)
}

// check is the address box. It runs the whole assessment inline rather than in the
// background the way a refresh does, because one address is a handful of guarded
// calls and the reader is standing there waiting for the answer.
func (s *site) check(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	addr := strings.TrimSpace(r.FormValue("address"))
	if addr == "" {
		http.Redirect(w, r, "/?error=an+address+is+needed", http.StatusSeeOther)
		return
	}

	extra := Listing{
		Beds:      num(r.FormValue("beds")),
		Baths:     num(r.FormValue("baths")),
		SqFt:      int(num(r.FormValue("sqft"))),
		Acres:     num(r.FormValue("acres")),
		YearBuilt: int(num(r.FormValue("year"))),
		Style:     strings.TrimSpace(r.FormValue("style")),
		URL:       strings.TrimSpace(r.FormValue("url")),
		Remarks:   strings.TrimSpace(r.FormValue("remarks")),
	}

	// Two minutes is past a slow run of the guarded lookups and short of a
	// browser giving up on the request.
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	id, err := s.refresher.CheckAddress(ctx, addr, int(num(r.FormValue("price"))), extra)
	if err != nil {
		slog.Error("checking an address failed", slog.String("address", addr), slog.Any("err", err))
		http.Redirect(w, r, "/?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/listing/%d", id), http.StatusSeeOther)
}

// verdict records a thumbs up or down and a note. It answers JSON because the
// card does it without leaving the grid, which matters on a phone.
func (s *site) verdict(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	rating := int(num(r.FormValue("rating")))
	if rating < -1 {
		rating = -1
	}
	if rating > 1 {
		rating = 1
	}

	if err := SetVerdict(r.Context(), s.db, id, rating); err != nil {
		slog.Error("verdict write failed", slog.Any("err", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "rating": rating})
}

// remove deletes a listing outright. A thumbs down hides a house and keeps what
// was learned about it, which is the right default, but a house checked by
// mistake or a typo that geocoded somewhere odd should be able to leave without
// a trace. Every table keyed to the listing cascades, and the cached photo files
// are not in the database so they are swept separately.
func (s *site) remove(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}

	s.photos.Forget(r.Context(), id)

	res, err := s.db.ExecContext(r.Context(), `DELETE FROM listings WHERE id = ?`, id)
	if err != nil {
		slog.Error("deleting a listing failed", slog.Int64("id", id), slog.Any("err", err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}
	slog.Info("listing deleted", slog.Int64("id", id))

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(`{"ok":true,"deleted":true}`))
}

// status is what the report polls while the background assessment runs. It says
// which facts have landed, who each one is being asked for, and which were
// already on disk, because a spinner that says nothing for a minute reads as
// broken.
func (s *site) status(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}

	var computed, firstSeen int64
	var lat, lon float64
	_ = s.db.QueryRowContext(r.Context(), `
		SELECT COALESCE(f.computed_at,0), l.first_seen, COALESCE(l.lat,0), COALESCE(l.lon,0)
		FROM listings l LEFT JOIN facts f ON f.listing_id = l.id WHERE l.id = ?`,
		id).Scan(&computed, &firstSeen, &lat, &lon)

	states, err := Progress(r.Context(), s.db, lat, lon, firstSeen)
	if err != nil {
		slog.Error("progress failed", slog.Any("err", err))
	}
	done, cached, total, eta := SummariseProgress(states)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		// Two different questions. A facts row exists is not the same as the run
		// has finished, and treating them as one is what made the page reload
		// halfway through and then sit there looking finished.
		"running": s.refresher.Running(id),
		"ready":   computed > 0,
		"done":    done,
		"cached":  cached,
		"total":   total,
		"seconds": int(eta.Seconds()),
		"sources": states,
	})
}

func (s *site) photo(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	idx, err := strconv.Atoi(r.PathValue("idx"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.photos.Serve(w, r, id, idx)
}

// nameDrives turns a configured destination's key into its name. The drives table
// carries the key, because a name in config can change and a row written last
// week should not be orphaned by an edit.
func (s *site) nameDrives(rows []DriveRow) {
	for i, row := range rows {
		if row.Name != "" {
			continue
		}
		for _, p := range s.cfg.Destinations {
			if p.Key == row.Key {
				rows[i].Name = p.Name
				rows[i].Who = p.Who
				break
			}
		}
	}
}

func (s *site) notFound(w http.ResponseWriter, r *http.Request) {
	data := s.page(r, "404", "That page does not exist.")
	s.renderer.Render(w, http.StatusNotFound, "notfound.html", data)
}

func firstOf(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
