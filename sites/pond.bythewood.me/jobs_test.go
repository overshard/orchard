package main

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestTheLevelTableIsRuneScapes(t *testing.T) {
	if xpFor[2] != 83 || xpFor[92] != 6517253 || xpFor[99] != 13034431 {
		t.Errorf("xp table: 2=%d 92=%d 99=%d", xpFor[2], xpFor[92], xpFor[99])
	}
	if levelOf(0) != 1 || levelOf(82) != 1 || levelOf(83) != 2 || levelOf(13034431) != 99 || levelOf(1<<40) != 99 {
		t.Error("levelOf is off")
	}
}

func TestAFrogWorksAShift(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	v, _ := s.view(id)
	f := v.Frogs[0].ID
	if _, err := s.setJob(id, f, "sticks", ""); !errors.Is(err, errNotYet) {
		t.Fatalf("an egg took a job: %v", err)
	}
	*clock = clock.Add(2 * 24 * time.Hour)
	if _, err := s.setJob(id, f, "juggling", ""); !errors.Is(err, errNotHeld) {
		t.Errorf("a job that isn't one: %v", err)
	}
	if _, err := s.setJob(id, f, "sticks", ""); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(30 * time.Hour)
	v, _ = s.view(id)
	if w := v.Frogs[0].Work; w == nil || w.Hours != 12 || v.Jobs.Waiting != 12 {
		t.Fatalf("a shift past the hold: %+v", v.Frogs[0].Work)
	}
	c, err := s.collectJobs(id)
	if err != nil || len(c.Done) != 1 {
		t.Fatalf("collect: %v %+v", err, c)
	}
	d := c.Done[0]
	if n := d.Got["sticks"]; n < 30 || n > 60 {
		t.Errorf("%d sticks for twelve hours", n)
	}
	if d.Level != levelOf(12*xpPerHour) || d.Up != d.Level-1 {
		t.Errorf("level after a shift: %+v", d)
	}
	if _, err := s.collectJobs(id); !errors.Is(err, errNotYet) {
		t.Errorf("collected twice: %v", err)
	}
	if v := c.Pond; v.Jobs.Total != d.Level+len(jobs)-1 || v.Share.Power.JobsTotal != v.Jobs.Total {
		t.Errorf("total level %d, power %+v", v.Jobs.Total, v.Share.Power)
	}
}

func TestCraftingUsesWhatItHas(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	v, _ := s.view(id)
	f := v.Frogs[0].ID
	*clock = clock.Add(2 * 24 * time.Hour)
	if _, err := s.setJob(id, f, jobCraft, "t_statue"); !errors.Is(err, errLevel) {
		t.Errorf("a statue at level 1: %v", err)
	}
	if _, err := s.setJob(id, f, jobCraft, "nonsense"); !errors.Is(err, errNotHeld) {
		t.Errorf("a recipe that isn't one: %v", err)
	}
	if _, err := s.setJob(id, f, jobCraft, "t_raft"); err != nil {
		t.Fatal(err)
	}
	if err := s.write(func(tx *sql.Tx) error {
		if err := addItem(tx, id, "sticks", 13); err != nil {
			return err
		}
		return addItem(tx, id, "reeds", 9)
	}); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(6 * time.Hour)
	c, err := s.collectJobs(id)
	if err != nil {
		t.Fatal(err)
	}
	if d := c.Done[0]; d.Got["t_raft"] != 2 || d.Short {
		t.Errorf("crafted %+v", d)
	}
	held := map[string]int{}
	for _, it := range c.Pond.Items {
		held[it.Kind] = it.Count
	}
	if held["sticks"] != 1 || held["reeds"] != 5 {
		t.Errorf("left over %+v", held)
	}
	if lv := c.Pond.Frogs[0].Work.Levels; lv[len(lv)-1].XP != 2*xpPerHour {
		t.Errorf("crafting xp %+v", lv[len(lv)-1])
	}
	*clock = clock.Add(3 * time.Hour)
	c, _ = s.collectJobs(id)
	if d := c.Done[0]; len(d.Got) != 0 || !d.Short {
		t.Errorf("made something from nothing: %+v", d)
	}
}

func TestAFullFrogWorksHarder(t *testing.T) {
	f := frogRow{Job: "sticks", JobAt: 1000, Fed: dishOf(10, 10).kind(), FedAt: 1000 + 3600}
	h, fed := worked(f, time.Unix(1000+5*3600, 0))
	if h != 5 || fed != 4 {
		t.Errorf("worked %v hours, %v of them full", h, fed)
	}
	if b := boosted(f, h, fed); b != 7 {
		t.Errorf("boosted to %v", b)
	}
	f.FedAt = 1000 - 47*3600
	if _, fed := worked(f, time.Unix(1000+5*3600, 0)); fed != 1 {
		t.Errorf("a meal running out mid shift counted %v hours", fed)
	}
}
