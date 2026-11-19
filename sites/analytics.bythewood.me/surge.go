package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"analytics.bythewood.me/ntfy"
	"github.com/google/uuid"
)

// The surge watcher reads the human events table only, so crawlers never count,
// and every rule needs several distinct visitors so one person reloading a page
// can't set it off.
const (
	surgeTopic    = "alerts"
	surgeEvery    = 10 * time.Minute
	surgeWindow   = time.Hour
	surgeBaseline = 14 * 24 * time.Hour

	// A property is busy at this many people in the window and this multiple of
	// its average hour, with the views floor keeping a quiet site's first good
	// hour from counting as five times nothing.
	busyPeople   = 10
	busyViews    = 20
	busyMultiple = 5.0
	busyCooldown = 6 * time.Hour

	// One outside site sending this many people in the window is a link worth
	// knowing about.
	linkPeople   = 5
	linkCooldown = 24 * time.Hour
)

// ownDomains are referrers that are only people moving between these sites.
var ownDomains = []string{"bythewood.me", "isaacbythewood.com"}

type Publisher interface {
	Publish(ctx context.Context, m ntfy.Message) error
}

type SurgeWatcher struct {
	db   *sql.DB
	push Publisher
	now  func() time.Time

	mu   sync.Mutex
	sent map[string]time.Time
}

func NewSurgeWatcher(db *sql.DB, push Publisher) *SurgeWatcher {
	return &SurgeWatcher{db: db, push: push, now: time.Now, sent: map[string]time.Time{}}
}

func (w *SurgeWatcher) Run(ctx context.Context) {
	t := time.NewTicker(surgeEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.Check(ctx)
		}
	}
}

type busyProperty struct {
	id     uuid.UUID
	name   string
	views  int
	people int
	normal float64
}

// Check looks at the last hour once and pushes whatever stands out.
func (w *SurgeWatcher) Check(ctx context.Context) {
	now := w.now()
	since, until := now.Add(-surgeWindow).UnixMilli(), now.UnixMilli()+1

	busy, err := w.busy(ctx, now, since, until)
	if err != nil {
		slog.Error("surge check", slog.String("component", "surge"), slog.Any("err", err))
		return
	}
	flagged := map[uuid.UUID]bool{}
	for _, b := range busy {
		flagged[b.id] = true
		if !w.claim("busy:"+b.id.String(), busyCooldown) {
			continue
		}
		page, pageN := w.top(ctx, b.id, since, until, "url")
		ref, refN := w.top(ctx, b.id, since, until, "referrer")
		body := fmt.Sprintf("%d views from %d people in the last hour, %s a normal hour.", b.views, b.people, multiple(b.views, b.normal))
		if page != "" {
			body += fmt.Sprintf(" Busiest page %s (%d).", page, pageN)
		}
		if ref != "" && !own(ref) {
			body += fmt.Sprintf(" Most came from %s (%d).", ref, refN)
		}
		w.publish(ctx, ntfy.Message{Title: "Busy: " + b.name, Body: body, Priority: "default",
			Tags: "chart_with_upwards_trend", Link: dashboardURL(b.id), Label: "Analytics"})
	}

	links, err := w.links(ctx, since, until)
	if err != nil {
		slog.Error("surge check", slog.String("component", "surge"), slog.Any("err", err))
		return
	}
	for _, l := range links {
		// A busy property's alert already names its top referrer.
		if flagged[l.id] || !w.claim("link:"+l.id.String()+":"+l.referrer, linkCooldown) {
			continue
		}
		body := fmt.Sprintf("%d people came from %s in the last hour.", l.people, l.referrer)
		if l.page != "" {
			body += " Most landed on " + l.page + "."
		}
		w.publish(ctx, ntfy.Message{Title: l.name + " linked from " + l.referrer, Body: body, Priority: "default",
			Tags: "link", Link: dashboardURL(l.id), Label: "Analytics"})
	}
}

func (w *SurgeWatcher) busy(ctx context.Context, now time.Time, since, until int64) ([]busyProperty, error) {
	rows, err := w.db.QueryContext(ctx, `
		SELECT p.id, p.name,
		    SUM(CASE WHEN e.created_at >= ? THEN 1 ELSE 0 END),
		    COUNT(DISTINCT CASE WHEN e.created_at >= ? THEN e.user_id END),
		    SUM(CASE WHEN e.created_at < ? THEN 1 ELSE 0 END)
		FROM events e JOIN properties p ON p.id = e.property_id
		WHERE e.event = 'page_view' AND e.created_at >= ? AND e.created_at < ?
		GROUP BY p.id`,
		since, since, since, now.Add(-surgeWindow-surgeBaseline).UnixMilli(), until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hours := surgeBaseline.Hours()
	var out []busyProperty
	for rows.Next() {
		var b busyProperty
		var id []byte
		var base int
		if err := rows.Scan(&id, &b.name, &b.views, &b.people, &base); err != nil {
			return nil, err
		}
		b.id, _ = uuid.FromBytes(id)
		b.normal = float64(base) / hours
		if b.people >= busyPeople && b.views >= busyViews && float64(b.views) >= busyMultiple*b.normal {
			out = append(out, b)
		}
	}
	return out, rows.Err()
}

type outsideLink struct {
	id       uuid.UUID
	name     string
	referrer string
	people   int
	page     string
}

func (w *SurgeWatcher) links(ctx context.Context, since, until int64) ([]outsideLink, error) {
	rows, err := w.db.QueryContext(ctx, `
		SELECT p.id, p.name, e.referrer, COUNT(DISTINCT e.user_id) AS people
		FROM events e JOIN properties p ON p.id = e.property_id
		WHERE e.event = 'page_view' AND e.created_at >= ? AND e.created_at < ? AND COALESCE(e.referrer, '') <> ''
		GROUP BY p.id, e.referrer
		HAVING people >= ?`, since, until, linkPeople)
	if err != nil {
		return nil, err
	}
	var out []outsideLink
	for rows.Next() {
		var l outsideLink
		var id []byte
		if err := rows.Scan(&id, &l.name, &l.referrer, &l.people); err != nil {
			rows.Close()
			return nil, err
		}
		l.id, _ = uuid.FromBytes(id)
		if !own(l.referrer) {
			out = append(out, l)
		}
	}
	rows.Close()
	for i := range out {
		_ = w.db.QueryRowContext(ctx, `
			SELECT url FROM events WHERE property_id = ? AND event = 'page_view' AND created_at >= ? AND created_at < ? AND referrer = ?
			GROUP BY url ORDER BY COUNT(*) DESC LIMIT 1`, out[i].id[:], since, until, out[i].referrer).Scan(&out[i].page)
	}
	return out, nil
}

// top is the commonest value of col in the window, for a page or a referrer.
func (w *SurgeWatcher) top(ctx context.Context, id uuid.UUID, since, until int64, col string) (string, int) {
	var v sql.NullString
	var n int
	_ = w.db.QueryRowContext(ctx, `
		SELECT `+col+`, COUNT(*) FROM events
		WHERE property_id = ? AND event = 'page_view' AND created_at >= ? AND created_at < ? AND COALESCE(`+col+`, '') <> ''
		GROUP BY 1 ORDER BY 2 DESC LIMIT 1`, id[:], since, until).Scan(&v, &n)
	return v.String, n
}

// claim is false while key is cooling down, so a long surge pushes once. It's
// in memory, so a deploy in the middle of one can push it a second time.
func (w *SurgeWatcher) claim(key string, cooldown time.Duration) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now()
	if last, ok := w.sent[key]; ok && now.Sub(last) < cooldown {
		return false
	}
	w.sent[key] = now
	return true
}

func (w *SurgeWatcher) publish(ctx context.Context, m ntfy.Message) {
	if err := w.push.Publish(ctx, m); err != nil {
		slog.Error("surge alert failed", slog.String("component", "surge"), slog.String("title", m.Title), slog.Any("err", err))
		return
	}
	slog.Info("surge alert", slog.String("component", "surge"), slog.String("title", m.Title))
}

func own(referrer string) bool {
	for _, d := range ownDomains {
		if referrer == d || strings.HasSuffix(referrer, "."+d) {
			return true
		}
	}
	return false
}

func multiple(views int, normal float64) string {
	if normal < 0.5 {
		return "against almost nothing in"
	}
	return fmt.Sprintf("about %.0fx", math.Round(float64(views)/normal))
}

func dashboardURL(id uuid.UUID) string {
	return "https://analytics.bythewood.me/" + id.String()
}
