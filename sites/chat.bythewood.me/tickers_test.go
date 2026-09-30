package main

import (
	"encoding/json"
	"testing"
)

func TestTheTickerHeTypedIsTheOneQuoted(t *testing.T) {
	for _, c := range []struct{ said, symbols, want string }{
		{"MU after market close and earnings how's it doing", "META", "MU"},
		{"how's MU doing against the S&P", "MU, S&P 500", "MU, S&P 500"},
		{"AMD vs NVDA today", "AMD", "AMD, NVDA"},
		{"how are futures doing", "futures", "futures"},
		{"is the market down after the CPI print", "S&P 500", "S&P 500"},
		{"WHAT IS THE DOW DOING TODAY", "DJIA", "DJIA"},
	} {
		args, _ := json.Marshal(map[string]any{"symbols": c.symbols, "period": "day"})
		var got map[string]any
		if err := json.Unmarshal(withTickers(args, c.said), &got); err != nil {
			t.Fatal(err)
		}
		if got["symbols"] != c.want || got["period"] != "day" {
			t.Errorf("%q with %q = %v, want %q", c.said, c.symbols, got["symbols"], c.want)
		}
	}
}
