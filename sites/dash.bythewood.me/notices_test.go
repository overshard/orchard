package main

import (
	"strings"
	"testing"
	"time"
)

func noticeIDs(ns []Notice) []string {
	var out []string
	for _, n := range ns {
		out = append(out, n.ID)
	}
	return out
}

// A poller replaces its own kind and nothing else, and a snapshot taken before
// the write still reads what it read, since the hub may be marshalling it.
func TestSetNoticesReplacesOneKind(t *testing.T) {
	var st State
	st.setNotices("news", []Notice{{ID: "news:a", Kind: "news"}})
	st.setNotices("weather", []Notice{{ID: "weather:w", Kind: "weather"}})
	before := st.Notices

	st.setNotices("news", []Notice{{ID: "news:b", Kind: "news"}})
	got := strings.Join(noticeIDs(st.Notices), ",")
	if got != "weather:w,news:b" {
		t.Errorf("notices = %s", got)
	}
	if before[0].ID != "news:a" {
		t.Errorf("an earlier snapshot changed under its reader: %v", noticeIDs(before))
	}
}

func sessionQuote(symbol string, prev float64, closes []float64, start time.Time) Quote {
	times := make([]int64, len(closes))
	for i := range closes {
		times[i] = start.Add(time.Duration(i) * 30 * time.Minute).Unix()
	}
	return Quote{Symbol: symbol, Price: closes[len(closes)-1], Previous: prev,
		AsOf: time.Unix(times[len(times)-1], 0), Closes: closes, Times: times}
}

// A notice comes at the act bar, keeps its id while the fall deepens,
// and takes in the Nasdaq when it follows, since that's the same afternoon.
func TestMarketNoticesAtTheActBar(t *testing.T) {
	open := at(t, "2026-09-29 09:30")
	now := at(t, "2026-09-29 11:00")
	quotes := func(spx, ndx []float64) map[string]Quote {
		return map[string]Quote{
			"^GSPC": sessionQuote("^GSPC", 100, spx, open),
			"^IXIC": sessionQuote("^IXIC", 100, ndx, open),
		}
	}

	quiet := buildMarket(quotes([]float64{98.5, 98.3, 98.2, 98.1}, []float64{98, 97.8, 97.7, 97.6}), now)
	if len(quiet.notices) != 0 {
		t.Errorf("a 1.9%% day raised %v", noticeIDs(quiet.notices))
	}

	two := buildMarket(quotes([]float64{98.2, 98.0, 97.9, 97.9}, []float64{98, 97.8, 97.7, 97.6}), now)
	both := buildMarket(quotes([]float64{98.2, 98.0, 97.3, 96.9}, []float64{98, 97.8, 97.2, 97.1}), now)
	if len(two.notices) != 1 || two.notices[0].Title != "S&P 500 down 2.1% today" {
		t.Fatalf("a 2.1%% fall gave %+v", two.notices)
	}
	if two.notices[0].ID != "market:stocks:2026-09-28:down" {
		t.Errorf("id = %s", two.notices[0].ID)
	}
	if len(both.notices) != 1 || both.notices[0].ID != two.notices[0].ID {
		t.Fatalf("a deeper fall with the Nasdaq gave %v, want the same id", noticeIDs(both.notices))
	}
	if got := both.notices[0].Title; got != "S&P 500 down 3.1% and Nasdaq down 2.9% today" {
		t.Errorf("title = %q", got)
	}
	if !strings.Contains(both.notices[0].Body, "S&P -3.10%, Nasdaq -2.90%") {
		t.Errorf("body = %q", both.notices[0].Body)
	}
}

// Evening futures still carry the day, so they're the same move as the cash
// index that closed down, and after midnight they're measured from that close.
func TestMarketNoticesForFutures(t *testing.T) {
	card := func(asOf string, pct string) []Card {
		return []Card{{Key: "sp500", Label: "S&P 500", Symbol: "ES=F", Percent: pct, asOf: at(t, asOf)}}
	}
	cash := marketNotices([]Card{{Key: "sp500", Label: "S&P 500", Symbol: "^GSPC", Percent: "-2.40%", asOf: at(t, "2026-09-29 15:59")}}, "regular")
	evening := marketNotices(card("2026-09-29 20:00", "-2.60%"), "closed")
	if len(cash) != 1 || len(evening) != 1 || cash[0].ID != evening[0].ID {
		t.Fatalf("cash %v and evening futures %v", noticeIDs(cash), noticeIDs(evening))
	}
	if evening[0].Title != "S&P 500 futures down 2.6% since Monday's close" {
		t.Errorf("title = %q", evening[0].Title)
	}
	if late := marketNotices(card("2026-09-30 01:00", "-2.10%"), "closed"); len(late) != 1 || late[0].ID != "market:stocks:2026-09-29:down" {
		t.Errorf("an overnight fall gave %v", noticeIDs(late))
	}
	if sunday := marketNotices(card("2026-10-04 19:00", "+2.20%"), "closed"); len(sunday) != 1 || sunday[0].ID != "market:stocks:2026-10-02:up" {
		t.Errorf("sunday night gave %v", noticeIDs(sunday))
	}
}

// A VIX that closed past 30 is still past it the next day, and only the day it
// crossed is a notice.
func TestVIXNoticeOnlyOnTheCross(t *testing.T) {
	vix := func(price, change string) []Card {
		return []Card{{Key: "vix", Label: "VIX", Symbol: "^VIX", Price: price, Change: change, asOf: at(t, "2026-09-29 14:00")}}
	}
	got := marketNotices(vix("31.20", "+4.80"), "regular")
	if len(got) != 1 || got[0].ID != "market:vix:2026-09-28" || got[0].Title != "VIX up to 31.20 from 26.40" {
		t.Errorf("a cross gave %+v", got)
	}
	if got := marketNotices(vix("33.00", "+1.50"), "regular"); len(got) != 0 {
		t.Errorf("a VIX already past the bar gave %v", noticeIDs(got))
	}
}

// A warning is continued and extended under new ids through its life, and the
// VTEC event number stays the same.
func TestAlertKeyFollowsTheEventNotTheMessage(t *testing.T) {
	first := alertKey([]string{"/O.NEW.KRAH.SV.W.0123.260928T2000Z-260928T2045Z/"}, "urn:1")
	later := alertKey([]string{"/O.CON.KRAH.SV.W.0123.000000T0000Z-260928T2045Z/"}, "urn:2")
	if first != "KRAH.SV.W.0123" || first != later {
		t.Errorf("keys %q and %q, want both KRAH.SV.W.0123", first, later)
	}
	if got := alertKey(nil, "urn:3"); got != "urn:3" {
		t.Errorf("no VTEC gave %q, want the id", got)
	}
}

func TestOnlySevereAlertsNotify(t *testing.T) {
	alerts := []Alert{
		{Event: "TORNADO WARNING", Severity: "extreme", key: "t", name: "Tornado Warning", Area: "ALEXANDER", Until: "MON 8:30PM"},
		{Event: "WINTER STORM WATCH", Severity: "moderate", key: "w", name: "Winter Storm Watch"},
		{Event: "HEAT ADVISORY", Severity: "moderate", key: "h", name: "Heat Advisory"},
		{Event: "SPECIAL WEATHER STATEMENT", Severity: "moderate", key: "s", name: "Special Weather Statement"},
		{Event: "SEVERE THUNDERSTORM WARNING", Severity: "severe", key: "c", name: "Severe Thunderstorm Warning", cancel: true},
	}
	got := strings.Join(noticeIDs(alertNotices(alerts)), ",")
	if got != "weather:t,weather:w" {
		t.Errorf("notified %s, want the warning and the watch", got)
	}
	if n := alertNotices(alerts[:1])[0]; n.Title != "Tornado Warning" || n.Body != "ALEXANDER, until MON 8:30PM" {
		t.Errorf("notice = %+v", n)
	}
}

// A story has to be everywhere and matter before it's a notice, and the same
// lead read again at the next slot is the same id.
func TestNewsNoticesOnlyForAMajorStory(t *testing.T) {
	lead := Link{Source: "AP", URL: "https://apnews.com/a", Title: "US strikes Iranian nuclear sites"}
	read := func(level string, impact int) Brief {
		return Brief{Slot: 1, Read: &Read{Level: level, Spread: "TOP STORY ON 9 OF 11 OUTLETS", Impact: impact, Links: []Link{lead}}}
	}
	for _, b := range []Brief{{}, read("steady", 5), read("major", 3), read("major", 0)} {
		if got := briefNotices(b); len(got) != 0 {
			t.Errorf("%+v notified %v", b.Read, noticeIDs(got))
		}
	}
	got, again := briefNotices(read("major", 4)), briefNotices(read("major", 5))
	if len(got) != 1 || got[0].ID != "news:https://apnews.com/a" || got[0].ID != again[0].ID {
		t.Fatalf("a major story gave %v and %v", noticeIDs(got), noticeIDs(again))
	}
	if got[0].Title != lead.Title || got[0].Body != "AP, top story on 9 of 11 outlets" || got[0].URL != lead.URL {
		t.Errorf("notice = %+v", got[0])
	}
}

func TestChangedReports(t *testing.T) {
	known := []Earning{{Symbol: "UNH", date: "2026-10-13", Verdict: "MISS", Actual: "$2.92", Forecast: "$4.12"}}
	if got := changedReports(known, known); len(got) != 0 {
		t.Errorf("a print the full poll already had counted as changed: %+v", got)
	}

	added := append(known, Earning{Symbol: "JNJ", date: "2026-10-13", Verdict: "BEAT"})
	if got := changedReports(added, known); len(got) != 1 || got[0].now.Symbol != "JNJ" || got[0].was != nil {
		t.Errorf("a new print = %+v", got)
	}

	revised := []Earning{{Symbol: "UNH", date: "2026-10-13", Verdict: "BEAT", Actual: "$4.69", Forecast: "$4.12"}}
	got := changedReports(revised, known)
	if len(got) != 1 || got[0].was == nil || got[0].was.Actual != "$2.92" || got[0].now.Actual != "$4.69" {
		t.Errorf("a revised print = %+v", got)
	}
}

// Monday's weekday before is Friday, since an evening report on Friday is still
// arriving on Monday morning.
func TestRecentReportsReachBackOverTheWeekend(t *testing.T) {
	monday := time.Date(2026, 9, 28, 7, 0, 0, 0, easternTime())
	var asked []string
	day := func(d time.Time) ([]Earning, bool) {
		asked = append(asked, d.Format("Mon"))
		return []Earning{{Symbol: "X", Verdict: "BEAT"}, {Symbol: "Y"}}, true
	}
	got := recentReports(day, monday)
	if strings.Join(asked, ",") != "Mon,Fri" || len(got) != 2 {
		t.Errorf("asked %v and kept %d rows", asked, len(got))
	}
}

func TestReportingHours(t *testing.T) {
	for when, want := range map[string]bool{
		"2026-09-28 05:59": false,
		"2026-09-28 06:00": true,
		"2026-09-28 16:30": true,
		"2026-09-28 20:00": false,
		"2026-09-27 12:00": false,
	} {
		if got := reportingHours(at(t, when)); got != want {
			t.Errorf("%s: %v, want %v", when, got, want)
		}
	}
}

func TestMarketLevels(t *testing.T) {
	cards := []Card{
		{Key: "sp500", Percent: "-0.40%"},
		{Key: "nasdaq", Percent: "-2.61%"},
		{Key: "dow", Percent: "-3.10%"},
		{Key: "vix", Price: "31.20", Percent: "+18.00%"},
		{Key: "bitcoin", Percent: "+12.40%"},
		{Key: "oil", Percent: "-6.00%"},
	}
	var got []string
	for _, l := range marketLevels(cards) {
		got = append(got, l.card.Key+" "+l.level+" "+l.why)
	}
	want := "nasdaq act the Nasdaq moved -2.61%, vix act the VIX is 31.2, bitcoin watch bitcoin moved +12.40%"
	if strings.Join(got, ", ") != want {
		t.Errorf("levels\n %s\nwant\n %s", strings.Join(got, ", "), want)
	}
}
