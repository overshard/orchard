package tools

import "testing"

func TestOddsKeepsOnlyMarketsAboutTheQuestion(t *testing.T) {
	for _, c := range []struct {
		title, query string
		want         bool
	}{
		{"Will Venezuelan crude oil production reach 1.2m barrels per day in 2026?", "Optimizing SQLite Django in production", false},
		{"Will BLG be number 1 on the end of year Global Power Rankings?", "blog analytics bythewood.me", false},
		{"Will Person A win the next Drummond–Bois-Francs election?", "page views", false},
		{"Will Harry Kane finish in the top 3 of the 2026 Ballon d'Or?", "top pages", false},
		{"Lions vs. Panthers", "Carolina Panthers", true},
		{"US Open 2026 Men's Winner", "US Open winner", true},
		{"Government shutdown by October 31?", "government shutdown", true},
		{"Will the Fed cut rates by 25 bps in October?", "interest rates", true},
		{"Presidential Election Winner 2028", "2028 presidential election", true},
	} {
		if got := aboutWords(c.title, oddsWords(c.query)); got != c.want {
			t.Errorf("%q for %q = %v, want %v", c.title, c.query, got, c.want)
		}
	}
}
