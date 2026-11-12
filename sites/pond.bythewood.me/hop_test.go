package main

import "testing"

func TestHopOnlyGoesToActivePonds(t *testing.T) {
	s, clock := testStore(t)
	_, mine := newPond(t, s)
	newPond(t, s)
	_, active := newPond(t, s)
	_, stale := newPond(t, s)
	pub := func(id int64) string {
		var c string
		s.r.QueryRow(`SELECT public_id FROM ponds WHERE id = ?`, id).Scan(&c)
		return c
	}
	now := clock.Unix()
	s.w.Exec(`UPDATE ponds SET played_at = ? WHERE id IN (?, ?)`, now+3600, mine, active)
	s.w.Exec(`UPDATE ponds SET created_at = ?, played_at = ? WHERE id = ?`, now-30*86400, now-8*86400, stale)
	*clock = clock.Add(2 * 3600e9)

	for i := 0; i < 20; i++ {
		code, err := s.hopTo(pub(mine), "")
		if err != nil || code != pub(active) {
			t.Fatalf("hopped to %q, %v, want only the active pond", code, err)
		}
	}
	if _, err := s.hopTo(pub(mine), pub(active)); err != errNoPond {
		t.Errorf("hopping from the only active pond: %v", err)
	}
}
