package main

import (
	"os"
	"testing"
)

// TestOneSearch spends exactly one real search, so it is off by default. A test
// run that quietly burns search allowance is how you end up rate limited
// without knowing why.
//
//	SEARCH_LIVE=1 go test -run TestOneSearch -v .
func TestOneSearch(t *testing.T) {
	if os.Getenv("SEARCH_LIVE") == "" {
		t.Skip("set SEARCH_LIVE=1 to spend one real search")
	}
	res, err := SearchDDG(newHTTPClient(), "sqlite wal mode", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res) == 0 {
		t.Fatal("no results parsed, the markup may have changed")
	}
	for i, r := range res {
		t.Logf("  %d. %s | %s", i+1, r.Title, r.URL)
	}
}
