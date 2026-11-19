package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"cron.bythewood.me/suite"
)

// chrome is the header and status line for a signed in page. on names the
// page tab that is lit.
func (s *site) chrome(on string, jobs []Job) suite.Page {
	p := suite.Page{Tool: "cron", SignedIn: true, Source: sourceURL, Author: author, Year: time.Now().Year()}
	for _, t := range []suite.Tab{{Name: "jobs", Href: "/"}, {Name: "runs", Href: "/runs"}, {Name: "new", Href: "/jobs/new"}, {Name: "keys", Href: "/keys"}} {
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
	Pushed, Failed, Quiet       []float64
	PushedWeek, PushedPrev      int
	FailedWeek, RunsWeek, Total int
}

func (s *site) activity(runs []Run) activity {
	const days = 14
	now := time.Now().In(s.loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.loc)
	a := activity{Pushed: make([]float64, days), Failed: make([]float64, days), Quiet: make([]float64, days)}
	for _, r := range runs {
		at := r.At.In(s.loc)
		day := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, s.loc)
		i := days - 1 - int(today.Sub(day).Hours()/24+0.5)
		if i < 0 || i >= days {
			continue
		}
		a.Total++
		switch {
		case r.Err != "":
			a.Failed[i]++
		case r.Pushed:
			a.Pushed[i]++
		default:
			a.Quiet[i]++
		}
		if i >= days-7 {
			a.RunsWeek++
			if r.Err != "" {
				a.FailedWeek++
			} else if r.Pushed {
				a.PushedWeek++
			}
		} else if r.Pushed && r.Err == "" {
			a.PushedPrev++
		}
	}
	return a
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
		out[i] = a.Pushed[i] + a.Failed[i] + a.Quiet[i]
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
