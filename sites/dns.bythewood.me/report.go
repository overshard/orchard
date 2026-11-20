package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// apiReport is the day in JSON, for the mcp connector. hours widens it to a week.
func (s *site) apiReport(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
	if hours <= 0 || hours > keepDays*24 {
		hours = 24
	}
	f := Filter{Since: now.Add(-time.Duration(hours) * time.Hour)}
	if d := r.URL.Query().Get("device"); d != "" {
		ds, _ := s.store.Devices()
		for _, x := range ds {
			if x.Name == d || strconv.FormatInt(x.ID, 10) == d {
				f.Device = x.ID
			}
		}
	}
	total, blocked := s.store.Count(f)
	bf := f
	bf.Status = "blocked"
	ff := f
	ff.Status = "fail"
	failed, _ := s.store.Count(ff)
	type top struct {
		Name    string `json:"name"`
		N       int    `json:"n"`
		Reason  string `json:"reason,omitempty"`
		Devices int    `json:"devices"`
	}
	tops := func(rs []Ranked) []top {
		out := []top{}
		for _, x := range rs {
			out = append(out, top{x.Key, x.N, x.Reason, x.Devices})
		}
		return out
	}
	pf := f
	pf.Status = "pass"
	recent, _ := s.store.Log(Filter{Device: f.Device, Status: "blocked", Since: now.Add(-15 * time.Minute), Limit: 25})
	type rec struct {
		At     time.Time `json:"at"`
		Device string    `json:"device"`
		Name   string    `json:"name"`
		Reason string    `json:"reason"`
		Via    string    `json:"via,omitempty"`
	}
	var rc []rec
	for _, x := range s.rows(recent) {
		rc = append(rc, rec{x.At.UTC(), x.DeviceName, x.Name, x.Reason, x.Via})
	}
	type dev struct {
		Name     string    `json:"name"`
		Kind     string    `json:"kind"`
		LastSeen time.Time `json:"last_seen"`
		Proto    string    `json:"proto"`
		Total    int       `json:"total_24h"`
		Blocked  int       `json:"blocked_24h"`
		OffLists []string  `json:"lists_off,omitempty"`
	}
	var devs []dev
	for _, d := range s.deviceRows(now) {
		devs = append(devs, dev{d.Name, d.Kind, d.LastSeen.UTC(), d.LastProto, d.Total, d.Blocked, d.OffLists})
	}
	states, _ := s.store.ListStates()
	type lst struct {
		Key     string    `json:"key"`
		Name    string    `json:"name"`
		Entries int       `json:"entries"`
		Fetched time.Time `json:"fetched"`
		Error   string    `json:"error,omitempty"`
	}
	var ls []lst
	for _, li := range lists {
		st := states[li.Key]
		ls = append(ls, lst{li.Key, li.Name, st.Entries, st.Fetched.UTC(), st.Error})
	}
	h := s.health(now)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"hours": hours, "state": h.Level, "note": h.Note,
		"lookups": total, "blocked": blocked, "failed": failed,
		"top_blocked": tops(s.store.Top(bf, "name", 20)), "top_asked": tops(s.store.Top(pf, "name", 20)),
		"blocked_by":   tops(s.store.Top(bf, "reason", 10)),
		"just_blocked": rc, "devices": devs, "lists": ls,
	})
}
