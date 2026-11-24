package main

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"cron.bythewood.me/suite"
)

// chrome is the header and status line for a signed in page. on names the
// page tab that is lit.
func (s *site) chrome(on string, jobs []Job) suite.Page {
	p := suite.Page{Tool: "cron", SignedIn: true, Source: sourceURL, Author: author, Year: time.Now().Year()}
	for _, t := range []suite.Tab{{Name: "jobs", Href: "/"}, {Name: "calendar", Href: "/calendar"}, {Name: "runs", Href: "/runs"}, {Name: "new", Href: "/jobs/new"}, {Name: "keys", Href: "/keys"}} {
		t.On = t.Name == on
		p.Tabs = append(p.Tabs, t)
	}
	sum := summarize(jobs)
	p.Status = append(p.Status, suite.Seg{Text: plural(sum.Active, "job", "jobs")})
	if sum.Failing > 0 {
		p.Status = append(p.Status, suite.Seg{Text: "● " + fmt.Sprint(sum.Failing) + " failing", Kind: "bad"})
	} else if len(jobs) > 0 {
		p.Status = append(p.Status, suite.Seg{Text: "● all ok", Kind: "ok"})
	}
	if sum.Paused > 0 {
		p.Status = append(p.Status, suite.Seg{Text: fmt.Sprint(sum.Paused) + " paused"})
	}
	if !sum.Next.IsZero() {
		p.Meta = "next " + s.eastern(sum.Next.NextRun) + " · " + sum.Next.Name
		p.Status = append(p.Status, suite.Seg{Text: "next " + until(sum.Next.NextRun)})
	}
	p.Right = []suite.Seg{{Text: "topic " + topic}, {Text: "times are Eastern"}}
	return p
}

type jobSummary struct {
	Active, Paused, Failing int
	Next                    Job
}

// IsZero reports a summary with no job coming up.
func (j Job) IsZero() bool { return j.ID == 0 }

func summarize(jobs []Job) jobSummary {
	var out jobSummary
	for _, j := range jobs {
		if !j.Enabled {
			out.Paused++
			continue
		}
		out.Active++
		if j.LastErr != "" {
			out.Failing++
		}
		if !j.NextRun.IsZero() && (out.Next.IsZero() || j.NextRun.Before(out.Next.NextRun)) {
			out.Next = j
		}
	}
	return out
}

// kindName is what a person calls a kind, since prompt and notify are the
// API's words.
func kindName(k string) string {
	switch k {
	case "prompt":
		return "check"
	case "quote":
		return "quote"
	}
	return "reminder"
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// spellNumber keeps a page title reading as a sentence for small counts.
func spellNumber(n int) string {
	words := []string{"No", "One", "Two", "Three", "Four", "Five", "Six", "Seven", "Eight", "Nine", "Ten", "Eleven", "Twelve"}
	if n >= 0 && n < len(words) {
		return words[n]
	}
	return fmt.Sprint(n)
}

// activity buckets two weeks of runs by Eastern day, oldest first, for the
// tiles on the jobs page.
type activity struct {
	Pushed, Failed, Quiet, Missed, Took []float64
	Labels                              []string
	PushedWeek, PushedPrev, QuietWeek   int
	FailedWeek, RunsWeek, Total         int
	MissedAll                           int
	// TookWeek is the median seconds a check took this week, checks only
	// since a reminder takes no time worth showing.
	TookWeek float64
}

func (s *site) activity(runs []Run) activity {
	const days = 14
	today := midnight(time.Now().In(s.loc))
	a := activity{Pushed: make([]float64, days), Failed: make([]float64, days), Quiet: make([]float64, days),
		Missed: make([]float64, days), Took: make([]float64, days)}
	took := make([][]float64, days)
	var week []float64
	for i := range days {
		a.Labels = append(a.Labels, today.AddDate(0, 0, i-days+1).Format("Mon Jan 2"))
	}
	for _, r := range runs {
		i := days - 1 - dayIndexFrom(midnight(r.At.In(s.loc)), today)
		if i < 0 || i >= days {
			continue
		}
		a.Total++
		res := r.Result()
		switch res {
		case "failed":
			a.Failed[i]++
		case "pushed":
			a.Pushed[i]++
		case "quiet":
			a.Quiet[i]++
		case "missed":
			a.Missed[i]++
			a.MissedAll++
		}
		if r.Kind == "prompt" && res != "missed" {
			took[i] = append(took[i], float64(r.MS)/1000)
		}
		if i >= days-7 {
			a.RunsWeek++
			switch res {
			case "failed":
				a.FailedWeek++
			case "pushed":
				a.PushedWeek++
			case "quiet":
				a.QuietWeek++
			}
			if r.Kind == "prompt" && res != "missed" {
				week = append(week, float64(r.MS)/1000)
			}
		} else if res == "pushed" {
			a.PushedPrev++
		}
	}
	for i, t := range took {
		a.Took[i] = median(t)
	}
	a.TookWeek = median(week)
	return a
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	v = slices.Clone(v)
	slices.Sort(v)
	m := v[len(v)/2]
	if len(v)%2 == 0 {
		m = (v[len(v)/2-1] + m) / 2
	}
	return math.Round(m*10) / 10
}

func (a activity) PushDelta() string {
	d := a.PushedWeek - a.PushedPrev
	switch {
	case d > 0:
		return fmt.Sprintf("+%d on last week", d)
	case d < 0:
		return fmt.Sprintf("−%d on last week", -d)
	}
	return "same as last week"
}

func (a activity) RunsSeries() []float64 {
	out := make([]float64, len(a.Pushed))
	for i := range out {
		out[i] = a.Pushed[i] + a.Failed[i] + a.Quiet[i] + a.Missed[i]
	}
	return out
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

var weekdays = []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}

// humanCron reads the common shapes of a cron line back in words and returns
// anything else as written, since a wrong reading is worse than the raw line.
func humanCron(expr string) string {
	f := strings.Fields(expr)
	if len(f) != 5 {
		return expr
	}
	if f[2] == "*" && f[3] == "*" && f[4] == "*" {
		switch {
		case f[0] == "*" && f[1] == "*":
			return "every minute"
		case strings.HasPrefix(f[0], "*/") && f[1] == "*":
			return "every " + f[0][2:] + " minutes"
		case f[0] == "0" && f[1] == "*":
			return "hourly"
		case f[0] == "0" && strings.HasPrefix(f[1], "*/"):
			return "every " + f[1][2:] + " hours"
		}
	}
	min, errM := strconv.Atoi(f[0])
	hour, errH := strconv.Atoi(f[1])
	if errM != nil || errH != nil || min < 0 || hour < 0 || min > 59 || hour > 23 {
		return expr
	}
	at := strings.ToLower(time.Date(2000, 1, 1, hour, min, 0, 0, time.UTC).Format("3:04PM"))
	dom, mon, dow := f[2], f[3], f[4]
	switch {
	case dom == "*" && mon == "*" && dow == "*":
		return "every day " + at
	case dom == "*" && strings.Contains(dow, "#"):
		day, ok := nthDay(dow)
		if !ok {
			return expr
		}
		if mon == "*" {
			return "the " + day + " of every month " + at
		}
		m, err := strconv.Atoi(mon)
		if err != nil || m < 1 || m > 12 {
			return expr
		}
		return "the " + day + " of " + time.Month(m).String()[:3] + " " + at
	case dom == "*" && mon == "*":
		days, ok := dayList(dow)
		if !ok {
			return expr
		}
		return days + " " + at
	case mon == "*" && dow == "*":
		d, err := strconv.Atoi(dom)
		if err != nil || d < 1 || d > 31 {
			return expr
		}
		return "monthly on the " + ordinal(d) + " " + at
	case dow == "*" && strings.HasPrefix(mon, "*/"):
		d, errD := strconv.Atoi(dom)
		n, errN := strconv.Atoi(mon[2:])
		if errD != nil || errN != nil || d < 1 || d > 31 {
			return expr
		}
		return "on the " + ordinal(d) + " every " + strconv.Itoa(n) + " months " + at
	case dow == "*":
		d, errD := strconv.Atoi(dom)
		m, errMo := strconv.Atoi(mon)
		if errD != nil || errMo != nil || m < 1 || m > 12 || d < 1 || d > 31 {
			return expr
		}
		return "every " + time.Month(m).String()[:3] + " " + strconv.Itoa(d) + " " + at
	}
	return expr
}

// nthDay reads thu#4 as "4th Thu" and mon#l as "last Mon".
func nthDay(dow string) (string, bool) {
	name, n, _ := strings.Cut(strings.ToLower(dow), "#")
	d, err := value(name, dayNames)
	if err != nil || d < 0 || d > 7 {
		return "", false
	}
	if n == "l" {
		return "last " + weekdays[d], true
	}
	i, err := strconv.Atoi(n)
	if err != nil || i < 1 || i > 5 {
		return "", false
	}
	return ordinal(i) + " " + weekdays[d], true
}

func dayList(dow string) (string, bool) {
	if dow == "1-5" {
		return "weekdays", true
	}
	if dow == "0,6" || dow == "6,0" {
		return "weekends", true
	}
	var out []string
	for _, part := range strings.Split(dow, ",") {
		if a, b, ok := strings.Cut(part, "-"); ok {
			x, e1 := strconv.Atoi(a)
			y, e2 := strconv.Atoi(b)
			if e1 != nil || e2 != nil || x > 7 || y > 7 || x >= y {
				return "", false
			}
			out = append(out, weekdays[x]+" to "+weekdays[y])
			continue
		}
		d, err := strconv.Atoi(part)
		if err != nil || d > 7 {
			return "", false
		}
		out = append(out, weekdays[d])
	}
	return strings.Join(out, ", "), true
}

func ordinal(n int) string {
	suffix := "th"
	switch {
	case n%100 >= 11 && n%100 <= 13:
	case n%10 == 1:
		suffix = "st"
	case n%10 == 2:
		suffix = "nd"
	case n%10 == 3:
		suffix = "rd"
	}
	return strconv.Itoa(n) + suffix
}

// hourTick is one hour of the next to fire tile.
type hourTick struct {
	N, Pct int
	Label  string
}

// ahead counts the fires in each of the next 24 hours, for the next to fire
// tile. A minutely cron line is capped so it can't run away.
func (s *site) ahead(jobs []Job, now time.Time) []hourTick {
	hours := make([]int, 24)
	end := now.Add(24 * time.Hour)
	for _, j := range jobs {
		if !j.Enabled || j.NextRun.IsZero() {
			continue
		}
		t := j.NextRun
		for range 24 * 60 {
			if !t.Before(end) {
				break
			}
			if !t.Before(now) {
				hours[int(t.Sub(now)/time.Hour)]++
			}
			if j.Once() {
				break
			}
			next, err := s.sched.NextRun(j, t)
			if err != nil || !next.After(t) {
				break
			}
			t = next
		}
	}
	top := 1
	for _, n := range hours {
		top = max(top, n)
	}
	out := make([]hourTick, 24)
	for i, n := range hours {
		out[i] = hourTick{N: n, Pct: 20 + 80*n/top, Label: s.clock(now.Add(time.Duration(i) * time.Hour))}
	}
	return out
}

// upcoming is a saved job's next n fires.
func (s *site) upcoming(j Job, n int) []time.Time {
	if j.ID == 0 || !j.Enabled || j.NextRun.IsZero() {
		return nil
	}
	out := []time.Time{j.NextRun}
	for !j.Once() && len(out) < n {
		next, err := s.sched.NextRun(j, out[len(out)-1])
		if err != nil || !next.After(out[len(out)-1]) {
			break
		}
		out = append(out, next)
	}
	return out
}
