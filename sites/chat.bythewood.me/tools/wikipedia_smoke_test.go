package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"
)

// Runs against a real kiwix only when told to, since the suite must not need
// a container to pass.
func TestWikipediaSmoke(t *testing.T) {
	base := os.Getenv("WIKI_SMOKE")
	if base == "" {
		t.Skip("set WIKI_SMOKE to a kiwix base url")
	}
	d := &Deps{HTTP: &http.Client{Timeout: 10 * time.Second}, Now: time.Now, Guard: NewGuard(time.Minute)}
	for _, q := range []string{"North Korea", "France", "Kim Jong-un", "yadkin valley"} {
		start := time.Now()
		out, err := wikiLookup(context.Background(), d, base, q)
		if err != nil {
			t.Errorf("%s: %v", q, err)
			continue
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		t.Logf("QUERY %q in %s\n%s\n", q, time.Since(start).Round(time.Millisecond), b)
	}
}
