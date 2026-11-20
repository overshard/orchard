package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"slices"
	"time"
)

// The residents trade on the board every hour, dealt off an HMAC of the
// pepper like the prices, so the market moves when few ponds are about and
// every replica and restart deals the same trades.
const (
	// Out of 256, the odds a resident trades in a given hour.
	folkOdds = 48
	// How far one lot of a good moves its price, as a log.
	folkLot = 0.002
	tapeLen = 8
)

// lean is a resident's habit on the board: what it trades, and the day that
// makes it buy or sell, with what the tape says when it does.
type lean struct {
	key  string
	when func(p push, day string) (buy bool, ok bool)
	// What the tape adds on a buy and on a sale.
	say [2]string
}

func pushUp(p push, _ string) (bool, bool)   { return true, p.by > 0.01 }
func pushDown(p push, _ string) (bool, bool) { return false, p.by < -0.01 }
func whyIs(why string, buy bool) func(push, string) (bool, bool) {
	return func(p push, _ string) (bool, bool) { return buy, p.why == why }
}

// In the order of residents.
var leans = []lean{
	{"g_reeds", whyIs("Rain on the way, and the gardens want reeds", true), [2]string{"before the rain"}},
	{"g_sticks", pushUp, [2]string{"for a warm fire"}},
	{coinKey, func(push, string) (bool, bool) { return true, true }, [2]string{"on a hunch"}},
	{"g_clay", pushUp, [2]string{"to patch the burrow"}},
	{"g_sticks", whyIs("The storm brought sticks down everywhere", false), [2]string{"", "off the fallen branches"}},
	{melonKey, func(_ push, day string) (bool, bool) {
		return melonDay(day), melonDay(day) || weekday(day) == time.Saturday
	},
		[2]string{"fresh off the cart", "before they turn"}},
	{"g_fish", pushDown, [2]string{"", "since he has too many"}},
	{"g_reeds", whyIs("Rain on the way, and the gardens want reeds", true), [2]string{"since his knee says rain"}},
	{"g_clay", pushDown, [2]string{"", "while it still fetches something"}},
	{"g_sticks", func(p push, _ string) (bool, bool) { return true, p.by < -0.01 }, [2]string{"for picture frames"}},
	{"g_reeds", pushDown, [2]string{"", "since the shop is full of them"}},
	{coinKey, func(push, string) (bool, bool) { return true, true }, [2]string{"and is hoarding it again"}},
}

type folkTrade struct {
	Min  int
	Who  int
	Key  string
	Buy  bool
	N    int
	Lean bool
}

// folkTradesOn is every trade the residents make on a day, in order.
func folkTradesOn(pepper []byte, day string, pushes map[string]push) []folkTrade {
	var out []folkTrade
	for h := range 24 {
		for i, l := range leans {
			mac := hmac.New(sha256.New, pepper)
			fmt.Fprintf(mac, "folk|%s|%d|%d", day, h, i)
			b := mac.Sum(nil)
			if int(b[0]) >= folkOdds {
				continue
			}
			t := folkTrade{Min: h*60 + int(b[1])%60, Who: i, Key: l.key}
			if b[2] >= 180 {
				t.Key = assets[int(b[3])%len(assets)].Key
			}
			p := pushes[t.Key]
			roll := int(b[4])
			switch buy, ok := l.when(p, day); {
			case t.Key == l.key && ok && roll < 205:
				t.Buy, t.Lean = buy, true
			case p.by > 0.01:
				t.Buy = roll < 166
			case p.by < -0.01:
				t.Buy = roll < 90
			default:
				t.Buy = roll < 128
			}
			if t.Key == melonKey && !melonDay(day) {
				t.Buy, t.Lean = false, t.Lean && !t.Buy
			}
			t.N = 1 + int(b[5])%4
			out = append(out, t)
		}
	}
	slices.SortStableFunc(out, func(a, b folkTrade) int { return a.Min - b.Min })
	return out
}

// folkImpact is the log move the residents' trades in a good have made by a
// minute of the day. Buying takes a good up and selling it down, while the
// coin and the melon go their own way whoever trades them.
func folkImpact(trades []folkTrade, key string, until int) float64 {
	if a, _ := assetOf(key); a.Kind != kindGood {
		return 0
	}
	x := 0.0
	for _, t := range trades {
		if t.Min >= until || t.Key != key {
			continue
		}
		if t.Buy {
			x += folkLot * float64(t.N)
		} else {
			x -= folkLot * float64(t.N)
		}
	}
	return x
}

func minuteOf(t time.Time) int {
	l := t.In(pondZone)
	return l.Hour()*60 + l.Minute()
}

func weekday(day string) time.Weekday {
	t, _ := time.ParseInLocation("2006-01-02", day, pondZone)
	return t.Weekday()
}

type tapeRow struct {
	Who  int    `json:"who"`
	Name string `json:"name"`
	Line string `json:"line"`
	At   string `json:"at"`
	Key  string `json:"key"`
	Buy  bool   `json:"buy"`
}

// tapeOf is the latest residents' trades up to now, newest first.
func tapeOf(pepper []byte, day string, pushes map[string]push, now time.Time) []tapeRow {
	out := []tapeRow{}
	until := minuteOf(now)
	trades := folkTradesOn(pepper, day, pushes)
	for _, t := range slices.Backward(trades) {
		if len(out) == tapeLen {
			break
		}
		if t.Min > until {
			continue
		}
		out = append(out, tapeRow{Who: t.Who, Name: residents[t.Who].name, Line: tradeLine(t), At: clockOf(t.Min), Key: t.Key, Buy: t.Buy})
	}
	return out
}

func tradeLine(t folkTrade) string {
	a, _ := assetOf(t.Key)
	what := ""
	switch a.Kind {
	case kindGood:
		what = fmt.Sprintf("%d %s", t.N*lot, storyName(a))
	case kindCoin:
		what = fmt.Sprintf("%d glow coin", t.N)
	default:
		what = plural(t.N, "moonmelon")
	}
	verb := "sold "
	if t.Buy {
		verb = "bought "
	}
	line := verb + what
	say := leans[t.Who].say[0]
	if !t.Buy {
		say = leans[t.Who].say[1]
	}
	if t.Lean && say != "" {
		line += " " + say
	}
	return line
}
