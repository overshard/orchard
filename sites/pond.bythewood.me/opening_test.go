package main

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// age backdates a pond and its frogs, so the day's columns stay on today.
func age(t *testing.T, s *store, id int64, by time.Duration) {
	t.Helper()
	secs := int64(by / time.Second)
	if _, err := s.w.Exec(`UPDATE ponds SET created_at = created_at - ? WHERE id = ?`, secs, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.w.Exec(`UPDATE frogs SET hatched_at = hatched_at - ? WHERE pond_id = ?`, secs, id); err != nil {
		t.Fatal(err)
	}
}

func TestANewPondOpensOverItsFirstDays(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	steps := []struct {
		by            time.Duration
		works, market bool
		notice        string
	}{
		{0, false, false, ""},
		{23 * time.Hour, false, false, ""},
		{time.Hour, true, false, "Works"},
		{23 * time.Hour, true, false, "Works"},
		{time.Hour, true, false, ""},
		{47 * time.Hour, true, false, ""},
		{time.Hour, true, true, "Market"},
		{24 * time.Hour, true, true, ""},
	}
	for _, st := range steps {
		age(t, s, id, st.by)
		v, err := s.view(id)
		if err != nil {
			t.Fatal(err)
		}
		if v.Open.Works != st.works || v.Open.Market != st.market {
			t.Errorf("after another %v: %+v", st.by, v.Open)
		}
		if n := v.Gazette.Notice; (st.notice == "") != (n == "") || !strings.Contains(n, st.notice) {
			t.Errorf("after another %v the gazette says %q", st.by, n)
		}
		if strings.ContainsAny(v.Gazette.Notice, "0123456789") {
			t.Errorf("a number in the gazette: %q", v.Gazette.Notice)
		}
		if v.Open.Works != (v.Frog.Stage != "egg") {
			t.Errorf("works open is %v on a frog that's %s", v.Open.Works, v.Frog.Stage)
		}
	}
}

func TestAnOlderPondIsFullyOpen(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	// A pond from before the column has its default.
	if _, err := s.w.Exec(`UPDATE ponds SET staged = (SELECT dflt_value FROM pragma_table_info('ponds') WHERE name = 'staged') WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	v, err := s.view(id)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Open.Works || !v.Open.Market || v.Gazette.Notice != "" {
		t.Errorf("an older pond: %+v %q", v.Open, v.Gazette.Notice)
	}
	for _, c := range addedColumns {
		if c.name == "staged" && !strings.HasSuffix(c.def, "DEFAULT 0") {
			t.Errorf("staged is added with %q", c.def)
		}
	}
}

func TestAClosedPlaceStillTakesItsMoves(t *testing.T) {
	s, _ := testStore(t)
	_, id := newPond(t, s)
	give(t, s, id, 100)
	if _, err := s.build(id, "gravel"); err != nil && !errors.Is(err, errPoor) {
		t.Errorf("the bank refused a pond a day old: %v", err)
	}
	v, _ := s.view(id)
	for _, td := range v.Todo {
		if !v.Open.has(td.To) {
			t.Errorf("a row points at a closed place: %+v", td)
		}
	}
}
