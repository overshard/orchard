// The read only JSON view of traffic, for chat.bythewood.me's tools.
//
// It answers the shape of question a person actually asks about their own
// analytics, which is how many people, from where, reading what, over some
// number of days. The dashboard's own cards are richer and the report PDF is
// richer still, and neither is worth a model reading past to find three
// numbers.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type apiPropertySummary struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Public       bool         `json:"public"`
	Sessions     int64        `json:"sessions"`
	PageViews    int64        `json:"page_views"`
	LiveUsers    int64        `json:"live_users"`
	TopPages     []LabelCount `json:"top_pages"`
	TopReferrers []LabelCount `json:"top_referrers"`
	TopCountries []LabelCount `json:"top_countries"`
	TopBrowsers  []LabelCount `json:"top_browsers"`
	TopDevices   []LabelCount `json:"top_devices"`
}

func (s *site) apiSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	days := int64(7)
	if n, err := strconv.ParseInt(r.URL.Query().Get("days"), 10, 64); err == nil && n > 0 && n <= 365 {
		days = n
	}
	end := time.Now()
	endMS := end.UnixMilli()
	startMS := end.AddDate(0, 0, -int(days)).UnixMilli()

	props, err := s.apiProperties(ctx)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	// One property can be asked for by name, since a model given every
	// property spends most of the answer saying which one it is talking about.
	want := r.URL.Query().Get("property")

	out := make([]apiPropertySummary, 0, len(props))
	for _, p := range props {
		if want != "" && !strings.EqualFold(p.Name, want) {
			continue
		}
		counts := eventCounts(ctx, s.db, p.ID, startMS, endMS, "")
		sum := apiPropertySummary{
			ID: p.ID.String(), Name: p.Name, Public: p.IsPublic,
			Sessions:     counts.SessionStart,
			PageViews:    counts.PageView,
			LiveUsers:    totalLiveUsers(ctx, s.db, p.ID),
			TopPages:     pageViewsByPageURL(ctx, s.db, p.ID, startMS, endMS, "", 10),
			TopReferrers: sessionStartsByReferrer(ctx, s.db, p.ID, startMS, endMS, "", 10),
			TopBrowsers:  eventsByBrowser(ctx, s.db, p.ID, startMS, endMS, "", 6),
			TopDevices:   eventsByDevice(ctx, s.db, p.ID, startMS, endMS, "", 6),
			TopCountries: topCountries(ctx, s.db, p.ID, startMS, endMS, 10),
		}
		out = append(out, sum)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"days":       days,
		"properties": out,
	})
}

func (s *site) apiProperties(ctx context.Context) ([]*Property, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+propertyColumns+" FROM properties ORDER BY is_protected DESC, created_at ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Property
	for rows.Next() {
		p, err := scanProperty(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// topCountries flattens the map the dashboard uses into the ordered list every
// other breakdown here already returns.
func topCountries(ctx context.Context, db *sql.DB, id uuid.UUID, startMS, endMS int64, limit int) []LabelCount {
	counts := sessionStartsByCountry(ctx, db, id, startMS, endMS, "")
	out := make([]LabelCount, 0, len(counts))
	for label, n := range counts {
		out = append(out, LabelCount{Label: label, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Label < out[j].Label
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
