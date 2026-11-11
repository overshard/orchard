package main

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/http"
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

// unsuited gives the pond's frogs a seed whose nature isn't the one a job
// suits, since a new frog's nature is random and a suited one works faster.
func unsuited(t *testing.T, s *store, id int64, job string) {
	t.Helper()
	j, _ := jobOf(job)
	seed := uint32(1)
	for genomeOf(seed, nil).Nature == j.nature {
		seed++
	}
	exec(t, s, `UPDATE frogs SET seed = ? WHERE pond_id = ?`, seed, id)
}

// working hatches the pond's frog and sets it to a target.
func working(t *testing.T, job, target string) (*store, *time.Time, int64, int64) {
	t.Helper()
	s, clock := testStore(t)
	_, id := newPond(t, s)
	unsuited(t, s, id, job)
	*clock = clock.Add(2 * 24 * time.Hour)
	v, _ := s.view(id)
	f := v.Frogs[0].ID
	if _, err := s.setJob(id, f, job, target); err != nil {
		t.Fatal(err)
	}
	return s, clock, id, f
}

func TestAFrogWorksActions(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	unsuited(t, s, id, "sticks")
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
	*clock = clock.Add(19 * time.Minute)
	if _, err := s.collectJobs(id); !errors.Is(err, errNotYet) {
		t.Errorf("collected before the first twig: %v", err)
	}
	if v, _ := s.view(id); v.Frogs[0].Work.XP != 0 || v.Frogs[0].Work.Levels[2].XP != 0 {
		t.Errorf("xp before an action finished: %+v", v.Frogs[0].Work)
	}
	*clock = clock.Add(30 * time.Hour)
	v, _ = s.view(id)
	w := v.Frogs[0].Work
	if w.Target != "twigs" || w.Act != 1200 || w.Done != 36 || w.Cap != 36 || w.Stop != "full" || w.FullIn != 0 || v.Jobs.Baskets != 1 {
		t.Fatalf("a basket past the hold: %+v", w)
	}
	row := false
	for _, r := range v.Todo {
		row = row || r.Label == "Basket full, collect to keep gathering" && r.To == "job"
	}
	if !row {
		t.Errorf("no row for the full basket in %+v", v.Todo)
	}
	*clock = clock.Add(5 * time.Hour)
	if v2, _ := s.view(id); v2.Frogs[0].Work.Done != 36 || v2.Frogs[0].Work.Basket[0].N != w.Basket[0].N {
		t.Errorf("a full basket kept filling: %+v", v2.Frogs[0].Work)
	}
	c, err := s.collectJobs(id)
	if err != nil || len(c.Done) != 1 {
		t.Fatalf("collect: %v %+v", err, c)
	}
	d := c.Done[0]
	if d.Actions != 36 || d.Got["sticks"] != w.Basket[0].N || d.XP != w.XP || d.Stop != "full" {
		t.Errorf("collected %+v from a basket of %+v", d, w)
	}
	if twigs, _ := jobs[2].target("twigs"); d.Level != levelOf(36*twigs.xp) || d.Up != d.Level-1 {
		t.Errorf("level after a basket: %+v", d)
	}
	if _, err := s.collectJobs(id); !errors.Is(err, errNotYet) {
		t.Errorf("collected twice: %v", err)
	}
	if v := c.Pond; v.Jobs.Total != d.Level+len(jobs)-1 || v.Share.Power.part("jobs").Have != v.Jobs.Total {
		t.Errorf("total level %d, power %+v", v.Jobs.Total, v.Share.Power)
	}
	if w := c.Pond.Frogs[0].Work; w.Done != 0 || w.Stop != "" || w.Since != 0 {
		t.Errorf("a stopped frog didn't start again from now: %+v", w)
	}
}

// What the view says is in the basket is what collecting pays, item for item
// and pebble for pebble, finds and misses too.
func TestTheBasketIsWhatCollectingPays(t *testing.T) {
	for _, job := range []string{"bugs", "pebbles", "garden", "mud"} {
		s, clock, id, _ := working(t, job, "")
		*clock = clock.Add(7*time.Hour + 13*time.Minute)
		v, _ := s.view(id)
		w := v.Frogs[0].Work
		before := v.Pebbles
		c, err := s.collectJobs(id)
		if err != nil {
			t.Fatal(err)
		}
		d := c.Done[0]
		got := map[string]int{}
		for k, n := range d.Got {
			got[k] += n
		}
		for k, n := range d.Finds {
			got[k] += n
		}
		if d.Pebbles > 0 {
			got["peb"] = d.Pebbles
		}
		shown := map[string]int{}
		for _, x := range w.Basket {
			shown[x.Kind] += x.N
		}
		if len(got) != len(shown) || d.XP != w.XP || d.Misses != w.Misses || d.Actions != w.Done {
			t.Errorf("%s: shown %v %+v, paid %v %+v", job, shown, w, got, d)
		}
		for k, n := range shown {
			if got[k] != n {
				t.Errorf("%s: shown %d %s, paid %d", job, n, k, got[k])
			}
		}
		if c.Pond.Pebbles-before != shown["peb"] {
			t.Errorf("%s: the purse moved %d for %d shown", job, c.Pond.Pebbles-before, shown["peb"])
		}
	}
}

// The page plays out the actions dealt ahead, and they have to be the ones
// the basket then holds.
func TestTheDealsAheadComeTrue(t *testing.T) {
	s, clock, id, _ := working(t, "bugs", "")
	*clock = clock.Add(2 * time.Hour)
	v, _ := s.view(id)
	w := v.Frogs[0].Work
	if len(w.Ahead) != dealAhead {
		t.Fatalf("dealt %d ahead", len(w.Ahead))
	}
	*clock = clock.Add(time.Duration(w.Act*int64(dealAhead)) * time.Second)
	v2, _ := s.view(id)
	w2 := v2.Frogs[0].Work
	if w2.Done != w.Done+dealAhead {
		t.Fatalf("%d done after %d and %d dealt", w2.Done, w.Done, dealAhead)
	}
	want := map[string]int{}
	for _, x := range w.Basket {
		want[x.Kind] += x.N
	}
	misses := w.Misses
	for _, a := range w.Ahead {
		for _, x := range a.Got {
			want[x.Kind] += x.N
		}
		if a.Miss {
			misses++
		}
	}
	for _, x := range w2.Basket {
		want[x.Kind] -= x.N
	}
	for k, n := range want {
		if n != 0 {
			t.Errorf("%s off by %d", k, n)
		}
	}
	if misses != w2.Misses {
		t.Errorf("%d misses dealt, %d in the basket", misses, w2.Misses)
	}
}

// XP only comes with an action that finished, and a miss pays a fifth.
func TestXPComesOnlyFromActions(t *testing.T) {
	j, _ := jobOf("bugs")
	all := j.targets[0]
	all.miss = 1
	r := &run{f: frogRow{ID: 7, WorkAt: 1000}, j: j, t: all, level: 1, act: 1800}
	xp, n := 0, 0
	for i := range 40 {
		d := r.dealAt([]byte("k"), i)
		xp += d.xp
		n += d.n
	}
	if n != 0 || xp != 40*(all.xp/missShare) {
		t.Errorf("a frog that caught nothing got %d things and %d xp", n, xp)
	}
	r.t = j.targets[0]
	hits, misses, xp := 0, 0, 0
	for i := range 400 {
		d := r.dealAt([]byte("k"), i)
		xp += d.xp
		if d.miss {
			misses++
		} else {
			hits += d.n
		}
	}
	if xp != hits*r.t.xp+misses*(r.t.xp/missShare) || hits == 0 || misses == 0 {
		t.Errorf("%d crickets and %d misses paid %d xp", hits, misses, xp)
	}
}

func TestTargetsLockByLevel(t *testing.T) {
	s, _, id, f := working(t, "bugs", "")
	v, _ := s.view(id)
	if v.Frogs[0].Work.Target != "cricket" {
		t.Errorf("a new catcher is on %q", v.Frogs[0].Work.Target)
	}
	if _, err := s.setJob(id, f, "bugs", "firefly"); !errors.Is(err, errLevel) {
		t.Errorf("fireflies at level 1: %v", err)
	}
	if _, err := s.setJob(id, f, "bugs", "moths"); !errors.Is(err, errNotHeld) {
		t.Errorf("a target that isn't one: %v", err)
	}
	if err := s.write(func(tx *sql.Tx) error { return addXP(tx, id, f, "bugs", xpFor[25]) }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.setJob(id, f, "bugs", "firefly"); err != nil {
		t.Errorf("fireflies at 25: %v", err)
	}
	if _, err := s.setJob(id, f, "bugs", ""); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.view(id); v.Frogs[0].Work.Target != "firefly" {
		t.Errorf("no target gave %q at 25", v.Frogs[0].Work.Target)
	}
	for _, j := range jobs {
		if len(j.targets) == 0 || j.targets[0].level != 1 {
			t.Errorf("%s has nothing to do at level 1", j.key)
		}
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
	if _, err := s.setJob(id, f, jobCraft, ""); !errors.Is(err, errNotHeld) {
		t.Errorf("crafting nothing: %v", err)
	}
	if _, err := s.setJob(id, f, jobCraft, "t_raft"); !errors.Is(err, errLevel) {
		t.Errorf("a raft at level 1: %v", err)
	}
	if err := s.write(func(tx *sql.Tx) error { return addXP(tx, id, f, jobCraft, xpFor[4]) }); err != nil {
		t.Fatal(err)
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
	v, _ = s.view(id)
	if w := v.Frogs[0].Work; w.Done != 2 || w.Stop != "short" || w.Short != "sticks" {
		t.Errorf("a crafter out of sticks: %+v", w)
	}
	short := false
	for _, r := range v.Todo {
		short = short || r.Kind == "short"
	}
	if !short {
		t.Errorf("no row for running short in %+v", v.Todo)
	}
	c, err := s.collectJobs(id)
	if err != nil {
		t.Fatal(err)
	}
	if d := c.Done[0]; d.Got["t_raft"] != 2 || d.Actions != 2 || d.XP != 2*mustRecipe(t, "t_raft").xp {
		t.Errorf("crafted %+v", d)
	}
	held := map[string]int{}
	for _, it := range c.Pond.Items {
		held[it.Kind] = it.Count
	}
	if held["sticks"] != 1 || held["reeds"] != 5 {
		t.Errorf("left over %+v", held)
	}
	*clock = clock.Add(3 * time.Hour)
	if _, err := s.collectJobs(id); !errors.Is(err, errNotYet) {
		t.Errorf("made something from nothing: %v", err)
	}
}

// Two crafters share what the pond holds, the first frog first, so neither
// basket counts the same reeds.
func TestCraftersShareTheMaterials(t *testing.T) {
	s, clock, id, a := working(t, jobCraft, "t_boat")
	exec(t, s, `INSERT INTO frogs (pond_id, seed, name, hatched_at, out_front) VALUES (?, 7, 'PIP', ?, 0)`, id, clock.Add(-48*time.Hour).Unix())
	v, _ := s.view(id)
	b := v.Frogs[1].ID
	if _, err := s.setJob(id, b, jobCraft, "t_boat"); err != nil {
		t.Fatal(err)
	}
	if err := s.write(func(tx *sql.Tx) error { return addItem(tx, id, "reeds", 15) }); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(10 * time.Hour)
	v, _ = s.view(id)
	made := v.Frogs[0].Work.Done + v.Frogs[1].Work.Done
	if made != 5 || v.Frogs[0].Work.Done < 1 {
		t.Errorf("two crafters made %d boats from 15 reeds", made)
	}
	if _, err := s.setJob(id, b, "", ""); err != nil {
		t.Fatal(err)
	}
	c, err := s.collectJobs(id)
	if err != nil || c.Done[0].Got["t_boat"] != v.Frogs[0].Work.Done {
		t.Errorf("the first crafter's basket moved: %v %+v", err, c)
	}
	_ = a
}

func TestAFullFrogBringsInSeconds(t *testing.T) {
	d := dishOf(10, 10)
	f := frogRow{Fed: d.kind(), FedAt: 1000}
	if mealAt(f, 999) != 0 || mealAt(f, 1000) != mealBoost[d.tier] || mealAt(f, fullUntil(f)) != 0 {
		t.Error("the meal counts outside its hours")
	}
	j, _ := jobOf("sticks")
	r := &run{f: f, j: j, t: j.targets[0], level: 1, act: 600}
	r.f.WorkAt = 1000
	twos := 0
	const n = 4000
	for i := range n {
		if r.dealAt([]byte("k"), i%40).n == 2 {
			twos++
		}
		r.f.ID = int64(i / 40)
	}
	if got := float64(twos) / n; math.Abs(got-mealBoost[d.tier]) > 0.03 {
		t.Errorf("a %v meal doubled %v of the time", mealBoost[d.tier], got)
	}
}

// A frog working by the hour when this came in keeps every level and has
// its held hours waiting as actions.
func TestOldWorkMovesOntoTargets(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	age(t, s, id, 3*24*time.Hour)
	v, _ := s.view(id)
	f := v.Frogs[0].ID
	exec(t, s, `UPDATE frogs SET job = 'mud', job_at = ? WHERE id = ?`, clock.Add(-3*time.Hour).Unix(), f)
	exec(t, s, `INSERT INTO job_xp (pond_id, frog_id, job, xp) VALUES (?, ?, 'mud', 60000), (?, ?, 'bugs', 900)`, id, f, id, f)
	exec(t, s, `INSERT INTO frogs (pond_id, seed, name, hatched_at, out_front, job, recipe, job_at, carry) VALUES (?, 7, 'PIP', ?, 0, 'craft', 't_boat', ?, 0.25)`,
		id, clock.Add(-72*time.Hour).Unix(), clock.Add(-time.Hour).Unix())
	if err := migrateJobs(s.w, *clock); err != nil {
		t.Fatal(err)
	}
	v, _ = s.view(id)
	w := v.Frogs[0].Work
	if w.Target != "soft" || w.Since != 3*3600 || w.Done == 0 || w.Levels[4].XP != 60000 || w.Levels[1].XP != 900 {
		t.Errorf("an old digger: %+v", w)
	}
	if c := v.Frogs[1].Work; c.Target != "t_boat" || c.Since != 3600+900 {
		t.Errorf("an old crafter: %+v", c)
	}
	if err := migrateJobs(s.w, clock.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if v2, _ := s.view(id); v2.Frogs[0].Work.Since != w.Since {
		t.Error("the move ran twice")
	}
}

// Bank builds cost sticks, reeds and clay, so a day of work has to bring in
// what it did by the hour, at every target and level.
func TestADaysMaterialsStayPut(t *testing.T) {
	today := map[string]float64{"sticks": 3, "reeds": 3, "mud": 2, "pebbles": 1}
	for _, j := range jobs {
		rate, ok := today[j.key]
		if !ok {
			continue
		}
		for _, tg := range j.targets {
			for _, level := range []int{tg.level, 50, 99} {
				if level < tg.level {
					continue
				}
				for _, suited := range []bool{false, true} {
					act := actSecs(tg, pace{level, 1, 0, suited})
					day := 86400 / float64(act) * float64(tg.n) * (1 - tg.miss)
					was := 24 * rate * speedAt(level)
					if suited {
						was *= natureSpeed
					}
					if math.Abs(day/was-1) > 0.03 {
						t.Errorf("%s %s at %d: %.1f a day, was %.1f", j.key, tg.key, level, day, was)
					}
				}
			}
		}
	}
	j, _ := jobOf("sticks")
	r := &run{f: frogRow{ID: 3, WorkAt: 1}, j: j, t: j.targets[1], level: 30}
	r.act = actSecs(r.t, pace{30, 1, 0, false})
	got := 0
	n := int(30 * 86400 / r.act)
	for i := range n {
		got += r.dealAt([]byte("k"), i).n
	}
	if perDay := float64(got) / 30; math.Abs(perDay/(72*speedAt(30))-1) > 0.03 {
		t.Errorf("thirty days of branches at 30 brought %.1f sticks a day", perDay)
	}
	t.Logf("sticks a day at level 1, 30, 60 and 99: %.0f %.0f %.0f %.0f", 72*speedAt(1), 72*speedAt(30), 72*speedAt(60), 72*speedAt(99))
}

// Finds and pets turn up at the same rate an hour whatever the target.
func TestFindsComeByTheHour(t *testing.T) {
	j, _ := jobOf("mud")
	for _, tg := range j.targets {
		r := &run{f: frogRow{ID: 11, WorkAt: 1}, j: j, t: tg, level: 99}
		r.act = actSecs(tg, pace{99, 1, 0, false})
		agates, found := 0, 0
		n := int(4000 * 3600 / r.act)
		for i := range n {
			for _, k := range r.dealAt([]byte("k"), i).finds {
				if k == "agate" {
					agates++
				}
				if pets[k] != "" {
					found++
				}
			}
		}
		if per := 4000 / float64(agates); per < 9 || per > 11 {
			t.Errorf("%s: an agate every %.1f hours", tg.key, per)
		}
		if found < 1 || found > 6 {
			t.Errorf("%s: %d pets in 4000 hours at 99", tg.key, found)
		}
	}
}

func TestA99EarnsTheHood(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	*clock = clock.Add(2 * 24 * time.Hour)
	v, _ := s.view(id)
	f := v.Frogs[0].ID
	hood := hoodFor(2)
	if hood.name() != "Stick gathering hood" || hood.tier() != tierCount-1 {
		t.Errorf("hood %q tier %d", hood.name(), hood.tier())
	}
	if _, err := s.wear(id, hood); !errors.Is(err, errNotHeld) {
		t.Fatalf("wore a hood at level 1: %v", err)
	}
	before := v.Share.Power
	if err := s.write(func(tx *sql.Tx) error { return addXP(tx, id, f, "sticks", xpFor[maxLevel]) }); err != nil {
		t.Fatal(err)
	}
	v, err := s.wear(id, hood)
	if err != nil {
		t.Fatal(err)
	}
	if v.Frog.Hat != hood || !v.Hoods[2].Got || v.Hoods[0].Got {
		t.Errorf("wearing %+v, hoods %+v", v.Frog.Hat, v.Hoods)
	}
	if got, h := v.Share.Power.Total-before.Total, v.Share.Power.part("hoods"); h.Have != 1 || h.Pts != h.Of/len(jobs) || got < h.Pts+v.Share.Power.part("jobs").Pts-before.part("jobs").Pts {
		t.Errorf("a 99 and its hood added %d power", got)
	}
	var inBag bool
	for _, b := range v.Bag {
		if b.Hat != nil && *b.Hat == hood {
			inBag = b.Free == 0 && b.Count == 1
		}
	}
	if !inBag {
		t.Error("the hood isn't in the bag to wear, or it can be sold")
	}
	if _, err := s.stallList(id, hatKind(hood), 10); err == nil {
		t.Error("a hood went on the stall")
	}
}

func TestPetsOnlyComeFromWork(t *testing.T) {
	for range 2000 {
		if k := trinketOfTier(rollTier()).kind; pets[k] != "" {
			t.Fatalf("a box gave %s", k)
		}
	}
	for _, sorts := range wizardPool {
		for _, kinds := range sorts {
			for _, k := range kinds {
				if pets[k] != "" {
					t.Fatalf("the wizard can make %s", k)
				}
			}
		}
	}
	for _, j := range jobs {
		if petFor(j.key) == "" {
			t.Errorf("no pet for %s", j.key)
		}
	}
}

func mustRecipe(t *testing.T, key string) target {
	t.Helper()
	j, _ := jobOf(jobCraft)
	r, ok := j.target(key)
	if !ok {
		t.Fatalf("no recipe %s", key)
	}
	return r
}

// player is somebody working one frog at one job, collecting after each gap
// in turn.
type player struct {
	name string
	gaps []time.Duration
	// Whether each tool is bought as soon as a frog can use it.
	tools bool
}

var (
	ordinary = player{"ordinary", []time.Duration{10 * time.Hour, 14 * time.Hour}, false}
	keen     = player{"keen", []time.Duration{workHold}, true}
)

// playJob plays a frog at a job from level 1 on what each action pays on
// average, moving to the newest target at every collect, and says on which
// day each level came.
func playJob(j job, pl player) [maxLevel + 1]float64 {
	var day [maxLevel + 1]float64
	xp, hours := 0.0, 0.0
	done := map[string]mastered{}
	for i := 0; levelOf(int(xp)) < maxLevel; i++ {
		gap := pl.gaps[i%len(pl.gaps)]
		level := levelOf(int(xp))
		t := j.best(level)
		m := masteryOf(done[t.key].hours(t), masteryCap(level))
		tool := 0
		if pl.tools {
			tool = toolFor(toolTiers, level)
		}
		act := actSecs(t, pace{level, m, tool, false})
		n := int(int64(min(gap, workHold)/time.Second) / act)
		miss := t.miss * (1 - masteryMiss*float64(m-1))
		hit := float64(t.xp) * (1 + masteryTwo*float64(m-1))
		xp += float64(n) * ((1-miss)*hit + miss*float64(t.xp/missShare))
		done[t.key] = mastered{done[t.key].acts + n, 0}
		hours += gap.Hours()
		for l := level + 1; l <= levelOf(int(xp)); l++ {
			day[l] = hours / 24
		}
	}
	return day
}

// A level is earned: about 10 at the end of the first day, 30 in a week, 50
// in a month and 99 in four to six months, for every job.
func TestTheJobsPace(t *testing.T) {
	for _, j := range jobs {
		if j.key == jobCraft {
			continue
		}
		o, k := playJob(j, ordinary), playJob(j, keen)
		t.Logf("%-8s ordinary %4.1f %4.1f %5.1f %5.1f %5.1f   keen %4.1f %4.1f %5.1f %5.1f %5.1f", j.key, o[10], o[30], o[50], o[70], o[99], k[10], k[30], k[50], k[70], k[99])
		within := func(what string, got, lo, hi float64) {
			if got < lo || got > hi {
				t.Errorf("%s: %s on day %.1f, want %.0f to %.0f", j.key, what, got, lo, hi)
			}
		}
		within("10", o[10], 0.5, 1.6)
		within("30", o[30], 5, 9)
		within("50", o[50], 22, 36)
		within("99", o[99], 140, 190)
		within("a keen 99", k[99], 100, o[99])
		for _, l := range []int{30, 50, 70, 99} {
			if k[l] > o[l] {
				t.Errorf("%s: a keen frog got to %d after an ordinary one", j.key, l)
			}
		}
	}
}

// Old levels stay where they are, whatever an action pays now.
func TestXPNeverDrops(t *testing.T) {
	s, clock, id, f := working(t, "bugs", "")
	if err := s.write(func(tx *sql.Tx) error { return addXP(tx, id, f, "bugs", xpFor[60]+5) }); err != nil {
		t.Fatal(err)
	}
	if err := s.write(func(tx *sql.Tx) error { return addXP(tx, id, f, "bugs", -xpFor[60]) }); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(5 * time.Hour)
	c, err := s.collectJobs(id)
	if err != nil {
		t.Fatal(err)
	}
	l := c.Pond.Frogs[0].Work.Levels[1]
	if l.Level < 60 || l.XP < xpFor[60]+5 || l.XP != xpFor[60]+5+c.Done[0].XP {
		t.Errorf("a level 60 catcher after a basket: %+v", l)
	}
}

// Mastery climbs with base hours at a target, held to what the frog's level
// allows, and each level makes the target quicker, surer and likelier to
// bring two.
func TestMasteryCapsAndWhatItChanges(t *testing.T) {
	if masteryCap(1) != 2 || masteryCap(11) != 3 || masteryCap(88) != masteryTop || masteryCap(99) != masteryTop {
		t.Errorf("caps: %d %d %d", masteryCap(1), masteryCap(11), masteryCap(88))
	}
	if masteryOf(0, 2) != 1 || masteryOf(masteryAt(2), 2) != 2 || masteryOf(1e9, 2) != 2 || masteryOf(1e9, masteryCap(99)) != masteryTop {
		t.Error("masteryOf is off")
	}
	j, _ := jobOf("bugs")
	tg := j.targets[0]
	if a, b := actSecs(tg, pace{50, 1, 0, false}), actSecs(tg, pace{50, masteryTop, 0, false}); float64(a)/float64(b) < 1.2 || float64(a)/float64(b) > 1.25 {
		t.Errorf("mastery 10 takes an action from %ds to %ds", a, b)
	}
	count := func(m int) (miss, two float64) {
		r := &run{f: frogRow{ID: 5, WorkAt: 1}, j: j, t: tg, level: 50, act: 1800, mastery: m}
		const n = 20000
		for i := range n {
			d := r.dealAt([]byte("k"), i)
			if d.miss {
				miss++
			} else if d.n == 2 {
				two++
			}
		}
		return miss / n, two / (n - miss)
	}
	miss1, two1 := count(1)
	miss10, two10 := count(masteryTop)
	if math.Abs(miss1-tg.miss) > 0.02 || two1 != 0 {
		t.Errorf("mastery 1 missed %.2f and doubled %.2f", miss1, two1)
	}
	if math.Abs(miss10-tg.miss*(1-9*masteryMiss)) > 0.02 || math.Abs(two10-9*masteryTwo) > 0.02 {
		t.Errorf("mastery 10 missed %.2f and doubled %.2f", miss10, two10)
	}
}

// Mastery is stored when a basket is paid and only counts from the next one,
// so the basket on show never changes under the page.
func TestMasteryComesWithTheBasket(t *testing.T) {
	s, clock, id, _ := working(t, "sticks", "")
	*clock = clock.Add(13 * time.Hour)
	v, _ := s.view(id)
	if w := v.Frogs[0].Work; w.Mastery != 1 || len(w.Targets) != 0 {
		t.Fatalf("mastery before a collect: %+v", w)
	}
	c, err := s.collectJobs(id)
	if err != nil {
		t.Fatal(err)
	}
	d := c.Done[0]
	w := c.Pond.Frogs[0].Work
	if len(w.Targets) != 1 || w.Targets[0].Target != "twigs" || w.Targets[0].Got != d.Got["sticks"] {
		t.Fatalf("mastery after a basket of %d: %+v", d.Actions, w.Targets)
	}
	if d.Mastery != 2 || d.MasteryUp != 1 || w.Mastery != 2 || w.Act >= 1200 {
		t.Errorf("36 twigs, 12 base hours, gave %+v and the next basket %+v", d, w)
	}
	var first string
	s.r.QueryRow(`SELECT first_day FROM mastery WHERE pond_id = ?`, id).Scan(&first)
	if first == "" {
		t.Error("no day for the first twig")
	}
}

// The page names the target it wants, and the route takes that one.
func TestThePagePicksATarget(t *testing.T) {
	b, db, clock := testSite(t)
	var g greeting
	b.post("/api/greet", "{}", &g)
	*clock = clock.Add(2 * 24 * time.Hour)
	b.post("/api/greet", "{}", &g)
	f := g.Pond.Frogs[0].ID
	if _, err := db.w.Exec(`INSERT INTO job_xp (pond_id, frog_id, job, xp) SELECT pond_id, id, 'bugs', ? FROM frogs`, xpFor[20]); err != nil {
		t.Fatal(err)
	}
	var c workCollected
	body := fmt.Sprintf(`{"frog":%d,"job":"bugs","target":"mayfly"}`, f)
	if code := b.post("/api/jobs/set", body, &c); code != http.StatusOK || c.Pond.Frogs[0].Work.Target != "mayfly" {
		t.Errorf("set to mayflies: %d %+v", code, c.Pond.Frogs[0].Work)
	}
	body = fmt.Sprintf(`{"frog":%d,"job":"craft","target":"t_boat"}`, f)
	if code := b.post("/api/jobs/set", body, &c); code != http.StatusOK || c.Pond.Frogs[0].Work.Target != "t_boat" {
		t.Errorf("set to boats: %d %+v", code, c.Pond.Frogs[0].Work)
	}
}

// perHour is what a frog at its best target brings in an hour of work, on
// average, with mastery as far as its level lets it go.
func perHour(j job, level, tool int) float64 {
	t := j.best(level)
	m := masteryCap(level)
	act := float64(actSecs(t, pace{level, m, tool, false}))
	hit := 1 - t.miss*(1-masteryMiss*float64(m-1))
	return 3600 / act * hit * float64(t.n) * (1 + masteryTwo*float64(m-1))
}

// A tool can't buy itself back fast. Pebbles off pebble diving take a month
// of nonstop work at the best target and full mastery to pay back what a
// tier adds, and sticks, reeds and clay made into things for Moth two weeks.
func TestToolsCantMintPebbles(t *testing.T) {
	mat := 0.0
	for _, r := range recipes {
		if tr, ok := trinketOf(r.key); ok {
			n := 0
			for _, k := range r.in {
				n += k
			}
			mat = max(mat, float64(hatRate[tr.tier]/4)/float64(n))
		}
	}
	if mat >= materialWorth {
		t.Errorf("Moth pays %.2f a material made into something, over the stall's %d", mat, materialWorth)
	}
	worth := map[string]float64{"pebbles": 1, "sticks": mat, "reeds": mat, "mud": mat}
	least := map[string]float64{"pebbles": 30, "sticks": 14, "reeds": 14, "mud": 14}
	for _, j := range jobs {
		w, ok := worth[j.key]
		if !ok {
			continue
		}
		for tier := 1; tier <= toolTiers; tier++ {
			for _, level := range []int{toolLevel[tier], maxLevel} {
				gain := (perHour(j, level, tier) - perHour(j, level, tier-1)) * w
				days := float64(toolPrices[tier]) / gain / 24
				if level == maxLevel {
					t.Logf("%-7s tier %d at 99: %.2f an hour, %.2f more than the tier before, worth %.2f pebbles, paid back in %.0f days", j.key, tier, perHour(j, level, tier), gain/w, gain, days)
				}
				if days < least[j.key] {
					t.Errorf("%s tier %d at %d pays back in %.0f days", j.key, tier, level, days)
				}
			}
		}
		// The stall sells a handful a day, and a frog brings in more than
		// that with no tool at all, so a tool adds nothing it can sell.
		if j.key != "pebbles" && perHour(j, 1, 0)*24 < stallSlots {
			t.Errorf("%s brings in %.0f a day at level 1, under the stall's %d", j.key, perHour(j, 1, 0)*24, stallSlots)
		}
	}
	bugs, _ := jobOf("bugs")
	t.Logf("Moth pays at most %.2f a material, fireflies an hour at 99: %.2f with every tool and %.2f with none", mat, perHour(bugs, maxLevel, toolTiers), perHour(bugs, maxLevel, 0))
}

// A tool is the pond's once bought, needs a frog at its level, and only
// changes a basket from its next one.
func TestAToolIsBoughtOnceAndKept(t *testing.T) {
	s, clock, id, f := working(t, "bugs", "")
	exec(t, s, `UPDATE ponds SET pebbles = 100000 WHERE id = ?`, id)
	if _, err := s.buyTool(id, "bugs"); !errors.Is(err, errToolLevel) {
		t.Errorf("a net at level 1: %v", err)
	}
	if _, err := s.buyTool(id, "juggling"); !errors.Is(err, errNotHeld) {
		t.Errorf("a tool for no job: %v", err)
	}
	if err := s.write(func(tx *sql.Tx) error { return addXP(tx, id, f, "bugs", xpFor[toolLevel[1]]) }); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(time.Hour)
	before, _ := s.view(id)
	b, err := s.buyTool(id, "bugs")
	if err != nil {
		t.Fatal(err)
	}
	if b.Tool.Name != "Twig net" || b.Tool.Shop != shopMoss || b.Pond.Pebbles != before.Pebbles-toolPrices[1] || b.Pond.Jobs.Tools["bugs"] != 1 {
		t.Errorf("bought %+v, purse %d to %d", b.Tool, before.Pebbles, b.Pond.Pebbles)
	}
	w := b.Pond.Frogs[0].Work
	if w.Tool != 0 || w.Act != before.Frogs[0].Work.Act || w.Done != before.Frogs[0].Work.Done {
		t.Errorf("the basket on the go changed: %+v", w)
	}
	if _, err := s.buyTool(id, "bugs"); !errors.Is(err, errToolLevel) {
		t.Errorf("a reed net at level 10: %v", err)
	}
	c, err := s.collectJobs(id)
	if err != nil {
		t.Fatal(err)
	}
	if w := c.Pond.Frogs[0].Work; w.Tool != 1 || w.Act >= before.Frogs[0].Work.Act {
		t.Errorf("the next basket took no net: %+v", w)
	}
	if err := s.write(func(tx *sql.Tx) error { return addXP(tx, id, f, "bugs", xpFor[maxLevel]) }); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := s.buyTool(id, "bugs"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.buyTool(id, "bugs"); !errors.Is(err, errSoldOut) {
		t.Errorf("a fourth net: %v", err)
	}
	// A new frog at level 1 can't use the fine net, and works with nothing.
	exec(t, s, `INSERT INTO frogs (pond_id, seed, name, hatched_at, out_front) VALUES (?, 7, 'PIP', ?, 0)`, id, clock.Add(-48*time.Hour).Unix())
	v, _ := s.view(id)
	if _, err := s.setJob(id, v.Frogs[1].ID, "bugs", ""); err != nil {
		t.Fatal(err)
	}
	v, _ = s.view(id)
	if v.Frogs[1].Work.Tool != 0 || v.Frogs[1].Work.Levels[1].Tool != 0 || v.Jobs.Tools["bugs"] != toolTiers {
		t.Errorf("a new catcher with the fine net: %+v", v.Frogs[1].Work)
	}
	for _, it := range v.Items {
		if it.Kind == "Fine net" || it.Kind == "tool" {
			t.Errorf("a tool in the bag: %+v", it)
		}
	}
}

// A level up says what it opened, whatever kind of thing that is.
func TestALevelUpSaysWhatItOpened(t *testing.T) {
	s, clock, id, f := working(t, "bugs", "")
	if err := s.write(func(tx *sql.Tx) error { return addXP(tx, id, f, "bugs", xpFor[9]) }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.setJob(id, f, "bugs", "mayfly"); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(workHold)
	c, err := s.collectJobs(id)
	if err != nil {
		t.Fatal(err)
	}
	d := c.Done[0]
	if d.Level < 12 || d.Level >= 16 || d.Up != d.Level-9 {
		t.Fatalf("went from 9 to %d", d.Level)
	}
	got := map[string]string{}
	for _, o := range d.Opened {
		got[o.Kind] = o.Name
		if o.Level <= 9 || o.Level > d.Level {
			t.Errorf("opened %+v going from 9 to %d", o, d.Level)
		}
	}
	if got["tool"] != "Twig net" || got["mastery"] != "Mastery 3" || got["target"] != "Snails" || len(d.Opened) != 3 {
		t.Errorf("9 to 12 opened %+v", d.Opened)
	}
	for _, j := range jobDefs() {
		for i, o := range j.Opens {
			if i > 0 && o.Level < j.Opens[i-1].Level || o.Level < 2 || o.Name == "" || o.Key == "" {
				t.Errorf("%s opens %+v", j.Key, o)
			}
		}
	}
}

// Something opens every few levels early on and every week or two later.
func TestThereIsAlwaysSomethingToCome(t *testing.T) {
	for _, j := range jobs {
		if j.key == jobCraft {
			continue
		}
		day := playJob(j, ordinary)
		last := 1
		for _, o := range append(openings(j), opening{Level: maxLevel}) {
			if o.Level == last {
				continue
			}
			wait := day[o.Level] - day[last]
			if o.Level <= 30 && o.Level-last > 6 || wait > 30 {
				t.Errorf("%s: nothing between %d and %d, %.0f days", j.key, last, o.Level, wait)
			}
			last = o.Level
		}
	}
}

// The work badges count what came in at every target that brings the thing.
func TestTheWorkBadgesLand(t *testing.T) {
	s, _, id, f := working(t, "bugs", "")
	exec(t, s, `INSERT INTO mastery (pond_id, frog_id, job, target, acts, got) VALUES (?, ?, 'bugs', 'cricket', 150, 60), (?, ?, 'bugs', 'chorus', 50, 40), (?, ?, 'bugs', 'meadow', 3, 1)`, id, f, id, f, id, f)
	v, _ := s.view(id)
	if !badgeOf(v, "crickets_100").Got || !badgeOf(v, "first_firefly").Got || badgeOf(v, "mastered").Got {
		t.Errorf("crickets %+v, firefly %+v, mastered %+v", badgeOf(v, "crickets_100"), badgeOf(v, "first_firefly"), badgeOf(v, "mastered"))
	}
	exec(t, s, `UPDATE mastery SET acts = 100000 WHERE target = 'cricket'`)
	if v, _ = s.view(id); badgeOf(v, "mastered").Got {
		t.Error("mastered at level 1, past the cap")
	}
	if err := s.write(func(tx *sql.Tx) error { return addXP(tx, id, f, "bugs", xpFor[88]) }); err != nil {
		t.Fatal(err)
	}
	if v, _ = s.view(id); !badgeOf(v, "mastered").Got {
		t.Errorf("not mastered at 88: %+v", v.Frogs[0].Work.Targets)
	}
}

// A pond with nobody at work is told to put a frog to work, and the row
// opens the job screen, where the frog is ready to pick a target.
func TestAnIdlePondIsToldToWork(t *testing.T) {
	s, clock := testStore(t)
	_, id := newPond(t, s)
	*clock = clock.Add(2 * 24 * time.Hour)
	v, _ := s.view(id)
	name := titled(v.Frogs[0].Name)
	todo, goal := false, false
	for _, r := range v.Todo {
		todo = todo || r.Kind == "idle" && r.Label == "Put "+name+" to work" && r.To == "job"
	}
	for _, g := range v.Goals {
		goal = goal || g.Kind == "idle" && g.To == "job"
	}
	if !todo || (!goal && v.Starter == nil) {
		t.Errorf("an idle pond: todo %+v, goals %+v", v.Todo, v.Goals)
	}
	for _, r := range v.Todo {
		if (r.Kind == "shift" || r.Kind == "short") && r.To != "job" {
			t.Errorf("%s points at %s", r.Kind, r.To)
		}
	}
}

// wizardToTully is what one thing of a tier comes to on average at Little
// Tully's by way of the wizard, with every roll that isn't a hat taken back.
func wizardToTully() [tierCount]float64 {
	var w [tierCount]float64
	for range 100 {
		var next [tierCount]float64
		for from := range tierCount {
			for _, o := range []struct{ by, pct int }{{1, wizardOdds.up}, {0, wizardOdds.same}, {-1, 100 - wizardOdds.up - wizardOdds.same}} {
				to, p := min(from+o.by, tierCount-1), float64(o.pct)/100
				if to < 0 {
					next[from] += p * wizardDust / wizardTakes
					continue
				}
				hats := 1 / float64(len(wizardPool[to]))
				next[from] += p * (hats*float64(hatRate[to]/4) + (1-hats)*w[to]) / wizardTakes
			}
		}
		w = next
	}
	return w
}

// A bug catcher at 99 with every tool, full mastery, the nature for it and a
// legendary meal in it all day can't sell a day's catch for more than the
// day's room at any buyer, the wizard's hats included.
func TestADaysCatchSellsForNoMoreThanTheRoom(t *testing.T) {
	bugs, _ := jobOf("bugs")
	tg := bugs.best(maxLevel)
	m := masteryCap(maxLevel)
	boost := mealBoost[tierCount-1]
	act := float64(actSecs(tg, pace{maxLevel, m, toolTiers, true}))
	hit := 1 - tg.miss*(1-masteryMiss*float64(m-1))
	day := 24 * 3600 / act * hit * float64(tg.n) * (1 + masteryTwo*float64(m-1) + boost)
	n := int(math.Ceil(day/2)) * 2
	finds := map[string]float64{petFor("bugs"): 24 * speedAt(maxLevel) * (1 + boost) / petEvery}
	for _, x := range bugs.finds {
		finds[x.kind] = 24 * (1 + boost) / float64(x.per)
	}

	s, _ := testStore(t)
	_, id := newPond(t, s)
	give(t, s, id, 100000)
	purse := func() int { v, _ := s.view(id); return v.Pebbles }

	stock(t, s, id, tg.kind, n)
	for range n / 2 {
		if _, err := s.cook(id, tg.kind, tg.kind); err != nil {
			t.Fatal(err)
		}
	}
	jam, before := dishOf(6, 6).kind(), purse()
	for range n / 2 {
		if _, err := s.sellDish(id, jam); errors.Is(err, errBeanFull) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
	}
	bean := purse() - before

	stock(t, s, id, tg.kind, n)
	w, _ := worthOf(tg.kind)
	stall, left := 0, n
	for left > 0 {
		v, _ := s.view(id)
		for range v.Stall.Slots - len(v.Stall.Rows) {
			if left == 0 {
				break
			}
			if _, err := s.stallList(id, tg.kind, stallCap(tg.kind, w)); err != nil {
				t.Fatal(err)
			}
			left--
		}
		exec(t, s, `UPDATE stall SET sells_at = ? WHERE pond_id = ?`, s.now().Unix(), id)
		paid, err := s.stallCollect(id)
		if errors.Is(err, errStallPaid) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		stall += paid.Pebbles
	}

	moth := 0
	for k := range finds {
		if _, ok := trinketOf(k); !ok {
			continue
		}
		stock(t, s, id, k, 1+int(math.Ceil(finds[k])))
		for {
			before := purse()
			if _, err := s.sellTrinket(id, k); err != nil {
				break
			}
			moth += purse() - before
		}
	}

	wiz := wizardToTully()
	tier, _ := wizardTier(tg.kind)
	unlimited := day * wiz[tier]
	for k, per := range finds {
		if t, ok := wizardTier(k); ok {
			unlimited += per * wiz[t]
		}
	}
	// A pond with every hat already, so all the wizard turns out is a spare.
	for _, hs := range hatsByTier {
		for _, h := range hs {
			stock(t, s, id, hatKind(h), 1)
		}
	}
	stock(t, s, id, tg.kind, n)
	for range n / wizardTakes {
		if _, err := s.wizard(id, tg.kind); err != nil {
			t.Fatal(err)
		}
	}
	tully := 0
	v, _ := s.view(id)
	for _, h := range v.Hats {
		for range h.Count - 1 {
			before := purse()
			if _, err := s.sell(id, h.hat); err != nil {
				break
			}
			tully += purse() - before
		}
	}

	t.Logf("%.1f %s a day, which Big Bean would have paid %d for as jam with no limit", day, tg.kind, n/2*dishWorth[4])
	t.Logf("through the wizard Little Tully would pay about %.0f for a day with no limit", unlimited)
	t.Logf("a day's catch sold for %d at Big Bean's, %d off the stall, %d at Moth's and %d at Little Tully's", bean, stall, moth, tully)
	for route, got := range map[string]int{"Big Bean": bean, "the stall": stall, "Moth": moth, "Little Tully": tully} {
		if got > sellRoom {
			t.Errorf("%s paid %d for a day's catch, over the room of %d", route, got, sellRoom)
		}
	}
}
