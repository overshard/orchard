package tools

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// pinRewind moves one host's clock back, so a test can step past a gap or a
// box without waiting it out.
func pinRewind(g *Guard, host string, d time.Duration) {
	s := g.boxes.mem[host]
	if !s.LastCall.IsZero() {
		s.LastCall = s.LastCall.Add(-d)
	}
	if !s.OpenUntil.IsZero() {
		s.OpenUntil = s.OpenUntil.Add(-d)
	}
	g.boxes.mem[host] = s
}

type pinReply struct {
	status int
	header map[string]string
	body   string
}

func pinUpstream(t *testing.T, replies ...pinReply) (*httptest.Server, *int) {
	t.Helper()
	var mu sync.Mutex
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if hits >= len(replies) {
			t.Errorf("an unscripted request reached the upstream")
			w.WriteHeader(http.StatusTeapot)
			return
		}
		rep := replies[hits]
		hits++
		for k, v := range rep.header {
			w.Header().Set(k, v)
		}
		w.WriteHeader(rep.status)
		fmt.Fprint(w, rep.body)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

var (
	pinDuration = regexp.MustCompile(`\b(\d+h)?(\d+m)?\d+(\.\d+)?(s|ms|µs)\b`)
	pinHostPort = regexp.MustCompile(`127\.0\.0\.1:\d{2,}`)
)

// pinNormal rounds every duration in s, to 10ms for a short one and ten
// seconds for the rest, since a box's time left has moved on by the time it is read.
func pinNormal(s string) string {
	s = pinHostPort.ReplaceAllString(s, "HOST")
	return pinDuration.ReplaceAllStringFunc(s, func(m string) string {
		d, err := time.ParseDuration(m)
		if err != nil {
			return m
		}
		if d < 10*time.Second {
			return d.Round(10 * time.Millisecond).String()
		}
		return d.Round(10 * time.Second).String()
	})
}

func pinCompare(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") == strings.Join(want, "\n") {
		return
	}
	t.Errorf("the sequence changed.\ngot:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
}

type pinStore struct {
	mu  sync.Mutex
	log []string
}

func (p *pinStore) SavePenalty(host string, till time.Time, trips int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.log = append(p.log, pinNormal(fmt.Sprintf("save %s trips=%d for=%s", host, trips, time.Until(till))))
}

func (p *pinStore) ClearPenalty(host string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.log = append(p.log, pinNormal("clear "+host))
}

func pinDown(g *Guard) string {
	var out []string
	for host, left := range g.Down() {
		out = append(out, host+"="+left.String())
	}
	sort.Strings(out)
	return pinNormal(strings.TrimSpace("down " + strings.Join(out, " ")))
}

// The penalty box through the real fetch path, against a host that answers
// every way the box cares about, Retry-After included, which it ignores.
func TestGuardSequenceIsPinned(t *testing.T) {
	date := time.Now().Add(2 * time.Hour).UTC().Format(http.TimeFormat)
	srv, hits := pinUpstream(t,
		pinReply{status: 200, body: "hello"},
		pinReply{status: 500, header: map[string]string{"Retry-After": "60"}},
		pinReply{status: 404},
		pinReply{status: 403},
		pinReply{status: 429, header: map[string]string{"Retry-After": "30"}},
		pinReply{status: 202, header: map[string]string{"Retry-After": date}},
		pinReply{status: 200, body: "  \n "},
		pinReply{status: 200, body: "ok"},
		pinReply{status: 503, header: map[string]string{"Retry-After": date}},
		pinReply{status: 429, header: map[string]string{"Retry-After": "7200"}},
	)
	store := &pinStore{}
	d := &Deps{HTTP: &http.Client{Timeout: 5 * time.Second}, Now: time.Now, Guard: NewGuard(time.Minute), Budgets: NewBudgets()}
	d.Guard.Restore(store, nil)
	host := hostOf(srv.URL)
	dead := "http://127.0.0.1:1/"

	var got []string
	call := func(url string) {
		before := *hits
		b, err := getWith(t.Context(), d, url, "", nil)
		line := fmt.Sprintf("sent=%v ", *hits > before)
		if err != nil {
			line += "err=" + err.Error()
		} else {
			line += "ok " + string(b)
		}
		got = append(got, pinNormal(line))
	}

	call(srv.URL)
	pinRewind(d.Guard, host, time.Second)
	call(srv.URL)
	pinRewind(d.Guard, host, time.Second)
	call(srv.URL)
	pinRewind(d.Guard, host, time.Second)
	call(srv.URL)
	got = append(got, pinDown(d.Guard))
	pinRewind(d.Guard, host, time.Second)
	call(srv.URL)
	call(srv.URL)
	got = append(got, pinDown(d.Guard))
	pinRewind(d.Guard, host, 6*time.Hour)
	call(srv.URL)
	call(srv.URL)
	pinRewind(d.Guard, host, 6*time.Hour)
	call(srv.URL)
	call(srv.URL)
	pinRewind(d.Guard, host, 5*time.Minute)
	call(srv.URL)
	pinRewind(d.Guard, host, time.Second)
	call(srv.URL)
	call(dead)
	call(dead)
	got = append(got, pinDown(d.Guard))
	pinRewind(d.Guard, host, time.Second)
	call(srv.URL)
	got = append(got, pinDown(d.Guard))

	pinCompare(t, got, []string{
		"sent=true ok hello",
		"sent=true err=HOST answered 500. That site is having trouble of its own. Use a different source.",
		"sent=true err=HOST answered 404. There is no page at that address. Do not guess another one, search for the page and fetch the url the search returns.",
		"sent=true err=HOST answered 403. That site blocks automated readers, and asking again will not change it. Use a different source.",
		"down",
		"sent=true err=HOST is rate limiting us (status 429)",
		"sent=false err=HOST refused us and is being left alone for another 6h0m0s, so nothing can be looked up there until then",
		"down HOST=6h0m0s",
		"sent=true err=HOST is rate limiting us (status 202)",
		"sent=false err=HOST refused us and is being left alone for another 6h0m0s, so nothing can be looked up there until then",
		"sent=true err=HOST returned an empty page, so try a different source",
		"sent=false err=HOST refused us and is being left alone for another 5m0s, so nothing can be looked up there until then",
		"sent=true ok ok",
		"sent=true err=HOST answered 503. That site is having trouble of its own. Use a different source.",
		"sent=false err=127.0.0.1:1 did not answer, so try a different source: nothing is listening there",
		"sent=false err=127.0.0.1:1 refused us and is being left alone for another 5m0s, so nothing can be looked up there until then",
		"down 127.0.0.1:1=5m0s",
		"sent=true err=HOST is rate limiting us (status 429)",
		"down 127.0.0.1:1=5m0s HOST=6h0m0s",
	})
	pinCompare(t, store.log, []string{
		"save HOST trips=1 for=6h0m0s",
		"save HOST trips=2 for=6h0m0s",
		"save HOST trips=3 for=5m0s",
		"clear HOST",
		"save 127.0.0.1:1 trips=1 for=5m0s",
		"save HOST trips=1 for=6h0m0s",
	})
}

// The box keeps the longer of what it holds and what it is given, and a call
// that worked clears the streak and leaves the box alone.
func TestGuardBoxesArePinned(t *testing.T) {
	store := &pinStore{}
	g := NewGuard(time.Minute)
	g.Restore(store, map[string][2]int64{
		"restored.example": {time.Now().Add(90 * time.Minute).UnixMilli(), 4},
		"expired.example":  {time.Now().Add(-time.Minute).UnixMilli(), 2},
	})
	var got []string
	blocked := func(host string) {
		b, left := g.Blocked(host)
		got = append(got, pinNormal(fmt.Sprintf("%s blocked=%v left=%s", host, b, left)))
	}

	blocked("restored.example")
	blocked("expired.example")
	g.Trip("a.example")
	g.Stumble("a.example")
	blocked("a.example")
	g.Stumble("b.example")
	blocked("b.example")
	g.Trip("b.example")
	blocked("b.example")
	g.Stumble("c.example")
	g.Stumble("c.example")
	g.Stumble("c.example")
	blocked("c.example")
	g.OK("c.example")
	blocked("c.example")
	g.OK("c.example")
	g.OK("never.example")
	g.OK("expired.example")
	got = append(got, pinDown(g))

	pinCompare(t, got, []string{
		"restored.example blocked=true left=1h30m0s",
		"expired.example blocked=false left=0s",
		"a.example blocked=true left=6h0m0s",
		"b.example blocked=true left=5m0s",
		"b.example blocked=true left=6h0m0s",
		"c.example blocked=true left=5m0s",
		"c.example blocked=true left=5m0s",
		"down a.example=6h0m0s b.example=6h0m0s c.example=5m0s restored.example=1h30m0s",
	})
	pinCompare(t, store.log, []string{
		"save a.example trips=1 for=6h0m0s",
		"save a.example trips=2 for=6h0m0s",
		"save b.example trips=1 for=5m0s",
		"save b.example trips=2 for=6h0m0s",
		"save c.example trips=1 for=5m0s",
		"save c.example trips=2 for=5m0s",
		"save c.example trips=3 for=5m0s",
		"clear c.example",
		"clear expired.example",
	})
}

// The gap is per host, and the lock is held across the wait, so a call to any
// host waits behind one that is sleeping out its gap.
func TestGuardPaceIsPinned(t *testing.T) {
	g := NewGuard(time.Minute)
	g.Wait("api.open-meteo.com")
	start := time.Now()
	g.Wait("api.open-meteo.com")
	if waited := time.Since(start); waited < 450*time.Millisecond || waited > 900*time.Millisecond {
		t.Errorf("the second call waited %s, want the 500ms gap", waited)
	}

	g.Wait("www.pollen.com")
	done := make(chan struct{})
	go func() {
		g.Wait("www.pollen.com")
		close(done)
	}()
	time.Sleep(100 * time.Millisecond)
	start = time.Now()
	g.Blocked("elsewhere.example")
	if waited := time.Since(start); waited < time.Second {
		t.Errorf("a check on another host took %s, want it held behind the sleeping gap", waited)
	}
	<-done
}

// Every host's gap, which is the guard's pace, so changing one means changing
// this test too.
func TestGuardGapsArePinned(t *testing.T) {
	want := map[string]time.Duration{
		SearchHost:                       6 * time.Second,
		"cdn.espn.com":                   3 * time.Second,
		"query1.finance.yahoo.com":       3 * time.Second,
		"api.coingecko.com":              2 * time.Second,
		"query2.finance.yahoo.com":       3 * time.Second,
		"api.open-meteo.com":             500 * time.Millisecond,
		"air-quality-api.open-meteo.com": 500 * time.Millisecond,
		"www.pollen.com":                 2 * time.Second,
		"nominatim.openstreetmap.org":    1500 * time.Millisecond,
		"anything.else":                  400 * time.Millisecond,
	}
	for host, gap := range want {
		if got := budgetFor(host).gap; got != gap {
			t.Errorf("%s paces at %s, want %s", host, got, gap)
		}
	}
	if refusalCool != 6*time.Hour || stumbleCool != 5*time.Minute {
		t.Errorf("boxes are %s and %s", refusalCool, stumbleCool)
	}
}
