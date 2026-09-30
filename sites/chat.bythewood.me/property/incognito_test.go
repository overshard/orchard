package property

import (
	"context"
	"testing"
)

func TestAnIncognitoHouseLeavesNoRow(t *testing.T) {
	db := testDB(t)
	const q = `INSERT INTO geocodes (query, lat, lon, matched, source, fetched_at) VALUES (?, 1, 2, 'x', 'census', 0)`

	if _, err := keep(Incognito(context.Background()), db, q, "12 secret lane"); err != nil {
		t.Fatal(err)
	}
	if _, err := keep(context.Background(), db, q, "1 main st"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM geocodes`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("%d geocode rows, want only the one that was not incognito", n)
	}
}
