package main

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
)

// A ticker he typed in capitals is the one he means. Asked about MU after the
// close, the model quoted META and called it Meta's earnings.
var (
	typedTicker  = regexp.MustCompile(`\b[A-Z]{2,5}\b`)
	tickerShaped = regexp.MustCompile(`^[A-Z]{1,5}$`)
	anyWord      = regexp.MustCompile(`\b[A-Za-z]{2,}\b`)
)

// Capitals that are not stocks, in the way people write about markets.
var notTicker = map[string]bool{
	"AI": true, "US": true, "USA": true, "UK": true, "EU": true, "CEO": true, "CFO": true, "CTO": true,
	"IPO": true, "ETF": true, "CPI": true, "PPI": true, "GDP": true, "FOMC": true, "FED": true, "SEC": true,
	"FTC": true, "DOJ": true, "EDT": true, "EST": true, "ET": true, "PT": true, "AM": true, "PM": true,
	"OK": true, "TV": true, "EV": true, "API": true, "YTD": true, "EPS": true, "ATH": true, "IMO": true,
	"LOL": true, "NYSE": true, "AH": true, "PE": true, "IRA": true, "LLC": true, "INC": true, "NC": true,
	"NY": true, "DOW": true, "OPEC": true, "WTI": true,
}

func typedTickers(said string) []string {
	words := anyWord.FindAllString(said, -1)
	caps := typedTicker.FindAllString(said, -1)
	// A message typed in capitals says nothing about which words are tickers.
	if len(caps)*2 > len(words) && len(words) > 2 {
		return nil
	}
	var out []string
	for _, c := range caps {
		if !notTicker[c] && !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	return out
}

// withTickers puts the tickers he typed into a markets call. When the model
// kept none of them it swapped one for another, so its own tickers go too.
func withTickers(args json.RawMessage, said string) json.RawMessage {
	typed := typedTickers(said)
	var m map[string]any
	if len(typed) == 0 || json.Unmarshal(args, &m) != nil {
		return args
	}
	raw, _ := m["symbols"].(string)
	var asked []string
	for _, s := range strings.Split(raw, ",") {
		if s = strings.TrimSpace(s); s != "" {
			asked = append(asked, s)
		}
	}
	swapped := !slices.ContainsFunc(asked, func(s string) bool { return slices.Contains(typed, strings.ToUpper(s)) })
	var keep []string
	for _, s := range asked {
		if swapped && tickerShaped.MatchString(s) {
			continue
		}
		keep = append(keep, s)
	}
	for _, t := range typed {
		if !slices.ContainsFunc(keep, func(s string) bool { return strings.EqualFold(s, t) }) {
			keep = append(keep, t)
		}
	}
	if slices.Equal(keep, asked) {
		return args
	}
	m["symbols"] = strings.Join(keep, ", ")
	out, err := json.Marshal(m)
	if err != nil {
		return args
	}
	return out
}
