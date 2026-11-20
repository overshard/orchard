package main

import (
	"context"
	"database/sql"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type row struct {
	event, user, url, ref, device, country string
	at                                     time.Duration
}

// twoVisitors is three visits. a lands on / from news.example.org, reads
// /pricing and signs up, then comes back two hours later to /docs. b lands on
// / directly and leaves.
func twoVisitors(t *testing.T) (*sql.DB, uuid.UUID, int64) {
	t.Helper()
	db, id := retentionTestDB(t)
	base := time.Date(2026, 11, 3, 15, 0, 0, 0, eastern)
	rows := []row{
		{"session_start", "a", "/", "news.example.org", "Mobile", "US", 0},
		{"page_view", "a", "/", "news.example.org", "Mobile", "", 0},
		{"page_view", "a", "/pricing", "shop.example.com", "Mobile", "", time.Minute},
		{"signup", "a", "/pricing", "", "Mobile", "", 2 * time.Minute},
		{"page_view", "a", "/docs", "", "Mobile", "", 2 * time.Hour},
		{"session_start", "b", "/", "", "Desktop", "DE", 0},
		{"page_view", "b", "/", "", "Desktop", "", 10 * time.Second},
	}
	for _, r := range rows {
		var country any
		if r.country != "" {
			country = r.country
		}
		if _, err := db.Exec(`INSERT INTO events (property_id, event, created_at, user_id, url, referrer, device, country)
			VALUES (?,?,?,?,?,?,?,?)`, id[:], r.event, base.Add(r.at).UnixMilli(), r.user, r.url, r.ref, r.device, country); err != nil {
			t.Fatal(err)
		}
	}
	return db, id, base.UnixMilli()
}

func scoped(t *testing.T, id uuid.UUID, base int64, query string) scope {
	t.Helper()
	q, err := url.ParseQuery(query)
	if err != nil {
		t.Fatal(err)
	}
	return newScope(id, base-dayMS, base+dayMS, parseFilters(q, id))
}

func TestFiltersNarrowEveryNumber(t *testing.T) {
	db, id, base := twoVisitors(t)
	end := time.UnixMilli(base).In(eastern)
	tests := []struct {
		query                                  string
		visitors, views, visits, bounces, conv int64
	}{
		{"", 2, 4, 3, 2, 1},
		{"f_referrer=news.example.org", 1, 2, 1, 0, 1},
		{"f_referrer=direct", 2, 2, 2, 2, 0},
		{"f_entry=%2F", 2, 3, 2, 1, 1},
		{"f_goal=signup", 1, 2, 1, 0, 1},
		{"f_country=DE", 1, 1, 1, 1, 0},
		{"f_device=Mobile", 1, 3, 2, 1, 1},
		{"filter_url=%2Fpricing", 1, 1, 1, 1, 1},
		{"f_referrer=news.example.org&f_page=%2Fpricing", 1, 1, 1, 1, 1},
		{"f_referrer=x%27+OR+1%3D1+--", 0, 0, 0, 0, 0},
		{"f_user_agent=x", 2, 4, 3, 2, 1},
	}
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for _, tt := range tests {
		// Once built in CTEs and once off the temp tables a dashboard cuts.
		for _, cut := range []bool{false, true} {
			s := scoped(t, id, base, tt.query)
			if cut {
				if err := s.materialize(ctx, conn, "t"); err != nil {
					t.Fatal(err)
				}
			}
			b := newBuckets(end, 7)
			p := readPeriod(ctx, conn, s, b)
			conv, _ := readConversions(ctx, conn, s, b, []string{"signup"})
			s.drop(ctx, conn)
			got := []int64{p.Visitors, p.PageViews, p.Visits, p.Bounces, conv["signup"]}
			want := []int64{tt.visitors, tt.views, tt.visits, tt.bounces, tt.conv}
			for i := range got {
				if got[i] != want[i] {
					t.Errorf("%q cut %v: visitors, views, visits, bounces, conversions = %v, want %v", tt.query, cut, got, want)
					break
				}
			}
		}
	}
}

func TestEntryExitAndReferrerPanels(t *testing.T) {
	db, id, base := twoVisitors(t)
	s := scoped(t, id, base, "")
	ctx := context.Background()

	entries := s.visitsBy(ctx, db, "entry", 10)
	if len(entries) != 2 || entries[0].Label != "/" || entries[0].Count != 2 || entries[0].Rate != 50 {
		t.Errorf("entry pages %+v, want / twice with half bouncing", entries)
	}
	exits := s.visitsBy(ctx, db, "exit", 10)
	labels := []string{}
	for _, e := range exits {
		labels = append(labels, e.Label)
	}
	if strings.Join(labels, " ") != "/ /docs /pricing" {
		t.Errorf("exit pages %v", labels)
	}
	refs := s.visitsBy(ctx, db, "ref", 10)
	if len(refs) != 2 || refs[0].Label != "direct" || refs[0].Count != 2 || refs[1].Label != "news.example.org" {
		t.Errorf("referrers %+v", refs)
	}
	countries, _ := s.visitorsByPlace(ctx, db)
	if countries["US"] != 1 || countries["DE"] != 1 {
		t.Errorf("countries %v", countries)
	}
}

func TestToggleAddsAndRemovesAFilter(t *testing.T) {
	q, _ := url.ParseQuery("date_range=28&filter_url=%2Fa&report=md")
	d := &Dashboard{query: q}
	if got := d.Toggle("referrer", "news.example.org"); got != "?date_range=28&f_referrer=news.example.org&filter_url=%2Fa" {
		t.Errorf("adding: %s", got)
	}
	if got := d.Toggle("page", "/a"); got != "?date_range=28&f_page=%2Fa" {
		t.Errorf("the old page filter turns into the new one: %s", got)
	}
	q, _ = url.ParseQuery("f_country=US&f_region=Ohio")
	d = &Dashboard{query: q}
	if got := d.Toggle("country", "US"); got != "?" {
		t.Errorf("removing a country drops its region: %s", got)
	}
	if got := d.Cleared(); got != "?" {
		t.Errorf("cleared: %s", got)
	}
}

func TestLongFilterIsIgnored(t *testing.T) {
	q := url.Values{"f_page": {strings.Repeat("a", maxFilterLen+1)}}
	if f := parseFilters(q, uuid.New()); len(f) != 0 {
		t.Errorf("kept %d filters", len(f))
	}
}
