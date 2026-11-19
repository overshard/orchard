package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"analytics.bythewood.me/ntfy"
	"github.com/google/uuid"
)

type caught struct{ sent []ntfy.Message }

func (c *caught) Publish(_ context.Context, m ntfy.Message) error {
	c.sent = append(c.sent, m)
	return nil
}

func view(t *testing.T, db *sql.DB, id uuid.UUID, at time.Time, user, url, ref string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO events (property_id, event, created_at, user_id, url, referrer) VALUES (?,?,?,?,?,?)`,
		id[:], "page_view", at.UnixMilli(), user, url, ref); err != nil {
		t.Fatal(err)
	}
}

// quietHistory gives the property about one view an hour for two weeks.
func quietHistory(t *testing.T, db *sql.DB, id uuid.UUID, now time.Time) {
	for h := 2; h < 14*24; h++ {
		view(t, db, id, now.Add(-time.Duration(h)*time.Hour), fmt.Sprint("old", h%40), "/", "google.com")
	}
}

func TestSurgeBusyProperty(t *testing.T) {
	db, id := retentionTestDB(t)
	now := time.Now()
	quietHistory(t, db, id, now)
	for i := range 30 {
		view(t, db, id, now.Add(-time.Duration(i)*time.Minute), fmt.Sprint("u", i%15), "/posts/sqlite/", "news.ycombinator.com")
	}
	push := &caught{}
	w := NewSurgeWatcher(db, push)
	w.now = func() time.Time { return now }
	w.Check(context.Background())

	if len(push.sent) != 1 {
		t.Fatalf("sent %d, want 1: %+v", len(push.sent), push.sent)
	}
	m := push.sent[0]
	for _, want := range []string{"30 views from 15 people", "/posts/sqlite/", "news.ycombinator.com"} {
		if !strings.Contains(m.Body, want) {
			t.Errorf("%q is missing %q", m.Body, want)
		}
	}
	if !strings.HasPrefix(m.Title, "Busy: ") {
		t.Errorf("title %q", m.Title)
	}

	w.Check(context.Background())
	if len(push.sent) != 1 {
		t.Error("the same surge pushed twice inside the cooldown")
	}
}

func TestSurgeIgnoresOnePersonReloading(t *testing.T) {
	db, id := retentionTestDB(t)
	now := time.Now()
	for i := range 200 {
		view(t, db, id, now.Add(-time.Duration(i)*10*time.Second), "isaac", "/", "")
	}
	push := &caught{}
	w := NewSurgeWatcher(db, push)
	w.now = func() time.Time { return now }
	w.Check(context.Background())
	if len(push.sent) != 0 {
		t.Errorf("one visitor set it off: %+v", push.sent)
	}
}

func TestSurgeNormalBusyHourIsQuiet(t *testing.T) {
	db, id := retentionTestDB(t)
	now := time.Now()
	// Two weeks at about thirty views an hour, then an ordinary hour of it.
	for h := 1; h < 14*24; h++ {
		for i := range 30 {
			view(t, db, id, now.Add(-time.Duration(h)*time.Hour-time.Duration(i)*time.Minute), fmt.Sprint("p", i), "/", "")
		}
	}
	for i := range 35 {
		view(t, db, id, now.Add(-time.Duration(i)*time.Minute), fmt.Sprint("n", i), "/", "")
	}
	push := &caught{}
	w := NewSurgeWatcher(db, push)
	w.now = func() time.Time { return now }
	w.Check(context.Background())
	if len(push.sent) != 0 {
		t.Errorf("an ordinary hour pushed: %+v", push.sent)
	}
}

func TestSurgeOutsideLink(t *testing.T) {
	db, id := retentionTestDB(t)
	now := time.Now()
	quietHistory(t, db, id, now)
	for i := range 6 {
		view(t, db, id, now.Add(-time.Duration(i)*time.Minute), fmt.Sprint("r", i), "/posts/x/", "reddit.com")
	}
	// Moving between the sites is never a link worth a push.
	for i := range 9 {
		view(t, db, id, now.Add(-time.Duration(i)*time.Minute), fmt.Sprint("s", i), "/", "blog.bythewood.me")
	}
	push := &caught{}
	w := NewSurgeWatcher(db, push)
	w.now = func() time.Time { return now }
	w.Check(context.Background())
	if len(push.sent) != 1 || !strings.Contains(push.sent[0].Title, "linked from reddit.com") || !strings.Contains(push.sent[0].Body, "/posts/x/") {
		t.Fatalf("sent %+v", push.sent)
	}
}
