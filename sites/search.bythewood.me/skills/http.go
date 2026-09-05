package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// getJSON fetches and decodes. Every upstream here answers a block page rather
// than JSON to a non-browser agent, so the User-Agent is not optional.
func getJSON(ctx context.Context, d Deps, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", d.UA)
	req.Header.Set("Accept", "application/json")

	client := d.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func round2(v float64) float64 { return float64(int64(v*100+0.5)) / 100 }

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func readAll(resp *http.Response) ([]byte, error) {
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

func decode(body []byte, out any) error {
	return json.Unmarshal(body, out)
}
