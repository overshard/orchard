package main

import (
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cron.bythewood.me/web"
)

func (s *site) loadTemplates() error {
	if Reloaded {
		resetAssetVersions()
	}
	t, err := template.New("").Funcs(template.FuncMap{
		"eastern":  s.eastern,
		"ago":      ago,
		"until":    until,
		"describe": s.describe,
		"local":    s.local,
		"asset":    assetURL,
		"comma":    comma,
	}).ParseFS(assets(), "templates/*.html")
	if err != nil {
		return err
	}
	s.tpl = t
	return nil
}

func (s *site) static() http.Handler {
	fs := web.NoDirs(http.FileServerFS(assets()))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if Reloaded {
			w.Header().Set("Cache-Control", "no-store")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		fs.ServeHTTP(w, r)
	})
}

func (s *site) render(w http.ResponseWriter, code int, name string, data map[string]any) {
	if Reloaded {
		if err := s.loadTemplates(); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}
	data["Source"] = sourceURL
	data["Author"] = author
	data["Year"] = time.Now().Year()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	if err := s.tpl.ExecuteTemplate(w, name, data); err != nil {
		slog.Error("render", "name", name, "err", err)
	}
}

// eastern is how every time on the page reads, since every job is about
// Isaac's day.
func (s *site) eastern(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	t = t.In(s.loc)
	now := time.Now().In(s.loc)
	clock := strings.ToLower(t.Format("3:04PM"))
	switch {
	case sameDay(t, now):
		return "today " + clock
	case sameDay(t, now.AddDate(0, 0, 1)):
		return "tomorrow " + clock
	case t.Year() != now.Year():
		return t.Format("Mon Jan 2 2006 ") + clock
	}
	return t.Format("Mon Jan 2 ") + clock
}

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

// local is the value a datetime-local input wants.
func (s *site) local(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.In(s.loc).Format("2006-01-02T15:04")
}

func (s *site) describe(j Job) string {
	if j.Once() {
		return "once"
	}
	return j.Cron
}

func ago(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

func until(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Until(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("in %dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("in %dh", int(d.Hours()))
	}
	return fmt.Sprintf("in %dd", int(d.Hours()/24))
}

func comma(n int) string {
	str := strconv.Itoa(n)
	var out []byte
	for i, c := range []byte(str) {
		if i > 0 && (len(str)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}

func (s *site) root(w http.ResponseWriter, r *http.Request) {
	if s.auth.Authenticated(r) {
		s.home(w, r)
		return
	}
	jobs, runs, pushed := s.store.Counts()
	s.render(w, 200, "landing.html", map[string]any{"Jobs": jobs, "Runs": runs, "Pushed": pushed})
}

func (s *site) home(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.store.Jobs()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	runs, _ := s.store.Runs(0, 15)
	s.render(w, 200, "home.html", map[string]any{"Title": "Jobs", "Jobs": jobs, "Runs": runs})
}

func (s *site) runsPage(w http.ResponseWriter, r *http.Request) {
	runs, err := s.store.Runs(0, 200)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.render(w, 200, "runs.html", map[string]any{"Title": "Runs", "Runs": runs})
}

// form reads the job form into an Input. The schedule radio decides which of
// the two time fields counts, so a stale value in the hidden one is ignored.
func form(r *http.Request) Input {
	str := func(k string) *string { v := r.FormValue(k); return &v }
	in := Input{Name: str("name"), Kind: str("kind"), Title: str("title"), Message: str("message"),
		Priority: str("priority"), Link: str("link")}
	if r.FormValue("schedule") == "repeat" {
		in.Cron = str("cron")
		if strings.TrimSpace(*in.Cron) == "" {
			in.At = new(string)
		}
	} else {
		in.At, in.Cron = str("at"), new(string)
	}
	enabled := r.FormValue("enabled") == "on"
	in.Enabled = &enabled
	return in
}

func (s *site) jobForm(w http.ResponseWriter, code int, j Job, errText string) {
	var runs []Run
	if j.ID > 0 {
		runs, _ = s.store.Runs(j.ID, 30)
	}
	title := "New job"
	if j.ID > 0 {
		title = j.Name
	}
	s.render(w, code, "job.html", map[string]any{
		"Title": title, "Job": j, "Runs": runs, "Error": errText, "Priorities": priorities,
		"Repeat": j.Cron != "", "Kind": j.Kind,
	})
}

func (s *site) newJobPage(w http.ResponseWriter, r *http.Request) {
	j := Job{Kind: "notify", Priority: "default", Enabled: true,
		At: time.Now().In(s.loc).Add(time.Hour).Truncate(time.Hour)}
	if r.URL.Query().Get("kind") == "prompt" {
		j.Kind = "prompt"
	}
	s.jobForm(w, 200, j, "")
}

func (s *site) pathJob(w http.ResponseWriter, r *http.Request) (Job, bool) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	j, err := s.store.Job(id)
	if err != nil {
		http.Error(w, "no such job, it may have fired and been removed", http.StatusNotFound)
		return j, false
	}
	return j, true
}

func (s *site) jobPage(w http.ResponseWriter, r *http.Request) {
	if j, ok := s.pathJob(w, r); ok {
		s.jobForm(w, 200, j, "")
	}
}

func (s *site) jobCreate(w http.ResponseWriter, r *http.Request) {
	j, err := s.create(form(r), "web")
	if err != nil {
		s.jobForm(w, 400, j, err.Error())
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *site) jobSave(w http.ResponseWriter, r *http.Request) {
	j, ok := s.pathJob(w, r)
	if !ok {
		return
	}
	if j, err := s.save(j, form(r)); err != nil {
		s.jobForm(w, 400, j, err.Error())
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *site) jobRunNow(w http.ResponseWriter, r *http.Request) {
	if j, ok := s.pathJob(w, r); ok {
		s.runNow(r.Context(), j)
		http.Redirect(w, r, "/jobs/"+strconv.FormatInt(j.ID, 10), http.StatusSeeOther)
	}
}

func (s *site) jobToggle(w http.ResponseWriter, r *http.Request) {
	j, ok := s.pathJob(w, r)
	if !ok {
		return
	}
	enabled := !j.Enabled
	if _, err := s.save(j, Input{Enabled: &enabled}); err != nil {
		s.jobForm(w, 400, j, err.Error())
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *site) jobDelete(w http.ResponseWriter, r *http.Request) {
	if j, ok := s.pathJob(w, r); ok {
		_ = s.store.Delete(j.ID)
		s.sched.Wake()
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

const newKeyCookie = "new_key"

func (s *site) keysPage(w http.ResponseWriter, r *http.Request) {
	keys, err := s.store.Keys()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	// Shown once, on the redirect after it's made, since only a hash is kept.
	fresh := ""
	if c, err := r.Cookie(newKeyCookie); err == nil {
		fresh = c.Value
		http.SetCookie(w, &http.Cookie{Name: newKeyCookie, Path: "/keys", Secure: true, HttpOnly: true,
			SameSite: http.SameSiteStrictMode, MaxAge: -1})
	}
	s.render(w, 200, "keys.html", map[string]any{"Title": "Keys", "Keys": keys, "Fresh": fresh})
}

func (s *site) keyCreate(w http.ResponseWriter, r *http.Request) {
	secret, err := s.store.NewKey(r.FormValue("name"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: newKeyCookie, Value: secret, Path: "/keys", Secure: true,
		HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 60})
	http.Redirect(w, r, "/keys", http.StatusSeeOther)
}

func (s *site) keyDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	_ = s.store.DeleteKey(id)
	http.Redirect(w, r, "/keys", http.StatusSeeOther)
}
