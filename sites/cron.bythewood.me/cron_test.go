package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"cron.bythewood.me/ntfy"
	"cron.bythewood.me/suite"
	"cron.bythewood.me/web"
)

var ny, _ = time.LoadLocation(zone)

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, ny)
	if err != nil {
		panic(err)
	}
	return t
}

func TestCronNext(t *testing.T) {
	cases := []struct {
		line, after, want string
	}{
		{"0 9 * * *", "2026-11-18 08:59", "2026-11-18 09:00"},
		{"0 9 * * *", "2026-11-18 09:00", "2026-11-19 09:00"},
		{"*/15 * * * *", "2026-11-18 10:01", "2026-11-18 10:15"},
		{"0 9 * * 1-5", "2026-11-21 10:00", "2026-11-23 09:00"},
		{"0 9 * * mon-fri", "2026-11-21 10:00", "2026-11-23 09:00"},
		{"0 9 3 3 *", "2026-11-18 00:00", "2027-03-03 09:00"},
		{"0 9 29 feb *", "2026-11-18 00:00", "2028-02-29 09:00"},
		{"0 8 * * 7", "2026-11-18 00:00", "2026-11-22 08:00"},
		{"@daily", "2026-11-18 12:00", "2026-11-19 00:00"},
		{"0 9 * 11 thu#4", "2026-11-23 00:00", "2026-11-26 09:00"},
		{"0 9 * 11 thu#4", "2026-11-26 09:00", "2027-11-25 09:00"},
		{"0 9 * 5 mon#l", "2026-11-23 00:00", "2027-05-31 09:00"},
		{"0 9 * 5 1#l", "2027-06-01 00:00", "2028-05-29 09:00"},
		{"0 9 * 9 mon#1", "2026-11-23 00:00", "2027-09-06 09:00"},
		{"0 9 * * sun#2,6", "2026-11-23 00:00", "2026-11-28 09:00"},
		// Both day fields named means either one matches, as in every cron.
		{"0 9 1 * fri", "2026-11-18 00:00", "2026-11-20 09:00"},
		// The spring change skips 2:30 on March 14 2027, so it runs the next day.
		{"30 2 * * *", "2027-03-13 03:00", "2027-03-15 02:30"},
		// The fall change repeats 1:30 on November 7 2027, and it runs once.
		{"30 1 * * *", "2027-11-07 00:00", "2027-11-07 01:30"},
	}
	for _, c := range cases {
		e, err := ParseCron(c.line)
		if err != nil {
			t.Fatalf("%q: %v", c.line, err)
		}
		got, ok := e.Next(at(c.after), ny)
		if !ok || !got.Equal(at(c.want)) {
			t.Errorf("%q after %s = %s, want %s", c.line, c.after, got, c.want)
		}
	}

	e, _ := ParseCron("30 1 * * *")
	first, _ := e.Next(at("2027-11-07 00:00"), ny)
	second, _ := e.Next(first, ny)
	if second.Day() != 8 {
		t.Errorf("the repeated hour fired twice, next was %s", second)
	}
}

func TestCronRejects(t *testing.T) {
	for _, line := range []string{"", "* * * *", "60 * * * *", "* 24 * * *", "0 9 * * thu#6", "0 9 * * #2", "0 0 0 * *", "0 0 * 13 *", "*/0 * * * *", "a b c d e", "5-1 * * * *"} {
		if _, err := ParseCron(line); err == nil {
			t.Errorf("%q parsed", line)
		}
	}
	e, _ := ParseCron("0 0 31 2 *")
	if _, ok := e.Next(time.Now(), ny); ok {
		t.Error("February 31st matched")
	}
}

type fakePush struct {
	mu   sync.Mutex
	sent []ntfy.Message
	err  error
}

func (f *fakePush) Publish(_ context.Context, m ntfy.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, m)
	return nil
}

func (f *fakePush) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

func newTestSite(t *testing.T) (*site, *fakePush) {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "cron.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	push := &fakePush{}
	s := &site{auth: web.NewAuthenticatorAt("http://127.0.0.1:1/verify"), store: store, loc: ny}
	s.sched = NewScheduler(store, push, nil, ny)
	s.sched.waits = nil
	if err := s.loadTemplates(); err != nil {
		t.Fatal(err)
	}
	return s, push
}

func str(v string) *string { return &v }

func TestOneShotFiresAndIsRemoved(t *testing.T) {
	s, push := newTestSite(t)
	now := time.Now()
	j, err := s.create(Input{Name: str("Switch"), At: str(now.Add(time.Minute).Format(time.RFC3339)), Message: str("buy it")}, "test")
	if err != nil {
		t.Fatal(err)
	}
	s.sched.now = func() time.Time { return now.Add(2 * time.Minute) }
	s.sched.fireDue(context.Background())
	s.sched.wg.Wait()

	if push.count() != 1 || push.sent[0].Title != "Switch" || push.sent[0].Body != "buy it" {
		t.Fatalf("sent %+v", push.sent)
	}
	if !strings.Contains(push.sent[0].Action(), "/snooze/") {
		t.Errorf("no snooze button in %q", push.sent[0].Action())
	}
	if _, err := s.store.Job(j.ID); err == nil {
		t.Error("the one-shot is still there after it fired")
	}
	runs, _ := s.store.Runs(0, 10)
	if len(runs) != 1 || !runs[0].Pushed || runs[0].Name != "Switch" {
		t.Errorf("runs %+v", runs)
	}
}

func TestOneShotRetriesThenGivesUp(t *testing.T) {
	s, push := newTestSite(t)
	push.err = errors.New("ntfy down")
	now := time.Now()
	j, _ := s.create(Input{Name: str("x"), At: str(now.Add(time.Minute).Format(time.RFC3339)), Message: str("m")}, "test")
	clock := now.Add(2 * time.Minute)
	s.sched.now = func() time.Time { return clock }
	for i := range maxRetries {
		s.sched.fireDue(context.Background())
		s.sched.wg.Wait()
		got, err := s.store.Job(j.ID)
		if i < maxRetries-1 && (err != nil || got.Failures != i+1) {
			t.Fatalf("try %d: %+v %v", i, got, err)
		}
		clock = clock.Add(retryEvery)
	}
	if _, err := s.store.Job(j.ID); err == nil {
		t.Error("still there after every retry failed")
	}
}

func TestRepeatingJobAdvances(t *testing.T) {
	s, push := newTestSite(t)
	j, err := s.create(Input{Name: str("standup"), Cron: str("0 9 * * *"), Message: str("m")}, "test")
	if err != nil {
		t.Fatal(err)
	}
	fire := j.NextRun
	s.sched.now = func() time.Time { return fire.Add(30 * time.Second) }
	s.sched.fireDue(context.Background())
	s.sched.wg.Wait()
	got, _ := s.store.Job(j.ID)
	if push.count() != 1 || !got.NextRun.Equal(fire.AddDate(0, 0, 1)) {
		t.Errorf("pushed %d, next %s", push.count(), got.NextRun)
	}
}

func TestStaleRepeatIsSkipped(t *testing.T) {
	s, push := newTestSite(t)
	j, _ := s.create(Input{Name: str("daily"), Cron: str("0 9 * * *"), Message: str("m")}, "test")
	s.sched.now = func() time.Time { return j.NextRun.Add(3 * time.Hour) }
	s.sched.skipStale()
	s.sched.fireDue(context.Background())
	s.sched.wg.Wait()
	if push.count() != 0 {
		t.Error("a run missed by hours fired anyway")
	}
}

// A birthday is a yearly line, so a push lost to an ntfy stall has to be tried
// again within minutes and not next year.
func TestDroppedRepeatRetriesSoon(t *testing.T) {
	s, push := newTestSite(t)
	push.err = errors.New("ntfy down")
	j, _ := s.create(Input{Name: str("birthday"), Cron: str("0 9 3 12 *"), Message: str("m")}, "test")
	clock := j.NextRun.Add(30 * time.Second)
	s.sched.now = func() time.Time { return clock }
	s.sched.fireDue(context.Background())
	s.sched.wg.Wait()

	got, _ := s.store.Job(j.ID)
	if want := clock.Add(retryEvery); !got.NextRun.Equal(want.Truncate(time.Second)) && !got.NextRun.Equal(want) {
		t.Fatalf("next run %s, want a retry at %s", got.NextRun, want)
	}

	push.err = nil
	clock = got.NextRun.Add(time.Second)
	s.sched.fireDue(context.Background())
	s.sched.wg.Wait()
	got, _ = s.store.Job(j.ID)
	if push.count() != 1 || got.Failures != 0 || !got.NextRun.Equal(j.NextRun.AddDate(1, 0, 0)) {
		t.Errorf("pushed %d, failures %d, next %s", push.count(), got.Failures, got.NextRun)
	}
}

func TestStaleYearlyStillFires(t *testing.T) {
	s, push := newTestSite(t)
	j, _ := s.create(Input{Name: str("birthday"), Cron: str("0 9 3 12 *"), Message: str("m")}, "test")
	s.sched.now = func() time.Time { return j.NextRun.Add(5 * time.Hour) }
	s.sched.skipStale()
	s.sched.fireDue(context.Background())
	s.sched.wg.Wait()
	if push.count() != 1 || !strings.Contains(push.sent[0].Body, "late by") {
		t.Fatalf("sent %+v, want one push that says it is late", push.sent)
	}
}

func TestIDsAreNotReused(t *testing.T) {
	s, _ := newTestSite(t)
	a, _ := s.create(Input{Name: str("a"), Cron: str("0 9 * * *"), Message: str("m")}, "t")
	_ = s.store.Delete(a.ID)
	b, _ := s.create(Input{Name: str("b"), Cron: str("0 9 * * *"), Message: str("m")}, "t")
	if b.ID == a.ID {
		t.Error("a deleted job's id was reused")
	}
}

func TestApplyChecks(t *testing.T) {
	s, _ := newTestSite(t)
	bad := []Input{
		{Message: str("m"), At: str("2030-01-01T09:00")},
		{Name: str("n"), At: str("2030-01-01T09:00")},
		{Name: str("n"), Message: str("m")},
		{Name: str("n"), Message: str("m"), At: str("2020-01-01T09:00")},
		{Name: str("n"), Message: str("m"), Cron: str("61 * * * *")},
		{Name: str("n"), Message: str("m"), Cron: str("* * * * *"), Kind: str("script")},
		{Name: str("n"), Message: str("m"), Cron: str("* * * * *"), Priority: str("loud")},
		{Name: str("n"), Message: str("m"), At: str("tomorrow")},
	}
	for _, in := range bad {
		if _, err := s.create(in, "t"); err == nil {
			t.Errorf("accepted %+v", in)
		}
	}
	j, err := s.create(Input{Name: str("n"), Message: str("m"), At: str("2030-01-01T09:00")}, "t")
	if err != nil || !j.At.Equal(time.Date(2030, 1, 1, 9, 0, 0, 0, ny)) {
		t.Errorf("a bare time isn't read as Eastern: %s %v", j.At, err)
	}
}

func TestAPI(t *testing.T) {
	s, push := newTestSite(t)
	key, _ := s.store.NewKey("claude")
	h := s.routes()
	call := func(method, path, body, k string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if k != "" {
			r.Header.Set("Authorization", "Bearer "+k)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	if w := call("GET", "/api/jobs", "", "cron-nope"); w.Code != 401 {
		t.Fatalf("a bad key got %d", w.Code)
	}
	w := call("POST", "/api/jobs", `{"name":"GTA","at":"2030-11-20T18:00","message":"reviews"}`, key)
	if w.Code != 201 {
		t.Fatalf("create %d %s", w.Code, w.Body)
	}
	var j jobJSON
	_ = json.Unmarshal(w.Body.Bytes(), &j)
	if j.CreatedBy != "claude" || j.Schedule != "once" || !strings.HasSuffix(j.NextRun, "-05:00") {
		t.Errorf("created %+v", j)
	}
	id := "/api/jobs/" + itoa(j.ID)
	if w := call("PATCH", id, `{"cron":"0 18 * * 5"}`, key); w.Code != 200 || !strings.Contains(w.Body.String(), `"schedule": "0 18 * * 5"`) {
		t.Errorf("patch %d %s", w.Code, w.Body)
	}
	if w := call("POST", id+"/run", "", key); w.Code != 200 || push.count() != 1 {
		t.Errorf("run %d, pushed %d", w.Code, push.count())
	}
	if w := call("GET", "/api/preview?cron=0+9+3+3+*", "", key); w.Code != 200 || strings.Count(w.Body.String(), `"at"`) != 5 {
		t.Errorf("preview %d %s", w.Code, w.Body)
	}
	w = call("GET", "/api/preview?when=every+weekday+at+7:30am", "", key)
	var p previewJSON
	_ = json.Unmarshal(w.Body.Bytes(), &p)
	if w.Code != 200 || p.Cron != "30 7 * * 1-5" || len(p.Next) != 5 || len(p.Days) != 14 {
		t.Errorf("preview of plain words %d %s", w.Code, w.Body)
	}
	fires := 0
	for _, d := range p.Days {
		fires += d.Fires
	}
	if fires != 10 && fires != 9 && fires != 11 {
		t.Errorf("two weeks of weekdays came to %d fires", fires)
	}
	if w := call("GET", "/api/preview?when=whenever", "", key); w.Code != 400 || !strings.Contains(w.Body.String(), "couldn't read") {
		t.Errorf("unreadable words %d %s", w.Code, w.Body)
	}
	w = call("POST", "/api/jobs", `{"name":"Plants","when":"saturdays at 8am","message":"water"}`, key)
	if w.Code != 201 || !strings.Contains(w.Body.String(), `"cron": "0 8 * * 6"`) {
		t.Errorf("create from plain words %d %s", w.Code, w.Body)
	}
	if w := call("POST", "/api/jobs", `{"name":"x","nope":1}`, key); w.Code != 400 {
		t.Errorf("an unknown field got %d", w.Code)
	}
	if w := call("DELETE", id, "", key); w.Code != 200 {
		t.Errorf("delete %d", w.Code)
	}
	if w := call("GET", id, "", key); w.Code != 404 {
		t.Errorf("deleted job answered %d", w.Code)
	}
}

func TestSnoozeIsSingleUse(t *testing.T) {
	s, push := newTestSite(t)
	j, _ := s.create(Input{Name: str("n"), Cron: str("0 9 * * *"), Message: str("m")}, "t")
	s.runNow(context.Background(), j)
	action := push.sent[0].Action()
	tok := action[strings.Index(action, "/snooze/")+len("/snooze/") : strings.Index(action, ", method")]

	h := s.routes()
	for i, want := range []int{200, 404} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/snooze/"+tok, nil))
		if w.Code != want {
			t.Errorf("press %d got %d", i+1, w.Code)
		}
	}
	jobs, _ := s.store.Jobs()
	if len(jobs) != 2 || jobs[0].CreatedBy != "snooze" && jobs[1].CreatedBy != "snooze" {
		t.Errorf("no snoozed job in %+v", jobs)
	}
}

func TestPagesRender(t *testing.T) {
	s, _ := newTestSite(t)
	j, _ := s.create(Input{Name: str("n"), Cron: str("0 9 * * *"), Message: str("m")}, "t")
	s.runNow(context.Background(), j)
	bad, _ := s.create(Input{Name: str("b"), Cron: str("0 9 * * *"), Message: str("m")}, "t")
	_ = s.store.Reschedule(bad.ID, bad.NextRun, 1, time.Now(), "model timeout")
	for _, c := range []struct {
		name string
		h    http.HandlerFunc
		path string
		want string
	}{
		{"home", s.home, "/", "one failing"},
		{"runs", s.runsPage, "/runs", "run"},
		{"keys", s.keysPage, "/keys", "one per caller"},
		{"calendar", s.calendarPage, "/calendar?m=2026-12", "December 2026"},
		{"new", s.newJobPage, "/jobs/new?kind=prompt", "What to look up"},
	} {
		w := httptest.NewRecorder()
		c.h(w, httptest.NewRequest("GET", c.path, nil))
		if body := w.Body.String(); !strings.Contains(body, "</html>") || !strings.Contains(body, c.want) {
			t.Errorf("%s didn't render, or is missing %q", c.name, c.want)
		}
	}
	w := httptest.NewRecorder()
	s.jobForm(w, 200, j, "an error")
	if !strings.Contains(w.Body.String(), "</html>") || !strings.Contains(w.Body.String(), "an error") {
		t.Error("job.html didn't finish rendering")
	}
	w = httptest.NewRecorder()
	s.routes().ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "reach your phone") {
		t.Errorf("signed out root %d", w.Code)
	}
	w = httptest.NewRecorder()
	s.routes().ServeHTTP(w, httptest.NewRequest("GET", suite.URL("suite.css"), nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("suite.css %d %q", w.Code, w.Header().Get("Cache-Control"))
	}
}

func mustRuns(s *site) []Run {
	r, _ := s.store.Runs(0, 10)
	return r
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestParseAnswer(t *testing.T) {
	a, err := parseAnswer([]byte(`{"choices":[{"message":{"content":"","tool_calls":[{"function":{"name":"report","arguments":"{\"notify\":false,\"title\":\"t\",\"message\":\"nothing new\"}"}}]}}]}`))
	if err != nil || a.Notify || a.Message != "nothing new" {
		t.Errorf("%+v %v", a, err)
	}
	a, err = parseAnswer([]byte(`{"choices":[{"message":{"content":"It's 87 on Metacritic"}}]}`))
	if err != nil || !a.Notify || a.Message == "" {
		t.Errorf("prose answer %+v %v", a, err)
	}
	if _, err := parseAnswer([]byte(`{"choices":[]}`)); err == nil {
		t.Error("an empty answer parsed")
	}
}

func TestHumanCron(t *testing.T) {
	for in, want := range map[string]string{
		"0 8 * * *":      "every day 8:00am",
		"30 19 * * 2":    "Tue 7:30pm",
		"0 9 * * 1-5":    "weekdays 9:00am",
		"0 9 * * 1,4":    "Mon, Thu 9:00am",
		"0 10 1 * *":     "monthly on the 1st 10:00am",
		"0 9 3 3 *":      "every Mar 3 9:00am",
		"0 9 * 11 thu#4": "the 4th Thu of Nov 9:00am",
		"0 9 * 5 mon#l":  "the last Mon of May 9:00am",
		"0 9 * * 5#1":    "the 1st Fri of every month 9:00am",
		"*/5 * * * *":    "every 5 minutes",
		"0 */4 * * *":    "every 4 hours",
		"0 9 1 */3 *":    "on the 1st every 3 months 9:00am",
		"0 9 1-7 * 1":    "0 9 1-7 * 1",
	} {
		if got := humanCron(in); got != want {
			t.Errorf("%q read as %q, want %q", in, got, want)
		}
	}
}

func TestNotFound(t *testing.T) {
	s, _ := newTestSite(t)
	for path, want := range map[string]string{"/nope": "No such page", "/api/nope": `"error"`} {
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusNotFound || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), want) {
			t.Errorf("%s answered %d %q without %q", path, w.Code, w.Header().Get("Cache-Control"), want)
		}
	}
	w := httptest.NewRecorder()
	s.pathJob(w, httptest.NewRequest("GET", "/jobs/999", nil))
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "That job fired") {
		t.Errorf("a missing job answered %d", w.Code)
	}
}
