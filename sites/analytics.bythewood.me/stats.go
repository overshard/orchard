package main

import (
	"context"
	"database/sql"
	"math"
	"time"
)

// buckets are the graph's columns, a day, a week or four weeks each, counted
// back from the window's last day and oldest first.
type buckets struct {
	labels []string
	day    map[string]int
	hour   map[int64]int
}

func newBuckets(endDate time.Time, rangeDays int64) *buckets {
	step, n := int64(1), rangeDays
	switch {
	case rangeDays <= 28:
	case rangeDays <= 6*28:
		step, n = 7, rangeDays/7
	default:
		step, n = 28, rangeDays/28
	}
	b := &buckets{day: map[string]int{}, hour: map[int64]int{}}
	for i := n - 1; i >= 0; i-- {
		// A bucket is labelled by its last day and counts back from it, so the
		// newest one ends on endDate instead of running into days to come.
		end := endDate.AddDate(0, 0, -int(i*step))
		idx := len(b.labels)
		b.labels = append(b.labels, formatGraphLabel(end))
		for j := range step {
			b.day[end.AddDate(0, 0, -int(j)).Format("2006-01-02")] = idx
		}
	}
	return b
}

// at is the bucket an hour since the epoch falls in, or -1. By the hour and
// folded into Eastern days here, since SQLite only knows UTC and a fixed
// offset would be an hour wrong across a DST change.
func (b *buckets) at(hour int64) int {
	if i, ok := b.hour[hour]; ok {
		return i
	}
	i, ok := b.day[time.UnixMilli(hour*3600000).In(eastern).Format("2006-01-02")]
	if !ok {
		i = -1
	}
	b.hour[hour] = i
	return i
}

func (b *buckets) zeros() []float64 { return make([]float64, len(b.labels)) }

// period is one window's headline numbers, in total and per bucket.
type period struct {
	Visitors, PageViews, Visits, Bounces, Engaged int64
	TimeMS                                        float64
	TimeN                                         int64

	visitors, pageViews, visits, bounces, engaged, timeMS, timeN []float64
}

func (p period) ViewsPerVisit() float64 { return ratio(float64(p.PageViews), float64(p.Visits)) }
func (p period) Bounce() float64        { return 100 * ratio(float64(p.Bounces), float64(p.Visits)) }
func (p period) TimeOnPage() float64    { return ratio(p.TimeMS, float64(p.TimeN)) / 1000 }
func (p period) Engagement() float64    { return 100 * ratio(float64(p.Engaged), float64(p.Visitors)) }

func ratio(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

func divide(a, b []float64, scale float64) []float64 {
	out := make([]float64, len(a))
	for i := range a {
		out[i] = math.Round(scale*ratio(a[i], b[i])*10) / 10
	}
	return out
}

// An engaged visitor has ten or more events in the window, which a single
// page view and a scroll never reaches.
const engagedEvents = 10

// readPeriod gathers the headline numbers in two queries, one row per visitor
// per hour and one row per visit, and folds them into buckets here.
func readPeriod(ctx context.Context, db querier, s scope, b *buckets) period {
	p := period{
		visitors: b.zeros(), pageViews: b.zeros(), visits: b.zeros(), bounces: b.zeros(),
		engaged: b.zeros(), timeMS: b.zeros(), timeN: b.zeros(),
	}
	with, args := s.with()
	minMS, maxMS := timeOnPageMinS*1000, timeOnPageMaxS*1000
	args = append(args, minMS, maxMS, minMS, maxMS)
	rows, err := db.QueryContext(ctx, with+`
		SELECT created_at / 3600000, user_id, SUM(event = 'page_view'), COUNT(*),
		    SUM(CASE WHEN event = 'page_leave' AND time_on_page_ms BETWEEN ? AND ? THEN time_on_page_ms ELSE 0 END),
		    SUM(event = 'page_leave' AND time_on_page_ms BETWEEN ? AND ?)
		FROM ev GROUP BY 1, 2`, args...)
	logQuery("period_hours", err)
	if err == nil {
		userEvents := map[string]int64{}
		seen := map[string]bool{}
		type bu struct {
			i    int
			user string
		}
		bucketEvents := map[bu]int64{}
		bucketSeen := map[bu]bool{}
		for rows.Next() {
			var hour, pv, n, tn int64
			var tsum float64
			var user sql.NullString
			if rows.Scan(&hour, &user, &pv, &n, &tsum, &tn) != nil {
				continue
			}
			p.PageViews += pv
			p.TimeMS += tsum
			p.TimeN += tn
			i := b.at(hour)
			if i >= 0 {
				p.pageViews[i] += float64(pv)
				p.timeMS[i] += tsum
				p.timeN[i] += float64(tn)
			}
			if !user.Valid {
				continue
			}
			userEvents[user.String] += n
			if pv > 0 {
				seen[user.String] = true
			}
			if i >= 0 {
				k := bu{i, user.String}
				bucketEvents[k] += n
				if pv > 0 {
					bucketSeen[k] = true
				}
			}
		}
		logQuery("period_hours", rows.Err())
		rows.Close()
		p.Visitors = int64(len(seen))
		for u := range seen {
			if userEvents[u] >= engagedEvents {
				p.Engaged++
			}
		}
		for k := range bucketSeen {
			p.visitors[k.i]++
			if bucketEvents[k] >= engagedEvents {
				p.engaged[k.i]++
			}
		}
	}

	with, args = s.withVisits()
	rows, err = db.QueryContext(ctx, with+` SELECT start / 3600000, pv FROM vis WHERE pv > 0`, args...)
	logQuery("period_visits", err)
	if err == nil {
		for rows.Next() {
			var hour, pv int64
			if rows.Scan(&hour, &pv) != nil {
				continue
			}
			p.Visits++
			bounce := pv == 1
			if bounce {
				p.Bounces++
			}
			if i := b.at(hour); i >= 0 {
				p.visits[i]++
				if bounce {
					p.bounces[i]++
				}
			}
		}
		logQuery("period_visits", rows.Err())
		rows.Close()
	}
	return p
}

// readConversions counts the visits that did each event, in total and per
// bucket. A visit that did it twice converted once.
func readConversions(ctx context.Context, db querier, s scope, b *buckets, events []string) (map[string]int64, map[string][]float64) {
	total := map[string]int64{}
	series := map[string][]float64{}
	if len(events) == 0 {
		return total, series
	}
	with, args := s.withVisits()
	for _, e := range events {
		args = append(args, e)
		series[e] = b.zeros()
	}
	rows, err := db.QueryContext(ctx, with+` SELECT event, MIN(created_at) / 3600000 FROM vs
		WHERE event IN (`+placeholders(len(events))+`) GROUP BY event, user_id, vn`, args...)
	logQuery("conversions", err)
	if err != nil {
		return total, series
	}
	defer rows.Close()
	for rows.Next() {
		var e string
		var hour int64
		if rows.Scan(&e, &hour) != nil {
			continue
		}
		total[e]++
		if i := b.at(hour); i >= 0 {
			series[e][i]++
		}
	}
	logQuery("conversions", rows.Err())
	return total, series
}

// readEventCounts counts each event, in total and per bucket, for pinned cards.
func readEventCounts(ctx context.Context, db querier, s scope, b *buckets, events []string) (map[string]int64, map[string][]float64) {
	total := map[string]int64{}
	series := map[string][]float64{}
	if len(events) == 0 {
		return total, series
	}
	with, args := s.with()
	for _, e := range events {
		args = append(args, e)
		series[e] = b.zeros()
	}
	rows, err := db.QueryContext(ctx, with+` SELECT event, created_at / 3600000, COUNT(*) FROM ev
		WHERE event IN (`+placeholders(len(events))+`) GROUP BY 1, 2`, args...)
	logQuery("event_counts", err)
	if err != nil {
		return total, series
	}
	defer rows.Close()
	for rows.Next() {
		var e string
		var hour, n int64
		if rows.Scan(&e, &hour, &n) != nil {
			continue
		}
		total[e] += n
		if i := b.at(hour); i >= 0 {
			series[e][i] += float64(n)
		}
	}
	logQuery("event_counts", rows.Err())
	return total, series
}

// graphStep names the bucket newBuckets picks for a window.
func graphStep(rangeDays int64) string {
	switch {
	case rangeDays <= 28:
		return "daily"
	case rangeDays <= 6*28:
		return "weekly"
	}
	return "every 4 weeks"
}
