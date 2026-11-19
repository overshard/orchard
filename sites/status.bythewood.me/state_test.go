package main

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSuiteStateFlagsADownProperty(t *testing.T) {
	db, id := freshDB(t)
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO checks (property_id, status_code, response_ms, headers, created_at) VALUES (?, 503, 900, '{}', ?)`,
		id[:], time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	s := &site{db: db}
	st := s.suiteState(httptest.NewRequest("GET", "/suite/state", nil))
	if st.Level != "bad" || st.Count != 1 {
		t.Fatalf("got level %q count %d, want bad 1", st.Level, st.Count)
	}
	last := st.Items[len(st.Items)-1]
	if last.Kind != "property" || last.Level != "bad" || last.Href != "https://status.bythewood.me/"+id.String() {
		t.Fatalf("got %+v", last)
	}
}
