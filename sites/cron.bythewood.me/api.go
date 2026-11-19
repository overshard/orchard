package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Input is a job as the API and the form both send it. A nil field is left as
// it was, which is what makes PATCH a partial update.
type Input struct {
	Name     *string `json:"name"`
	Kind     *string `json:"kind"`
	Cron     *string `json:"cron"`
	At       *string `json:"at"`
	Title    *string `json:"title"`
	Message  *string `json:"message"`
	Priority *string `json:"priority"`
	Link     *string `json:"link"`
	Enabled  *bool   `json:"enabled"`
}

var priorities = []string{"min", "low", "default", "high", "urgent"}

// atLayouts are tried in order. Anything without an offset is Eastern.
var atLayouts = []string{"2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02T15:04:05", "2006-01-02 3:04pm", "2006-01-02 3pm"}

func (s *site) parseAt(v string) (time.Time, error) {
	v = strings.TrimSpace(v)
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	for _, l := range atLayouts {
		if t, err := time.ParseInLocation(l, strings.ReplaceAll(strings.ReplaceAll(v, "PM", "pm"), "AM", "am"), s.loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("%q isn't a time, send 2026-11-22T08:00 for Eastern or RFC3339 with an offset", v)
}

// apply folds in onto j and checks the result. Setting a cron clears at and
// setting at clears the cron, so a job is always one or the other.
func (s *site) apply(j *Job, in Input) error {
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = strings.TrimSpace(*v)
		}
	}
	set(&j.Name, in.Name)
	set(&j.Kind, in.Kind)
	set(&j.Title, in.Title)
	set(&j.Priority, in.Priority)
	set(&j.Link, in.Link)
	if in.Message != nil {
		j.Message = strings.TrimSpace(strings.ReplaceAll(*in.Message, "\r\n", "\n"))
	}
	if in.Enabled != nil {
		j.Enabled = *in.Enabled
	}
	if in.Cron != nil && strings.TrimSpace(*in.Cron) != "" {
		j.Cron, j.At = strings.TrimSpace(*in.Cron), time.Time{}
	} else if in.At != nil && strings.TrimSpace(*in.At) != "" {
		at, err := s.parseAt(*in.At)
		if err != nil {
			return err
		}
		j.Cron, j.At = "", at.Truncate(time.Minute)
	}

	if j.Kind == "" {
		j.Kind = "notify"
	}
	if j.Priority == "" {
		j.Priority = "default"
	}
	switch {
	case j.Name == "":
		return errors.New("a job needs a name")
	case len(j.Name) > 120:
		return errors.New("keep the name under 120 characters")
	case j.Kind != "notify" && j.Kind != "prompt" && j.Kind != "quote":
		return errors.New("kind is notify, prompt or quote")
	case j.Message == "" && j.Kind == "quote":
		return errors.New("a quote job needs a ticker as its message")
	case j.Message == "" && j.Kind == "prompt":
		return errors.New("a prompt job needs a prompt")
	case j.Message == "":
		return errors.New("a reminder needs a message")
	case !contains(priorities, j.Priority):
		return fmt.Errorf("priority is one of %s", strings.Join(priorities, ", "))
	case j.Link != "" && !strings.HasPrefix(j.Link, "https://") && !strings.HasPrefix(j.Link, "http://"):
		return errors.New("a link starts with https://")
	case j.Cron == "" && j.At.IsZero():
		return errors.New("a job needs a time to run at or a cron line")
	}

	now := time.Now()
	next, err := s.sched.NextRun(*j, now)
	if err != nil {
		return err
	}
	if j.Once() && j.Enabled && next.Before(now.Add(-time.Minute)) {
		return fmt.Errorf("%s has already passed", s.eastern(next))
	}
	j.NextRun = next
	if !j.Enabled {
		j.NextRun = time.Time{}
	}
	return nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func (s *site) create(in Input, by string) (Job, error) {
	j := Job{Enabled: true, CreatedBy: by}
	if err := s.apply(&j, in); err != nil {
		return j, err
	}
	id, err := s.store.Insert(j)
	j.ID = id
	s.sched.Wake()
	return j, err
}

func (s *site) save(j Job, in Input) (Job, error) {
	if err := s.apply(&j, in); err != nil {
		return j, err
	}
	err := s.store.Update(j)
	s.sched.Wake()
	return j, err
}

// runNow fires a job outside its schedule and leaves the schedule alone. It is
// bounded so a hung model can't hold a request open forever.
func (s *site) runNow(ctx context.Context, j Job) Run {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), modelTimeout+30*time.Second)
	defer cancel()
	return s.sched.Execute(ctx, j, 0)
}

// The API takes a key from -newkey or the keys page, or a signed in session so
// the page's own scripts can call it.
func (s *site) api(next func(w http.ResponseWriter, r *http.Request, caller string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
			if name, ok := s.store.Authenticate(strings.TrimSpace(tok)); ok {
				next(w, r, name)
				return
			}
		} else if s.auth.Authenticated(r) {
			next(w, r, "web")
			return
		}
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "send Authorization: Bearer cron-..."})
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func fail(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

type jobJSON struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Cron      string `json:"cron,omitempty"`
	At        string `json:"at,omitempty"`
	Schedule  string `json:"schedule"`
	NextRun   string `json:"next_run,omitempty"`
	Next      string `json:"next,omitempty"`
	Title     string `json:"title,omitempty"`
	Message   string `json:"message"`
	Priority  string `json:"priority"`
	Link      string `json:"link,omitempty"`
	Enabled   bool   `json:"enabled"`
	LastRun   string `json:"last_run,omitempty"`
	LastErr   string `json:"last_err,omitempty"`
	Failures  int    `json:"failures,omitempty"`
	CreatedBy string `json:"created_by,omitempty"`
}

func (s *site) rfc(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.In(s.loc).Format(time.RFC3339)
}

func (s *site) toJSON(j Job) jobJSON {
	out := jobJSON{
		ID: j.ID, Name: j.Name, Kind: j.Kind, Cron: j.Cron, At: s.rfc(j.At), Schedule: s.describe(j),
		NextRun: s.rfc(j.NextRun), Title: j.Title, Message: j.Message, Priority: j.Priority, Link: j.Link,
		Enabled: j.Enabled, LastRun: s.rfc(j.LastRun), LastErr: j.LastErr, Failures: j.Failures, CreatedBy: j.CreatedBy,
	}
	if !j.NextRun.IsZero() {
		out.Next = s.eastern(j.NextRun)
	}
	return out
}

type runJSON struct {
	ID     int64  `json:"id"`
	JobID  int64  `json:"job_id"`
	Name   string `json:"name"`
	At     string `json:"at"`
	MS     int64  `json:"ms"`
	OK     bool   `json:"ok"`
	Pushed bool   `json:"pushed"`
	Title  string `json:"title,omitempty"`
	Body   string `json:"body,omitempty"`
	Err    string `json:"err,omitempty"`
}

func (s *site) runToJSON(r Run) runJSON {
	return runJSON{ID: r.ID, JobID: r.JobID, Name: r.Name, At: s.rfc(r.At), MS: r.MS, OK: r.OK,
		Pushed: r.Pushed, Title: r.Title, Body: r.Body, Err: r.Err}
}

func (s *site) jobFromPath(w http.ResponseWriter, r *http.Request) (Job, bool) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	j, err := s.store.Job(id)
	if err != nil {
		fail(w, http.StatusNotFound, errors.New("no such job"))
		return j, false
	}
	return j, true
}

func decode(r *http.Request) (Input, error) {
	var in Input
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return in, fmt.Errorf("bad json: %w", err)
	}
	return in, nil
}

func (s *site) apiList(w http.ResponseWriter, r *http.Request, _ string) {
	jobs, err := s.store.Jobs()
	if err != nil {
		fail(w, 500, err)
		return
	}
	out := make([]jobJSON, len(jobs))
	for i, j := range jobs {
		out[i] = s.toJSON(j)
	}
	writeJSON(w, 200, map[string]any{"jobs": out})
}

func (s *site) apiCreate(w http.ResponseWriter, r *http.Request, caller string) {
	in, err := decode(r)
	if err != nil {
		fail(w, 400, err)
		return
	}
	j, err := s.create(in, caller)
	if err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, 201, s.toJSON(j))
}

func (s *site) apiGet(w http.ResponseWriter, r *http.Request, _ string) {
	if j, ok := s.jobFromPath(w, r); ok {
		writeJSON(w, 200, s.toJSON(j))
	}
}

func (s *site) apiPatch(w http.ResponseWriter, r *http.Request, _ string) {
	j, ok := s.jobFromPath(w, r)
	if !ok {
		return
	}
	in, err := decode(r)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if j, err = s.save(j, in); err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, 200, s.toJSON(j))
}

func (s *site) apiDelete(w http.ResponseWriter, r *http.Request, _ string) {
	j, ok := s.jobFromPath(w, r)
	if !ok {
		return
	}
	if err := s.store.Delete(j.ID); err != nil {
		fail(w, 500, err)
		return
	}
	s.sched.Wake()
	writeJSON(w, 200, map[string]any{"deleted": j.ID, "name": j.Name})
}

func (s *site) apiRun(w http.ResponseWriter, r *http.Request, _ string) {
	if j, ok := s.jobFromPath(w, r); ok {
		writeJSON(w, 200, s.runToJSON(s.runNow(r.Context(), j)))
	}
}

func (s *site) apiRuns(w http.ResponseWriter, r *http.Request, _ string) {
	job, _ := strconv.ParseInt(r.URL.Query().Get("job"), 10, 64)
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit < 1 || limit > 500 {
		limit = 50
	}
	runs, err := s.store.Runs(job, limit)
	if err != nil {
		fail(w, 500, err)
		return
	}
	out := make([]runJSON, len(runs))
	for i, r := range runs {
		out[i] = s.runToJSON(r)
	}
	writeJSON(w, 200, map[string]any{"runs": out})
}

// apiPreview says when a cron line or a time would fire without saving
// anything, for the form and for checking a line before it's stored.
func (s *site) apiPreview(w http.ResponseWriter, r *http.Request, _ string) {
	q := r.URL.Query()
	var times []time.Time
	if c := strings.TrimSpace(q.Get("cron")); c != "" {
		e, err := ParseCron(c)
		if err != nil {
			fail(w, 400, err)
			return
		}
		t := time.Now()
		for range 3 {
			next, ok := e.Next(t, s.loc)
			if !ok {
				break
			}
			times, t = append(times, next), next
		}
	} else {
		at, err := s.parseAt(q.Get("at"))
		if err != nil {
			fail(w, 400, err)
			return
		}
		times = append(times, at)
	}
	out := []map[string]string{}
	for _, t := range times {
		out = append(out, map[string]string{"at": s.rfc(t), "text": s.eastern(t)})
	}
	writeJSON(w, 200, map[string]any{"next": out})
}

func (s *site) snooze(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	n, ok := s.store.TakeSnooze(r.PathValue("token"), time.Now())
	if !ok {
		http.Error(w, "that snooze was used or has expired", http.StatusNotFound)
		return
	}
	j, err := s.sched.Snoozed(n)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	fmt.Fprintf(w, "snoozed until %s\n", s.eastern(j.NextRun))
}
