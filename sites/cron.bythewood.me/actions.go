package main

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"
)

// try runs a job without sending it, bounded like a run by hand.
func (s *site) try(ctx context.Context, j Job) Trial {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), modelTimeout+30*time.Second)
	defer cancel()
	return s.sched.composer.Try(ctx, j)
}

func (s *site) jobTry(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	j, err := s.store.Job(id)
	if err != nil {
		fail(w, http.StatusNotFound, errors.New("no such job, it may have fired and been removed"))
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, s.try(r.Context(), j))
}

func (s *site) apiTry(w http.ResponseWriter, r *http.Request, _ string) {
	if j, ok := s.jobFromPath(w, r); ok {
		writeJSON(w, 200, s.try(r.Context(), j))
	}
}

// jobMove puts a one-shot on another day at the same time of day, for the
// week's drag and its undo. A repeating job's days are its cron line.
func (s *site) jobMove(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	j, err := s.store.Job(id)
	if err != nil {
		fail(w, http.StatusNotFound, errors.New("no such job, it may have fired"))
		return
	}
	if !j.Once() {
		fail(w, http.StatusBadRequest, errors.New("it repeats, so edit the job to change its days"))
		return
	}
	day, err := time.ParseInLocation("2006-01-02", r.FormValue("day"), s.loc)
	if err != nil {
		fail(w, http.StatusBadRequest, errors.New("send day as 2006-01-02"))
		return
	}
	from := j.At.In(s.loc)
	to := clock{from.Hour(), from.Minute()}.on(day, s.loc).Format("2006-01-02T15:04")
	if j, err = s.save(j, Input{At: &to}); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, 200, map[string]string{"at": s.rfc(j.At), "text": s.eastern(j.At), "from": from.Format("2006-01-02")})
}

// setEnabled pauses or resumes, and says so with a toast on the page it lands on.
func (s *site) setEnabled(on bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		j, ok := s.pathJob(w, r)
		if !ok {
			return
		}
		if _, err := s.save(j, Input{Enabled: &on}); err != nil {
			s.jobForm(w, 400, j, err.Error())
			return
		}
		did := "paused"
		if on {
			did = "resumed"
		}
		http.Redirect(w, r, "/?"+did+"="+strconv.FormatInt(j.ID, 10), http.StatusSeeOther)
	}
}

// jobBulk pauses or resumes every picked row. It's posted after the undo
// toast runs out or by a beacon, so nothing reads the answer.
func (s *site) jobBulk(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	on := r.FormValue("do") == "resume"
	for _, v := range r.Form["id"] {
		id, _ := strconv.ParseInt(v, 10, 64)
		j, err := s.store.Job(id)
		if err != nil || j.Enabled == on {
			continue
		}
		_, _ = s.save(j, Input{Enabled: &on})
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
