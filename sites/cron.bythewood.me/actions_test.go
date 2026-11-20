package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"cron.bythewood.me/web"
)

func fakeModel(t *testing.T, notify bool) *Model {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		args, _ := json.Marshal(map[string]any{"notify": notify, "title": "Bundle is back", "message": "In stock at $469"})
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{
			"tool_calls": []any{map[string]any{"function": map[string]any{"name": "report", "arguments": string(args)}}},
		}}}})
	}))
	t.Cleanup(srv.Close)
	return NewModel(srv.URL, "k")
}

// signedIn points the site at a fake auth that takes any cookie, and returns
// its routes with every request carrying one.
func signedIn(t *testing.T, s *site) http.Handler {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	s.auth = web.NewAuthenticatorAt(srv.URL)
	h := s.routes()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.AddCookie(&http.Cookie{Name: web.SessionCookie, Value: "test"})
		h.ServeHTTP(w, r)
	})
}

// A try has nothing to publish with, and that has to stay true.
func TestComposerHoldsNoPublisher(t *testing.T) {
	pub := reflect.TypeOf((*Publisher)(nil)).Elem()
	ty := reflect.TypeOf(composer{})
	for i := range ty.NumField() {
		f := ty.Field(i).Type
		if f.Implements(pub) || reflect.PointerTo(f).Implements(pub) || f.Kind() == reflect.Interface {
			t.Errorf("composer.%s could reach a publisher", ty.Field(i).Name)
		}
	}
}

func TestTryNeverSends(t *testing.T) {
	s, push := newTestSite(t)
	s.sched.model = fakeModel(t, true)
	check, _ := s.create(Input{Name: str("Price watch"), Kind: str("prompt"), Cron: str("0 */4 * * *"), Message: str("look")}, "t")
	note, _ := s.create(Input{Name: str("Bins"), Cron: str("0 20 * * 1,4"), Message: str("Take them out")}, "t")

	tr := s.try(context.Background(), check)
	if !tr.Push || tr.Title != "Bundle is back" || tr.Body != "In stock at $469" || tr.Err != "" {
		t.Errorf("check try %+v", tr)
	}
	if tr := s.try(context.Background(), note); !tr.Push || tr.Body != "Take them out" {
		t.Errorf("reminder try %+v", tr)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/jobs/"+itoa(check.ID)+"/try", nil)
	r.SetPathValue("id", itoa(check.ID))
	s.jobTry(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"push": true`) {
		t.Errorf("try page %d %s", w.Code, w.Body)
	}

	s.sched.model = nil
	if tr := s.try(context.Background(), check); tr.Err == "" || tr.Push {
		t.Errorf("a try with no model %+v", tr)
	}
	if push.count() != 0 {
		t.Errorf("a try pushed %d times", push.count())
	}
	if runs := mustRuns(s); len(runs) != 0 {
		t.Errorf("a try wrote runs %+v", runs)
	}
	var n int
	_ = s.store.db.QueryRow(`SELECT COUNT(*) FROM snoozes`).Scan(&n)
	got, _ := s.store.Job(check.ID)
	if n != 0 || !got.LastRun.IsZero() || got.LastErr != "" {
		t.Errorf("a try left %d snoozes and a job of %+v", n, got)
	}
}

func TestMissedWhileDownAreRecorded(t *testing.T) {
	s, push := newTestSite(t)
	j, _ := s.create(Input{Name: str("daily"), Cron: str("0 9 * * *"), Message: str("m")}, "t")
	s.sched.now = func() time.Time { return j.NextRun.AddDate(0, 0, 2).Add(3 * time.Hour) }
	s.sched.skipStale()
	runs, _ := s.store.FindRuns(RunFilter{Result: "missed", Limit: 50})
	if len(runs) != 3 || push.count() != 0 {
		t.Fatalf("three missed days came to %d rows and %d pushes", len(runs), push.count())
	}
	for _, r := range runs {
		if r.Missed != "down" || r.Result() != "missed" || r.At.In(ny).Hour() != 9 {
			t.Errorf("missed run %+v", r)
		}
	}

	minutely, _ := s.create(Input{Name: str("often"), Cron: str("* * * * *"), Message: str("m")}, "t")
	s.sched.now = func() time.Time { return minutely.NextRun.Add(48 * time.Hour) }
	s.sched.skipStale()
	runs, _ = s.store.FindRuns(RunFilter{Job: minutely.ID, Limit: 500})
	if len(runs) != maxMissed || !strings.Contains(runs[len(runs)-1].Err, "before it") {
		t.Errorf("two days of a minutely job wrote %d rows, oldest %+v", len(runs), runs[len(runs)-1])
	}
}

func TestDroppedPushesAreRecorded(t *testing.T) {
	s, push := newTestSite(t)
	push.err = errors.New("ntfy down")
	j, _ := s.create(Input{Name: str("standup"), Cron: str("0 9 * * *"), Message: str("m")}, "t")
	s.sched.now = func() time.Time { return j.NextRun.Add(30 * time.Second) }
	s.sched.fireDue(context.Background())
	s.sched.wg.Wait()
	runs := mustRuns(s)
	if len(runs) != 1 || runs[0].Missed != "dropped" || !strings.Contains(runs[0].Err, "ntfy down") {
		t.Errorf("a repeating push that never landed %+v", runs)
	}

	once, _ := s.create(Input{Name: str("once"), At: str(time.Now().Add(time.Minute).Format(time.RFC3339)), Message: str("m")}, "t")
	clock := time.Now().Add(2 * time.Minute)
	s.sched.now = func() time.Time { return clock }
	for range maxRetries {
		s.sched.fireDue(context.Background())
		s.sched.wg.Wait()
		clock = clock.Add(retryEvery)
	}
	runs, _ = s.store.FindRuns(RunFilter{Job: once.ID, Limit: 50})
	if len(runs) != maxRetries || runs[0].Missed != "dropped" || runs[1].Missed != "" {
		t.Errorf("a one-shot given up on %+v", runs)
	}
}

func TestRunFilters(t *testing.T) {
	s, _ := newTestSite(t)
	now := time.Now()
	for _, r := range []Run{
		{JobID: 1, Name: "a", At: now, OK: true, Pushed: true},
		{JobID: 1, Name: "a", At: now, OK: true},
		{JobID: 2, Name: "b", At: now, Err: "boom"},
		{JobID: 2, Name: "b", At: now, Err: "missed while down", Missed: "down"},
	} {
		_, _ = s.store.AddRun(r)
	}
	for res, want := range map[string]int{"": 4, "pushed": 1, "quiet": 1, "failed": 1, "missed": 1, "nonsense": 4} {
		if got, _ := s.store.FindRuns(RunFilter{Result: res, Limit: 10}); len(got) != want {
			t.Errorf("%q gave %d", res, len(got))
		}
	}
	if got, _ := s.store.FindRuns(RunFilter{Job: 2, Result: "failed", Limit: 10}); len(got) != 1 {
		t.Errorf("job and result together gave %d", len(got))
	}
	last, _ := s.store.LastRuns(14)
	if len(last[1]) != 2 || len(last[2]) != 2 {
		t.Errorf("last runs per job %+v", last)
	}
}

func TestMoveAndBulk(t *testing.T) {
	s, _ := newTestSite(t)
	h := signedIn(t, s)
	post := func(path string, form url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	at := time.Now().In(ny).AddDate(0, 0, 2).Truncate(time.Minute)
	once, _ := s.create(Input{Name: str("market"), At: str(at.Format(time.RFC3339)), Message: str("m")}, "t")
	rep, _ := s.create(Input{Name: str("bins"), Cron: str("0 20 * * 1"), Message: str("m")}, "t")

	day := at.AddDate(0, 0, 3).Format("2006-01-02")
	if w := post("/jobs/"+itoa(once.ID)+"/move", url.Values{"day": {day}}); w.Code != 200 {
		t.Fatalf("move %d %s", w.Code, w.Body)
	}
	got, _ := s.store.Job(once.ID)
	if got.At.In(ny).Format("2006-01-02 15:04") != day+" "+at.Format("15:04") || !got.NextRun.Equal(got.At) {
		t.Errorf("moved to %s", got.At.In(ny))
	}
	if w := post("/jobs/"+itoa(rep.ID)+"/move", url.Values{"day": {day}}); w.Code != 400 {
		t.Errorf("a repeating job moved, %d", w.Code)
	}
	if w := post("/jobs/"+itoa(once.ID)+"/move", url.Values{"day": {"2020-01-01"}}); w.Code != 400 {
		t.Errorf("moved into the past, %d", w.Code)
	}

	post("/jobs/bulk", url.Values{"do": {"pause"}, "id": {itoa(once.ID), itoa(rep.ID)}})
	for _, id := range []int64{once.ID, rep.ID} {
		if j, _ := s.store.Job(id); j.Enabled {
			t.Errorf("%s still on after a bulk pause", j.Name)
		}
	}
	post("/jobs/bulk", url.Values{"do": {"resume"}, "id": {itoa(rep.ID)}})
	if j, _ := s.store.Job(rep.ID); !j.Enabled || j.NextRun.IsZero() {
		t.Errorf("bulk resume left %+v", j)
	}
	if w := post("/jobs/"+itoa(rep.ID)+"/pause", nil); w.Code != 303 || !strings.Contains(w.Header().Get("Location"), "paused=") {
		t.Errorf("pause %d %s", w.Code, w.Header().Get("Location"))
	}
}

func TestSnoozeKnowsItsJob(t *testing.T) {
	s, push := newTestSite(t)
	j, _ := s.create(Input{Name: str("plants"), Cron: str("0 8 * * 6"), Message: str("water")}, "t")
	s.runNow(context.Background(), j)
	action := push.sent[0].Action()
	tok := action[strings.Index(action, "/snooze/")+len("/snooze/") : strings.Index(action, ", method")]
	n, ok := s.store.TakeSnooze(tok, time.Now())
	if !ok || n.JobID != j.ID {
		t.Fatalf("snooze %+v %v", n, ok)
	}
	sj, _ := s.sched.Snoozed(n)
	got, _ := s.store.Job(sj.ID)
	if got.SnoozeOf != j.ID {
		t.Errorf("snoozed job points at %d", got.SnoozeOf)
	}
}
