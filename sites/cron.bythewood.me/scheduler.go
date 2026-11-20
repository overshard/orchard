package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"cron.bythewood.me/ntfy"
)

const (
	// A one-shot that fails to deliver is tried again this often, this many
	// times, before it is given up on.
	retryEvery = 5 * time.Minute
	maxRetries = 6
	// A repeating job missed by more than this while the container was down is
	// skipped to its next time instead of fired late.
	staleAfter = time.Hour
	// Late enough that the push says so.
	lateAfter = 10 * time.Minute
	snoozeFor = time.Hour
	// Jobs running at once. A prompt job can take a minute and the plain
	// reminders behind it shouldn't wait on it.
	parallel = 4
)

// Publisher is the slice of ntfy the scheduler uses, so tests can catch pushes.
type Publisher interface {
	Publish(ctx context.Context, m ntfy.Message) error
}

// composer works out what a job would say. It holds no Publisher, so a try
// built on it alone has no way to reach ntfy.
type composer struct {
	store *Store
	model *Model
	http  *http.Client
	loc   *time.Location
	now   func() time.Time
}

type Scheduler struct {
	composer
	push Publisher
	// Where a snooze button posts, the public hostname since the phone
	// presses it.
	base string

	// Waits between publish attempts. ntfy restarting with the edge is the
	// usual reason a push fails, and it's back within ten seconds.
	waits []time.Duration

	wake chan struct{}
	sem  chan struct{}
	wg   sync.WaitGroup
}

func NewScheduler(store *Store, push Publisher, model *Model, loc *time.Location) *Scheduler {
	return &Scheduler{
		composer: composer{store: store, model: model, loc: loc, now: time.Now, http: &http.Client{Timeout: 20 * time.Second}},
		push:     push, base: baseURL,
		waits: []time.Duration{2 * time.Second, 8 * time.Second},
		wake:  make(chan struct{}, 1), sem: make(chan struct{}, parallel),
	}
}

// Wake makes the loop look again, after a job is saved or deleted.
func (s *Scheduler) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// NextRun is when a job should next fire, counted from after.
func (s *Scheduler) NextRun(j Job, after time.Time) (time.Time, error) {
	if j.Once() {
		return j.At, nil
	}
	e, err := ParseCron(j.Cron)
	if err != nil {
		return time.Time{}, err
	}
	t, ok := e.Next(after, s.loc)
	if !ok {
		return time.Time{}, errors.New("that cron line never matches")
	}
	return t, nil
}

func (s *Scheduler) Loop(ctx context.Context) {
	s.skipStale()
	tick := time.NewTimer(0)
	defer tick.Stop()
	for {
		s.fireDue(ctx)
		wait := time.Minute
		if t := s.store.Soonest(); !t.IsZero() {
			wait = min(wait, max(time.Until(t), time.Second))
		}
		tick.Reset(wait)
		select {
		case <-ctx.Done():
			s.wg.Wait()
			return
		case <-s.wake:
		case <-tick.C:
		}
	}
}

// skipStale moves repeating jobs that came due while the container was down
// past the gap, since a daily check fired six hours late helps nobody, and
// writes a missed run for each fire it skipped.
func (s *Scheduler) skipStale() {
	now := s.now()
	jobs, err := s.store.Due(now.Add(-staleAfter))
	if err != nil {
		slog.Error("reading missed jobs", "err", err)
		return
	}
	for _, j := range jobs {
		if j.Once() {
			continue
		}
		next, err := s.NextRun(j, now)
		if err != nil {
			continue
		}
		s.recordMissed(j, now)
		slog.Warn("skipped a missed run", "job", j.Name, "due", j.NextRun.In(s.loc).Format(time.DateTime))
		_ = s.store.Reschedule(j.ID, next, j.Failures, j.LastRun, "missed while down")
	}
}

// maxMissed bounds the rows one outage writes for a job, so a minutely job
// down for a week leaves a line of them instead of ten thousand.
const maxMissed = 12

func (s *Scheduler) recordMissed(j Job, now time.Time) {
	var fires []time.Time
	total := 0
	for t := j.NextRun; !t.IsZero() && !t.After(now) && total < 100000; {
		total++
		fires = append(fires, t)
		if len(fires) > maxMissed {
			fires = fires[1:]
		}
		next, err := s.NextRun(j, t)
		if err != nil || !next.After(t) {
			break
		}
		t = next
	}
	for i, t := range fires {
		msg := "missed while down"
		if i == 0 && total > len(fires) {
			msg = fmt.Sprintf("missed while down, along with %d before it", total-len(fires))
		}
		if _, err := s.store.AddRun(Run{JobID: j.ID, Name: j.Name, Kind: j.Kind, At: t, Err: msg, Missed: "down"}); err != nil {
			slog.Error("writing a missed run", "job", j.Name, "err", err)
		}
	}
}

func (s *Scheduler) fireDue(ctx context.Context) {
	now := s.now()
	jobs, err := s.store.Due(now)
	if err != nil {
		slog.Error("reading due jobs", "err", err)
		return
	}
	for _, j := range jobs {
		// Claimed before it runs, so the next pass of the loop doesn't fire
		// it again while a slow prompt is still out.
		next := time.Time{}
		if !j.Once() {
			if next, err = s.NextRun(j, now); err != nil {
				slog.Error("job has a bad schedule", "job", j.Name, "err", err)
			}
		}
		if err := s.store.Reschedule(j.ID, next, j.Failures, j.LastRun, j.LastErr); err != nil {
			slog.Error("claiming a job", "job", j.Name, "err", err)
			continue
		}
		s.sem <- struct{}{}
		s.wg.Go(func() {
			defer func() { <-s.sem }()
			run := s.Execute(ctx, j, now.Sub(j.NextRun))
			s.settle(j, next, run)
		})
	}
}

// settle records how a scheduled fire went. A one-shot is deleted once it has
// landed, and the run row keeps its name.
func (s *Scheduler) settle(j Job, next time.Time, run Run) {
	switch {
	case j.Once() && run.OK:
		_ = s.store.Delete(j.ID)
	case j.Once() && j.Failures+1 < maxRetries:
		_ = s.store.Reschedule(j.ID, s.now().Add(retryEvery), j.Failures+1, run.At, run.Err)
	case j.Once():
		slog.Error("gave up on a reminder", "job", j.Name, "err", run.Err)
		_ = s.store.MarkRun(run.ID, "dropped", fmt.Sprintf("dropped after %d tries: %s", maxRetries, run.Err))
		_ = s.store.Delete(j.ID)
	case run.OK:
		_ = s.store.Reschedule(j.ID, next, 0, run.At, "")
	default:
		if run.dropped {
			_ = s.store.MarkRun(run.ID, "dropped", "push dropped: "+run.Err)
		}
		_ = s.store.Reschedule(j.ID, next, j.Failures+1, run.At, run.Err)
	}
	s.Wake()
}

// Execute runs a job once and writes the run row. late is how far past its time
// it fired, zero for a run by hand.
func (s *Scheduler) Execute(ctx context.Context, j Job, late time.Duration) Run {
	start := s.now()
	run := Run{JobID: j.ID, Name: j.Name, Kind: j.Kind, At: start}

	d, err := s.compose(ctx, j)
	if err != nil {
		run.Err = err.Error()
		// A check that silently stops is the thing this site exists to
		// prevent, so a failed one says so. A quote failing every minute
		// would be noise, so it only logs.
		if j.Kind == "prompt" {
			_ = s.publish(ctx, ntfy.Message{
				Title: "Check failed: " + j.Name, Body: run.Err, Priority: "default", Tags: "warning",
			})
		}
		return s.record(run, start)
	}
	if late > lateAfter {
		d.Body += fmt.Sprintf("\n\n(late by %s)", roundLate(late))
	}
	run.Title, run.Body = d.Title, d.Body
	if !d.Send {
		run.OK = true
		return s.record(run, start)
	}

	m := ntfy.Message{Title: d.Title, Body: d.Body, Priority: j.Priority, Tags: "alarm_clock", Link: j.Link, Label: "Open"}
	if tok, err := s.store.AddSnooze(Snooze{Name: j.Name, Title: d.Title, Body: d.Body, Priority: j.Priority, Link: j.Link, JobID: cmp.Or(j.SnoozeOf, j.ID)}, start); err == nil {
		m.Buttons = []string{"http, Snooze 1h, " + s.base + "/snooze/" + tok + ", method=POST, clear=true"}
	}
	if err := s.publish(ctx, m); err != nil {
		run.Err, run.dropped = err.Error(), true
	} else {
		run.OK, run.Pushed = true, true
	}
	return s.record(run, start)
}

// draft is what a job would push, and Send false means it would stay quiet.
type draft struct {
	Title, Body string
	Send        bool
}

func (c *composer) compose(ctx context.Context, j Job) (draft, error) {
	d := draft{Title: j.Title, Body: j.Message, Send: true}
	if d.Title == "" {
		d.Title = j.Name
	}
	switch j.Kind {
	case "prompt":
		ans, err := c.ask(ctx, j)
		if err != nil {
			return d, err
		}
		d.Send, d.Body = ans.Notify, ans.Message
		if ans.Title != "" {
			d.Title = ans.Title
		}
	case "quote":
		t, b, ok, err := c.quote(ctx, j)
		if err != nil {
			return d, err
		}
		d.Title, d.Body, d.Send = t, b, ok
	}
	return d, nil
}

// Trial is what a try came back with. Nothing about it was sent or stored.
type Trial struct {
	Push  bool   `json:"push"`
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
	MS    int64  `json:"ms"`
	Err   string `json:"err,omitempty"`
}

// Try runs a job the way a fire would and says what it would push. It lives on
// composer so it can't publish, write a run or mint a snooze.
func (c *composer) Try(ctx context.Context, j Job) Trial {
	start := c.now()
	d, err := c.compose(ctx, j)
	t := Trial{MS: c.now().Sub(start).Milliseconds()}
	if err != nil {
		t.Err = err.Error()
		return t
	}
	t.Push, t.Title, t.Body = d.Send, d.Title, d.Body
	return t
}

func (s *Scheduler) record(run Run, start time.Time) Run {
	run.MS = s.now().Sub(start).Milliseconds()
	id, err := s.store.AddRun(run)
	if err != nil {
		slog.Error("writing a run", "job", run.Name, "err", err)
	}
	run.ID = id
	if run.Err != "" {
		slog.Error("job failed", "job", run.Name, "err", run.Err)
	} else {
		slog.Info("job ran", "job", run.Name, "pushed", run.Pushed, "ms", run.MS)
	}
	return run
}

func (s *Scheduler) publish(ctx context.Context, m ntfy.Message) error {
	err := s.push.Publish(ctx, m)
	for _, wait := range s.waits {
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		err = s.push.Publish(ctx, m)
	}
	return err
}

func (s *composer) ask(ctx context.Context, j Job) (Answer, error) {
	if s.model == nil {
		return Answer{}, errors.New("no LLM_KEY, so prompt jobs can't run")
	}
	return s.model.Ask(ctx, j.Message+s.lastPush(j.ID), s.now().In(s.loc))
}

// lastPush gives a check its previous push, so one can report a change since
// then or stay quiet when it already told him.
func (s *composer) lastPush(job int64) string {
	r, ok := s.lastPushRun(job)
	if !ok {
		return "\n\nThis check hasn't pushed anything yet."
	}
	return fmt.Sprintf("\n\nYour last push for this check was at %s Eastern, titled %q: %s",
		r.At.In(s.loc).Format("Jan 2 3:04pm"), r.Title, r.Body)
}

// Snoozed turns a pressed snooze button into a one-shot an hour out.
func (s *Scheduler) Snoozed(n Snooze) (Job, error) {
	j := Job{
		Name: n.Name, Kind: "notify", At: s.now().Add(snoozeFor).Truncate(time.Minute),
		Title: n.Title, Message: n.Body, Priority: n.Priority, Link: n.Link, Enabled: true, CreatedBy: "snooze",
		SnoozeOf: n.JobID,
	}
	j.NextRun = j.At
	id, err := s.store.Insert(j)
	j.ID = id
	s.Wake()
	return j, err
}

func roundLate(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return strings.TrimSuffix(d.Round(time.Minute).String(), "0s")
}
