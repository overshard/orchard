package main

import (
	"context"
	"os"
	"testing"
	"time"
)

// ADVISORIES_LIVE=1 runs the security panel against the real upstreams and
// prints what it would show.
func TestAdvisoriesLive(t *testing.T) {
	if os.Getenv("ADVISORIES_LIVE") == "" {
		t.Skip("set ADVISORIES_LIVE=1")
	}
	g := NewGuard(t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	a := fetchAdvisories(ctx, g, Advisories{}, time.Now())
	t.Logf("act %d, kev %d added %d kept, next patch %s", a.Act, a.KEVAdded, a.KEVKept, a.NextPatch)
	for _, r := range a.Rows {
		t.Logf("%-8s %-13s %-9s %-20s %-8s %-14s %-22s %s | %s", r.State, r.Product, r.Tag, r.ID, r.Age, r.Status, r.Note, r.Title, r.Detail)
	}
	for _, i := range fetchIncidents(ctx, g, nil) {
		t.Logf("%-14s %-10s %s", i.Name, i.State, i.Text)
	}
}
