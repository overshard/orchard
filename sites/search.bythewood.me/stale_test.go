package main

import (
	"testing"
	"time"
)

func TestStaleFutures(t *testing.T) {
	now := time.Date(2026, time.September, 5, 12, 0, 0, 0, time.UTC)

	flagged := []string{
		"Artemis II is scheduled for April 2026 as the first crewed lunar flyby.",
		"The updated timeline targets the first crewed lunar flyby for April 2026.",
		"The mission will launch in March 2026 from Kennedy Space Center.",
		"A decision is expected in January 2026.",
	}
	for _, s := range flagged {
		if got := staleFutures(s, now); len(got) == 0 {
			t.Errorf("missed a past date written as upcoming: %q", s)
		}
	}

	clean := []string{
		// Genuinely ahead.
		"Artemis IV is scheduled for early 2028.",
		"The next launch is planned for December 2026.",
		// This month is not past.
		"A hearing is scheduled for September 2026.",
		// Past tense about a past date is right.
		"Artemis II launched in April 2026 and returned ten days later.",
		"The report was published in March 2026.",
		// No date at all.
		"The programme is years behind schedule.",
	}
	for _, s := range clean {
		if got := staleFutures(s, now); len(got) != 0 {
			t.Errorf("false positive on %q: %v", s, got)
		}
	}

	// One warning per month, not one per mention.
	twice := "It is scheduled for April 2026. It remains planned for April 2026."
	if got := staleFutures(twice, now); len(got) != 1 {
		t.Errorf("want one warning for a repeated month, got %d: %v", len(got), got)
	}
}

func TestStaleNow(t *testing.T) {
	now := time.Date(2026, time.September, 5, 12, 0, 0, 0, time.UTC)

	flagged := []string{
		"Artemis II is currently active: the mission reached Trans-Lunar Injection, with the crew cleared for launch on April 2, 2026.",
		"The mission is currently underway following its April 2026 launch.",
		"The trial is ongoing as of March 2026.",
	}
	for _, s := range flagged {
		if got := staleNow(s, now); len(got) == 0 {
			t.Errorf("missed stale present tense: %q", s)
		}
	}

	clean := []string{
		// Dated to this month, so "currently" is fair.
		"The mission is currently underway, having launched on September 2, 2026.",
		// Present tense with no date attached is not evidence of staleness.
		"The programme is currently years behind schedule.",
		// Past tense about a past date carries no present-tense marker.
		"Artemis II launched on April 2, 2026 and returned ten days later.",
	}
	for _, s := range clean {
		if got := staleNow(s, now); len(got) != 0 {
			t.Errorf("false positive on %q: %v", s, got)
		}
	}
}
