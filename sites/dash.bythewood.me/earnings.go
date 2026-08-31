package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Earnings for companies big enough to move an index fund. Nasdaq publishes a
// free calendar with a market cap on every row, which is what makes "only the
// major ones" a filter rather than a curated list that would go stale.
const (
	earningsURL   = "https://api.nasdaq.com/api/calendar/earnings?date="
	earningsEvery = 6 * time.Hour

	// Ten billion. Below this a name is not going to be a reason the S&P moved,
	// and on a normal week it cuts a forty row day down to two or three.
	earningsMinCap = 10_000_000_000

	earningsDays  = 5
	earningsShown = 8
)

type Earning struct {
	Symbol string `json:"symbol"`
	Name   string `json:"name"`
	Cap    string `json:"cap"`
	When   string `json:"when"`
	Day    string `json:"day"`
}

type nasdaqEarnings struct {
	Data struct {
		Rows []struct {
			Symbol    string `json:"symbol"`
			Name      string `json:"name"`
			MarketCap string `json:"marketCap"`
			Time      string `json:"time"`
		} `json:"rows"`
	} `json:"data"`
}

func fetchEarnings(ctx context.Context, g *Guard, now time.Time) ([]Earning, error) {
	var out []Earning

	for i := 0; i < earningsDays && len(out) < earningsShown; i++ {
		day := now.In(easternTime()).AddDate(0, 0, i)

		// Weekends have no reports and asking for them is two wasted calls.
		if wd := day.Weekday(); wd == time.Saturday || wd == time.Sunday {
			continue
		}

		var payload nasdaqEarnings
		if err := getJSONWith(ctx, g, "nasdaq", earningsURL+day.Format("2006-01-02"), &payload); err != nil {
			// One bad day costs its own rows, not the panel.
			continue
		}

		label := dayLabel(day, now.In(easternTime()))
		for _, r := range payload.Data.Rows {
			cap := parseMoney(r.MarketCap)
			if cap < earningsMinCap {
				continue
			}
			out = append(out, Earning{
				Symbol: strings.TrimSpace(r.Symbol),
				Name:   trimCompany(r.Name),
				Cap:    shortMoney(cap),
				When:   whenLabel(r.Time),
				Day:    label,
			})
			if len(out) == earningsShown {
				break
			}
		}
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("nasdaq: nothing above the cap floor in %d days", earningsDays)
	}
	return out, nil
}

// getJSONWith is getJSON with the one header Nasdaq's API insists on. Without
// an Accept of application/json it answers with an HTML challenge page.
func getJSONWith(ctx context.Context, g *Guard, endpoint, url string, out any) error {
	return getJSONHeaders(ctx, g, endpoint, url, map[string]string{
		"Accept": "application/json",
	}, out)
}

func parseMoney(s string) float64 {
	s = strings.NewReplacer("$", "", ",", "", " ", "").Replace(strings.TrimSpace(s))
	if s == "" || s == "N/A" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}

func shortMoney(v float64) string {
	switch {
	case v >= 1e12:
		return fmt.Sprintf("%.1fT", v/1e12)
	case v >= 1e9:
		return fmt.Sprintf("%.0fB", v/1e9)
	default:
		return fmt.Sprintf("%.0fM", v/1e6)
	}
}

// trimCompany drops the suffixes that make every row the same width and say
// nothing, since the ticker is already there.
func trimCompany(name string) string {
	name = strings.TrimSpace(name)
	for _, suffix := range []string{
		", Inc.", " Inc.", " Inc", ", Ltd.", " Ltd.", " Ltd",
		" Corporation", " Corp.", " Corp", " Company", " Co.",
		" Holdings", " plc", " PLC", " S.A.", " N.V.",
	} {
		name = strings.TrimSuffix(name, suffix)
	}
	return strings.TrimSpace(strings.TrimSuffix(name, ","))
}

func whenLabel(t string) string {
	switch {
	case strings.Contains(t, "pre-market"):
		return "PRE"
	case strings.Contains(t, "after-hours"):
		return "POST"
	default:
		return ""
	}
}

func dayLabel(day, today time.Time) string {
	switch days := int(day.Truncate(24*time.Hour).Sub(today.Truncate(24*time.Hour)).Hours() / 24); days {
	case 0:
		return "TODAY"
	case 1:
		return "TOMORROW"
	default:
		return strings.ToUpper(day.Format("Mon 2 Jan"))
	}
}
