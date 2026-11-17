package property

import (
	"strings"
	"testing"
)

// The zoned name with the nearest school's distance reads as the drive to the
// zoned one unless the line says otherwise.
func TestSchoolsPartSaysWhenADistanceIsBorrowed(t *testing.T) {
	r := &Report{Zones: SchoolZones{
		Elementary: "Sampleton Elementary", ElemMiles: 2.4, ElemBorrowed: true,
		High: "Sampleton High", HighMiles: 5.1, Verified: true,
	}}
	m := r.SchoolsPart()
	if e, _ := m["elementary"].(string); !strings.Contains(e, "nearest elementary") {
		t.Errorf("a borrowed distance read as the zoned school's: %q", e)
	}
	if h, _ := m["high"].(string); strings.Contains(h, "nearest") {
		t.Errorf("a located school was called borrowed: %q", h)
	}
}
