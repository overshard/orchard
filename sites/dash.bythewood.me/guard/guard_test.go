package guard

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func one(p Policy) (*Guard, Memory) {
	m := Memory{}
	return New(m, func(string) (Policy, bool) { return p, true }), m
}

func TestTakePacesWithoutCounting(t *testing.T) {
	g, m := one(Policy{Gap: 2 * time.Second, Budget: 10, Window: time.Hour})
	if wait, err := g.Take("k", t0); wait != 0 || err != nil {
		t.Fatalf("first call: %s, %v", wait, err)
	}
	if wait, _ := g.Take("k", t0.Add(500*time.Millisecond)); wait != 1500*time.Millisecond {
		t.Errorf("a call inside the gap waits %s, want 1.5s", wait)
	}
	if m["k"].Count != 1 {
		t.Errorf("a paced call was counted, count is %d", m["k"].Count)
	}
	if wait, _ := g.Take("k", t0.Add(2*time.Second)); wait != 0 {
		t.Errorf("a call at the gap still waits %s", wait)
	}
}

func TestJitterStaysInsideAFifth(t *testing.T) {
	p := Policy{Gap: 100 * time.Millisecond, Jitter: true}
	s := State{LastCall: t0}
	for i := 0; i < 200; i++ {
		if w := pace(p, s, t0); w < p.Gap || w > p.Gap+p.Gap/5 {
			t.Fatalf("paced %s, want 100ms to 120ms", w)
		}
	}
}

func TestBudgetIsAFixedWindow(t *testing.T) {
	g, _ := one(Policy{Budget: 2, Window: time.Hour})
	g.Take("k", t0)
	g.Take("k", t0.Add(time.Minute))
	_, err := g.Take("k", t0.Add(59*time.Minute))
	var spent *SpentError
	if !errors.As(err, &spent) || spent.Used != 2 || spent.Budget != 2 {
		t.Fatalf("the third call in the window got %v", err)
	}
	if _, err := g.Take("k", t0.Add(time.Hour)); err != nil {
		t.Errorf("the window an hour after its first call did not roll: %v", err)
	}
}

func TestUnknownKeyIsRefused(t *testing.T) {
	g := New(Memory{}, func(string) (Policy, bool) { return Policy{}, false })
	if _, err := g.Take("nobody", t0); !errors.Is(err, ErrUnknown) {
		t.Errorf("got %v, want ErrUnknown", err)
	}
}

func TestFailuresTripAfterTheStreak(t *testing.T) {
	g, _ := one(Policy{TripAfter: 3, OpenFor: time.Minute, MaxOpen: time.Hour})
	for i := 0; i < 2; i++ {
		if d := g.Fail("k", t0, Failure{Kind: Failed}); d != 0 {
			t.Fatalf("failure %d opened the breaker for %s", i+1, d)
		}
	}
	if d := g.Fail("k", t0, Failure{Kind: Failed}); d != time.Minute {
		t.Fatalf("the third failure opened it for %s, want 1m", d)
	}
	_, err := g.Take("k", t0.Add(30*time.Second))
	var open *OpenError
	if !errors.As(err, &open) || open.Left != 30*time.Second {
		t.Errorf("got %v, want open for 30s", err)
	}
}

func TestNotedNeverTripsByItself(t *testing.T) {
	g, m := one(Policy{TripAfter: 2, OpenFor: time.Minute})
	for i := 0; i < 5; i++ {
		if d := g.Fail("k", t0, Failure{Kind: Noted, Why: "HTTP 404"}); d != 0 {
			t.Fatalf("a noted failure opened the breaker")
		}
	}
	if m["k"].Fails != 5 {
		t.Errorf("fails is %d, want 5", m["k"].Fails)
	}
	if d := g.Fail("k", t0, Failure{Kind: Failed}); d != time.Minute {
		t.Errorf("a failure on top of the noted ones opened it for %s, want 1m", d)
	}
}

func TestRefusalsDoubleUpToTheCeiling(t *testing.T) {
	g, _ := one(Policy{OpenFor: time.Minute, MaxOpen: 5 * time.Minute})
	var got []time.Duration
	for i := 0; i < 5; i++ {
		got = append(got, g.Fail("k", t0, Failure{Kind: Refused}))
	}
	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 5 * time.Minute, 5 * time.Minute}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("open %d was %s, want %s", i+1, got[i], want[i])
		}
	}
	g.Succeed("k")
	if d := g.Fail("k", t0, Failure{Kind: Refused}); d != time.Minute {
		t.Errorf("a success did not reset the doubling, the next open is %s", d)
	}
}

func TestAZeroCeilingIsFlat(t *testing.T) {
	g, _ := one(Policy{OpenFor: 6 * time.Hour, FailOpenFor: 5 * time.Minute, TripAfter: 1})
	for i := 0; i < 3; i++ {
		if d := g.Fail("k", t0, Failure{Kind: Refused}); d != 6*time.Hour {
			t.Errorf("refusal %d opened for %s, want 6h", i+1, d)
		}
	}
	if d := g.Fail("k", t0, Failure{Kind: Failed}); d != 5*time.Minute {
		t.Errorf("a failure opened for %s, want its own 5m", d)
	}
}

func TestFillOnRefusalSetsTheStreak(t *testing.T) {
	for _, fill := range []bool{false, true} {
		g, m := one(Policy{TripAfter: 5, OpenFor: time.Minute, FillOnRefusal: fill})
		g.Fail("k", t0, Failure{Kind: Noted})
		g.Fail("k", t0, Failure{Kind: Refused})
		want := 2
		if fill {
			want = 5
		}
		if m["k"].Fails != want {
			t.Errorf("fill=%v: fails is %d, want %d", fill, m["k"].Fails, want)
		}
	}
}

func TestRetryAfterLongerWins(t *testing.T) {
	g, _ := one(Policy{OpenFor: time.Minute, MaxOpen: time.Hour, TripAfter: 4})
	if d := g.Fail("k", t0, Failure{Kind: Refused, RetryAfter: 20 * time.Minute}); d != 20*time.Minute {
		t.Errorf("opened for %s, want the 20m asked for", d)
	}
	if d := g.Fail("k", t0, Failure{Kind: Refused, RetryAfter: time.Second}); d != 2*time.Minute {
		t.Errorf("opened for %s, want the 2m backoff over a shorter ask", d)
	}
}

func TestKeepLongerHoldsTheLongerOpen(t *testing.T) {
	for _, keep := range []bool{false, true} {
		g, m := one(Policy{OpenFor: time.Hour, FailOpenFor: time.Minute, TripAfter: 1, KeepLonger: keep})
		g.Fail("k", t0, Failure{Kind: Refused})
		g.Fail("k", t0, Failure{Kind: Failed})
		want := t0.Add(time.Minute)
		if keep {
			want = t0.Add(time.Hour)
		}
		if !m["k"].OpenUntil.Equal(want) {
			t.Errorf("keep=%v: open until %s, want %s", keep, m["k"].OpenUntil, want)
		}
	}
}

// Without HalfOpen an expired open leaves the streak in place, so the next
// failure reopens it. With it the streak starts over.
func TestHalfOpenForgivesTheStreak(t *testing.T) {
	for _, half := range []bool{false, true} {
		g, m := one(Policy{OpenFor: time.Minute, MaxOpen: time.Hour, TripAfter: 2, HalfOpen: half})
		g.Fail("k", t0, Failure{Kind: Failed})
		g.Fail("k", t0, Failure{Kind: Failed})
		later := t0.Add(2 * time.Minute)
		if _, err := g.Take("k", later); err != nil {
			t.Fatalf("half=%v: an expired open refused: %v", half, err)
		}
		if half != m["k"].OpenUntil.IsZero() {
			t.Errorf("half=%v: open until %s after it expired", half, m["k"].OpenUntil)
		}
		d := g.Fail("k", later, Failure{Kind: Failed})
		if half && d != 0 {
			t.Errorf("half open reopened on one failure for %s", d)
		}
		if !half && d != 2*time.Minute {
			t.Errorf("closed reopened for %s, want the doubled 2m", d)
		}
	}
}

func TestWaitSleepsOutThePaceAndHonoursTheContext(t *testing.T) {
	g, m := one(Policy{Gap: 80 * time.Millisecond})
	if err := g.Wait(t.Context(), "k", time.Now()); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := g.Wait(t.Context(), "k", time.Now()); err != nil {
		t.Fatal(err)
	}
	if waited := time.Since(start); waited < 80*time.Millisecond {
		t.Errorf("waited %s, want the 80ms gap", waited)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	before := m["k"]
	if err := g.Wait(ctx, "k", time.Now()); !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled wait returned %v", err)
	}
	if m["k"] != before {
		t.Error("a cancelled wait was counted")
	}
}

func TestOpenEachAndPut(t *testing.T) {
	g, _ := one(Policy{})
	g.Put("a", State{OpenUntil: t0.Add(time.Hour), Trips: 3})
	g.Put("b", State{})
	if open, left := g.Open("a", t0); !open || left != time.Hour {
		t.Errorf("a is open=%v for %s", open, left)
	}
	if open, _ := g.Open("b", t0); open {
		t.Error("b reads as open")
	}
	n := 0
	g.Each(func(string, State) { n++ })
	if n != 2 {
		t.Errorf("Each saw %d keys, want 2", n)
	}
}

func TestBackoff(t *testing.T) {
	cases := []struct {
		base, ceiling time.Duration
		trips         int
		want          time.Duration
	}{
		{30 * time.Second, 30 * time.Minute, 0, 30 * time.Second},
		{30 * time.Second, 30 * time.Minute, 1, 30 * time.Second},
		{30 * time.Second, 30 * time.Minute, 6, 16 * time.Minute},
		{30 * time.Second, 30 * time.Minute, 7, 30 * time.Minute},
		{10 * time.Minute, 8 * time.Hour, 7, 8 * time.Hour},
		{6 * time.Hour, 0, 9, 6 * time.Hour},
	}
	for _, c := range cases {
		if got := Backoff(c.base, c.ceiling, c.trips); got != c.want {
			t.Errorf("Backoff(%s, %s, %d) = %s, want %s", c.base, c.ceiling, c.trips, got, c.want)
		}
	}
}

func TestRetryAfterReadsBothForms(t *testing.T) {
	cases := map[string]time.Duration{
		"120":   2 * time.Minute,
		" 120 ": 2 * time.Minute,
		"":      0,
		"later": 0,
		"-5":    0,
		t0.Add(10 * time.Minute).Format(http.TimeFormat): 10 * time.Minute,
		t0.Add(-time.Hour).Format(http.TimeFormat):       0,
	}
	for in, want := range cases {
		if got := RetryAfter(in, t0); got != want {
			t.Errorf("RetryAfter(%q) = %s, want %s", in, got, want)
		}
	}
}

// What a server sends is what Fail is handed, through a real response, in
// both forms.
func TestRetryAfterOffTheWire(t *testing.T) {
	now := time.Now()
	for _, v := range []string{"600", now.Add(10 * time.Minute).UTC().Format(http.TimeFormat)} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", v)
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		resp, err := http.Get(srv.URL)
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		got := RetryAfter(resp.Header.Get("Retry-After"), now)
		if got < 9*time.Minute || got > 10*time.Minute {
			t.Errorf("Retry-After %q read as %s, want about 10m", v, got)
		}
		g, _ := one(Policy{OpenFor: time.Minute})
		if d := g.Fail("k", now, Failure{Kind: Refused, RetryAfter: got, Why: "HTTP " + strconv.Itoa(resp.StatusCode)}); d != got {
			t.Errorf("opened for %s, want %s", d, got)
		}
	}
}
