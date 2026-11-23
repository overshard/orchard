package main

import (
	"testing"
	"time"
)

func TestMonthExpandsCron(t *testing.T) {
	s := &site{loc: ny}
	jobs := []Job{
		{ID: 1, Name: "trash", Cron: "0 7 * * 2", Enabled: true},
		{ID: 2, Name: "pills", Cron: "0 8,20 * * *", Enabled: true},
		{ID: 3, Name: "dentist", At: time.Date(2026, 12, 9, 15, 0, 0, 0, ny), Enabled: true},
		{ID: 4, Name: "off", Cron: "0 9 * * *"},
	}
	now := time.Date(2026, 12, 15, 12, 0, 0, 0, ny)
	m := s.month(jobs, time.Date(2026, 12, 20, 0, 0, 0, 0, ny), now)

	if len(m.Weeks) != 5 || m.Weeks[0][0].Day.Day() != 29 || m.Weeks[0][0].In {
		t.Fatalf("weeks %d, first %v", len(m.Weeks), m.Weeks[0][0].Day)
	}
	trash, pills, dentist := 0, 0, 0
	for _, w := range m.Weeks {
		if len(w) != 7 {
			t.Fatalf("a week of %d days", len(w))
		}
		for _, d := range w {
			if d.Today != (d.In && d.Day.Day() == 15) {
				t.Errorf("today wrong on %v", d.Day)
			}
			for _, it := range d.Items {
				switch it.Name {
				case "trash":
					trash++
					if d.Day.Weekday() != time.Tuesday || it.At.Hour() != 7 {
						t.Errorf("trash on %v", it.At)
					}
				case "pills":
					pills++
					if it.Count != 2 || it.At.Hour() != 8 {
						t.Errorf("pills %+v", it)
					}
				case "dentist":
					dentist++
					if d.Day.Day() != 9 {
						t.Errorf("dentist on %v", d.Day)
					}
				default:
					t.Errorf("paused job %q shown", it.Name)
				}
			}
		}
	}
	if trash != 5 || pills != 31 || dentist != 1 {
		t.Errorf("trash %d pills %d dentist %d", trash, pills, dentist)
	}
	if m.Fires != 5+62+1 || m.Prev.Month() != time.November || m.Next.Month() != time.January {
		t.Errorf("fires %d prev %v next %v", m.Fires, m.Prev, m.Next)
	}
}
