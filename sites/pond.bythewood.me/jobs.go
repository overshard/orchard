package main

import (
	"database/sql"
	"errors"
	"math"
	"math/rand/v2"
	"strings"
	"time"
)

// Every frog past the egg can hold one job. Work runs on the server's clock
// and piles up for workHold, then waits to be workCollected. A frog's level in a
// job only ever goes up, on RuneScape's curve to 99.
const (
	workHold  = 12 * time.Hour
	xpPerHour = 5000
	maxLevel  = 99
	jobCraft  = "craft"
	// What a stick, a reed or a lump of clay goes for on the stall.
	materialWorth = 3
)

// The new things jobs bring in. Crafting uses them up.
var materials = map[string]string{"sticks": "Sticks", "reeds": "Reeds", "clay": "Clay"}

func init() {
	for k, n := range materials {
		itemNames[k] = n
	}
}

func isMaterial(kind string) bool { _, ok := materials[kind]; return ok }

// A find a job turns up now and then once a frog is good enough at it, at
// about one in every `per` hours of work.
type jobFind struct {
	level int
	kind  string
	per   float64
}

type job struct {
	key, name, verb string
	// Who's best at it, and gets a quarter more out of it.
	nature string
	// What an hour of work brings in at level 1, which doubles by 99.
	rate  float64
	main  func(now time.Time, rain bool, level int) string
	finds []jobFind
}

var jobs = []job{
	{"garden", "Gardening", "tending the plots", "GLUTTON", 1,
		func(now time.Time, rain bool, _ int) string { return ingredients[rollCrop(now, rain)].kind },
		[]jobFind{{25, "t_button", 40}, {50, "t_chanterelle", 90}, {99, "t_goldmorel", 2500}}},
	{"bugs", "Bug catching", "catching bugs", "NIGHT OWL", 1,
		func(_ time.Time, _ bool, level int) string {
			r := rand.Float64()
			switch {
			case level >= 25 && r < 0.15:
				return "firefly"
			case level >= 10 && r < 0.45:
				return "mayfly"
			}
			return "cricket"
		},
		[]jobFind{{75, "t_jar", 1500}}},
	{"sticks", "Stick gathering", "gathering sticks", "DRIFTER", 3,
		func(time.Time, bool, int) string { return "sticks" },
		[]jobFind{{10, "t_acorn", 30}, {25, "t_pinecone", 30}, {50, "wood", 150}}},
	{"reeds", "Reed cutting", "cutting reeds", "SINGER", 3,
		func(time.Time, bool, int) string { return "reeds" },
		[]jobFind{{10, "feather", 40}, {25, "t_boat", 60}, {75, "t_duck", 300}}},
	{"mud", "Mud digging", "digging in the mud", "SHY", 2,
		func(time.Time, bool, int) string { return "clay" },
		[]jobFind{{10, "agate", 10}, {25, "quartz", 20}, {40, "calcite", 40}, {50, "fossil", 200}, {75, "geode", 100}, {99, "moonstone", 1500}}},
	{"pebbles", "Pebble diving", "diving for pebbles", "BOLD", 1,
		func(time.Time, bool, int) string { return "" },
		[]jobFind{{25, "geode", 150}, {50, "t_marble", 80}, {75, "t_earring", 400}, {99, "t_pearl", 3000}}},
	{jobCraft, "Crafting", "at the craft bench", "", 0, nil, nil},
}

func jobOf(key string) (job, bool) {
	for _, j := range jobs {
		if j.key == key {
			return j, true
		}
	}
	return job{}, false
}

// A recipe at the craft bench, made from materials in `hours` of work at
// level 1, and quicker as the frog gets better.
type recipe struct {
	key   string
	level int
	hours float64
	in    map[string]int
}

var recipes = []recipe{
	{"t_boat", 1, 0.5, map[string]int{"reeds": 3}},
	{"t_raft", 1, 1, map[string]int{"sticks": 6, "reeds": 2}},
	{"t_basket", 8, 1, map[string]int{"reeds": 8}},
	{"t_pot", 15, 2, map[string]int{"clay": 6}},
	{"stamp_spots", 20, 2, map[string]int{"clay": 10}},
	{"stamp_bands", 30, 2, map[string]int{"clay": 10, "sticks": 2}},
	{"stamp_speckled", 40, 3, map[string]int{"clay": 12, "sticks": 2}},
	{"t_flute", 50, 4, map[string]int{"reeds": 20, "sticks": 4}},
	{"stamp_mask", 55, 3, map[string]int{"clay": 14, "reeds": 4}},
	{"t_gnome", 60, 6, map[string]int{"clay": 30, "sticks": 6}},
	{"stamp_stripe", 70, 3, map[string]int{"clay": 16, "reeds": 6}},
	{"t_statue", 80, 8, map[string]int{"clay": 40, "sticks": 20}},
}

func recipeOf(key string) (recipe, bool) {
	for _, r := range recipes {
		if r.key == key {
			return r, true
		}
	}
	return recipe{}, false
}

// xpFor is the xp a level starts at, RuneScape's table, so 92 is about half
// of 99.
var xpFor = func() [maxLevel + 1]int {
	var t [maxLevel + 1]int
	pts := 0.0
	for l := 1; l < maxLevel; l++ {
		pts += math.Floor(float64(l) + 300*math.Pow(2, float64(l)/7))
		t[l+1] = int(math.Floor(pts / 4))
	}
	return t
}()

func levelOf(xp int) int {
	l := 1
	for l < maxLevel && xp >= xpFor[l+1] {
		l++
	}
	return l
}

// A meal is the potion. A frog stays full longer on a better dish, and while
// it's full it works faster, lays a better egg and learns quicker.
var (
	mealHours = [tierCount]float64{8, 12, 18, 24, 48}
	mealBoost = [tierCount]float64{0.1, 0.2, 0.3, 0.4, 0.5}
)

func fullUntil(f frogRow) int64 {
	d, ok := dishOfKind(f.Fed)
	if !ok {
		return 0
	}
	return f.FedAt + int64(mealHours[d.tier]*3600)
}

// worked is the hours of work held since `at`, and how many of them were on a
// full stomach.
func worked(f frogRow, now time.Time) (hours, fed float64) {
	if f.Job == "" || f.JobAt == 0 {
		return 0, 0
	}
	from := f.JobAt
	to := min(now.Unix(), from+int64(workHold/time.Second))
	if to <= from {
		return 0, 0
	}
	hours = float64(to-from) / 3600
	if until := fullUntil(f); until > from {
		fed = float64(min(to, until)-max(from, f.FedAt)) / 3600
		fed = max(0, min(fed, hours))
	}
	return hours, fed
}

// boosted is work hours with the meal's hours counted for more.
func boosted(f frogRow, hours, fed float64) float64 {
	d, ok := dishOfKind(f.Fed)
	if !ok {
		return hours
	}
	return hours + fed*mealBoost[d.tier]
}

// roll turns an expected count into a whole one, the fraction left to chance.
func roll(x float64) int {
	n := int(x)
	if rand.Float64() < x-float64(n) {
		n++
	}
	return n
}

func speedAt(level int) float64 { return 1 + float64(level-1)/98 }

type jobXP map[int64]map[string]int

func loadXP(q querier, pondID int64) (jobXP, error) {
	rows, err := q.Query(`SELECT frog_id, job, xp FROM job_xp WHERE pond_id = ?`, pondID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := jobXP{}
	for rows.Next() {
		var id int64
		var j string
		var xp int
		if err := rows.Scan(&id, &j, &xp); err != nil {
			return nil, err
		}
		if out[id] == nil {
			out[id] = map[string]int{}
		}
		out[id][j] = xp
	}
	return out, rows.Err()
}

func addXP(tx *sql.Tx, pondID, frogID int64, j string, xp int) error {
	if xp <= 0 {
		return nil
	}
	_, err := tx.Exec(`INSERT INTO job_xp (pond_id, frog_id, job, xp) VALUES (?, ?, ?, ?)
		ON CONFLICT (frog_id, job) DO UPDATE SET xp = xp + excluded.xp`, pondID, frogID, j, xp)
	return err
}

// giveN puts n of a kind in the pond, a trinket by way of the set it belongs to.
func giveN(tx *sql.Tx, p *pond, kind string, n int) error {
	if kind == "" {
		p.Pebbles += n
		return nil
	}
	if strings.HasPrefix(kind, "t_") {
		for range n {
			if _, err := addTrinket(tx, p.ID, kind); err != nil {
				return err
			}
		}
		return nil
	}
	return addItem(tx, p.ID, kind, n)
}

type workDone struct {
	Frog    string         `json:"frog"`
	Job     string         `json:"job"`
	Got     map[string]int `json:"got"`
	Pebbles int            `json:"pebbles"`
	Level   int            `json:"level"`
	Up      int            `json:"up"`
	// Why a crafter made nothing.
	Short bool `json:"short,omitempty"`
}

type workCollected struct {
	Done []workDone `json:"done"`
	Pond *view      `json:"pond"`
}

// collectWork pays out one frog's held work and starts its clock again.
func collectWork(tx *sql.Tx, p *pond, f frogRow, xp map[string]int, held map[string]int, now time.Time, rain bool) (workDone, error) {
	j, _ := jobOf(f.Job)
	before := levelOf(xp[f.Job])
	hours, fed := worked(f, now)
	w := workDone{Frog: titled(frogViewOf(f, now).Name), Job: j.name, Got: map[string]int{}, Level: before}
	eff := boosted(f, hours, fed)
	gained := 0
	if j.key == jobCraft {
		r, ok := recipeOf(f.Recipe)
		if !ok || before < r.level {
			return w, nil
		}
		progress := f.Carry + eff*speedAt(before)
		made := int(progress / r.hours)
		for k, n := range r.in {
			made = min(made, held[k]/n)
		}
		if made == 0 && progress >= r.hours {
			w.Short = true
		}
		for k, n := range r.in {
			if made > 0 {
				if err := takeItem(tx, p.ID, k, n*made); err != nil {
					return w, err
				}
				held[k] -= n * made
			}
		}
		if made > 0 {
			if err := giveN(tx, p, r.key, made); err != nil {
				return w, err
			}
			w.Got[r.key] = made
		}
		// Work past what the materials cover waits at most one item's worth.
		carry := min(progress-float64(made)*r.hours, r.hours)
		if _, err := tx.Exec(`UPDATE frogs SET job_at = ?, carry = ? WHERE id = ?`, now.Unix(), carry, f.ID); err != nil {
			return w, err
		}
		gained = int(float64(made) * r.hours * xpPerHour)
	} else {
		level := before
		mult := speedAt(level)
		if g := genomeOf(f.Seed, f.Mods); g.Nature == j.nature {
			mult *= 1.25
		}
		n := roll(eff * j.rate * mult)
		if j.key == "pebbles" {
			w.Pebbles = n
			p.Pebbles += n
		} else {
			for range n {
				w.Got[j.main(now, rain, level)]++
			}
		}
		for _, x := range j.finds {
			if level >= x.level {
				if k := roll(eff / x.per); k > 0 {
					w.Got[x.kind] += k
				}
			}
		}
		for k, c := range w.Got {
			if err := giveN(tx, p, k, c); err != nil {
				return w, err
			}
		}
		if _, err := tx.Exec(`UPDATE frogs SET job_at = ? WHERE id = ?`, now.Unix(), f.ID); err != nil {
			return w, err
		}
		gained = int(eff * xpPerHour)
	}
	if err := addXP(tx, p.ID, f.ID, f.Job, gained); err != nil {
		return w, err
	}
	xp[f.Job] += gained
	w.Level = levelOf(xp[f.Job])
	w.Up = w.Level - before
	return w, nil
}

func heldItems(q querier, id int64) (map[string]int, error) {
	rows, err := q.Query(`SELECT kind, count FROM items WHERE pond_id = ? AND count > 0`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, rows.Err()
}

// workersDone counts the frogs whose shift is full and waiting.
func workersDone(frogs []frogRow, now time.Time) int {
	n := 0
	for _, f := range frogs {
		if h, _ := worked(f, now); h >= workHold.Hours() {
			n++
		}
	}
	return n
}

func canWork(f frogRow, now time.Time) bool {
	stage, _ := stageOf(now.Sub(time.Unix(f.HatchedAt, 0)))
	return stage != "egg"
}

// collect pays out every working frog at once.
func (s *store) collectJobs(id int64) (*workCollected, error) {
	out := &workCollected{Done: []workDone{}}
	v, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		frogs, xp, held, err := jobState(tx, p.ID)
		if err != nil {
			return err
		}
		rain := dayOfReading(s.day.reading(now), now).Rain
		for _, f := range frogs {
			if h, _ := worked(f, now); h < 1.0/60 {
				continue
			}
			w, err := collectWork(tx, p, f, xpOf(xp, f.ID), held, now, rain)
			if err != nil {
				return err
			}
			out.Done = append(out.Done, w)
		}
		if len(out.Done) == 0 {
			return errNotYet
		}
		return nil
	})
	out.Pond = v
	return out, err
}

func jobState(tx *sql.Tx, pondID int64) ([]frogRow, jobXP, map[string]int, error) {
	frogs, err := loadFrogs(tx, pondID)
	if err != nil {
		return nil, nil, nil, err
	}
	xp, err := loadXP(tx, pondID)
	if err != nil {
		return nil, nil, nil, err
	}
	held, err := heldItems(tx, pondID)
	return frogs, xp, held, err
}

func xpOf(all jobXP, frogID int64) map[string]int {
	if all[frogID] == nil {
		all[frogID] = map[string]int{}
	}
	return all[frogID]
}

var errLevel = errors.New("that frog isn't good enough at crafting for that yet")

// setJob pays out whatever a frog had done and puts it to something new. A
// crafter also says what it makes.
func (s *store) setJob(id, frogID int64, key, rec string) (*workCollected, error) {
	if _, ok := jobOf(key); !ok && key != "" {
		return nil, errNotHeld
	}
	out := &workCollected{Done: []workDone{}}
	v, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		frogs, xp, held, err := jobState(tx, p.ID)
		if err != nil {
			return err
		}
		var f *frogRow
		for i := range frogs {
			if frogs[i].ID == frogID {
				f = &frogs[i]
			}
		}
		if f == nil {
			return errNotHeld
		}
		if !canWork(*f, now) {
			return errNotYet
		}
		if key == jobCraft {
			r, ok := recipeOf(rec)
			if !ok {
				return errNotHeld
			}
			if levelOf(xpOf(xp, f.ID)[jobCraft]) < r.level {
				return errLevel
			}
		} else {
			rec = ""
		}
		if h, _ := worked(*f, now); h >= 1.0/60 {
			w, err := collectWork(tx, p, *f, xpOf(xp, f.ID), held, now, dayOfReading(s.day.reading(now), now).Rain)
			if err != nil {
				return err
			}
			out.Done = append(out.Done, w)
		}
		carry := f.Carry
		if key != f.Job || rec != f.Recipe {
			carry = 0
		}
		at := now.Unix()
		if key == "" {
			at = 0
		}
		_, err = tx.Exec(`UPDATE frogs SET job = ?, recipe = ?, job_at = ?, carry = ? WHERE id = ?`, key, rec, at, carry, f.ID)
		return err
	})
	out.Pond = v
	return out, err
}

// ---------------------------------------------------------------- the view

type jobLevel struct {
	Job   string `json:"job"`
	Level int    `json:"level"`
	XP    int    `json:"xp"`
	// Where this level starts and the next begins, for a bar.
	From int `json:"from"`
	To   int `json:"to"`
}

type workView struct {
	Job    string     `json:"job"`
	Recipe string     `json:"recipe,omitempty"`
	Hours  float64    `json:"hours"`
	Full   bool       `json:"full"`
	Levels []jobLevel `json:"levels"`
	Total  int        `json:"total"`
}

type findView struct {
	Level int    `json:"level"`
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	Tier  int    `json:"tier"`
}

type jobDef struct {
	Key    string     `json:"key"`
	Name   string     `json:"name"`
	Verb   string     `json:"verb"`
	Nature string     `json:"nature"`
	Rate   float64    `json:"rate"`
	Finds  []findView `json:"finds"`
}

type recipeView struct {
	Kind  string         `json:"kind"`
	Name  string         `json:"name"`
	Tier  int            `json:"tier"`
	Level int            `json:"level"`
	Hours float64        `json:"hours"`
	In    map[string]int `json:"in"`
}

type jobsView struct {
	Defs    []jobDef     `json:"defs"`
	Recipes []recipeView `json:"recipes"`
	Hold    int          `json:"hold"`
	// Hours of work waiting across every frog.
	Waiting float64 `json:"waiting"`
	// The pond's best level at each job, added up.
	Total int `json:"total"`
}

func jobDefs() []jobDef {
	out := make([]jobDef, 0, len(jobs))
	for _, j := range jobs {
		d := jobDef{j.key, j.name, j.verb, j.nature, j.rate, []findView{}}
		for _, x := range j.finds {
			t, _ := wizardTier(x.kind)
			d.Finds = append(d.Finds, findView{x.level, x.kind, thingName(x.kind), t})
		}
		out = append(out, d)
	}
	return out
}

func recipeViews() []recipeView {
	out := make([]recipeView, 0, len(recipes))
	for _, r := range recipes {
		t, _ := wizardTier(r.key)
		out = append(out, recipeView{r.key, thingName(r.key), t, r.level, r.hours, r.in})
	}
	return out
}

func workOf(f frogRow, xp map[string]int, now time.Time) *workView {
	h, _ := worked(f, now)
	w := &workView{Job: f.Job, Recipe: f.Recipe, Hours: math.Round(h*100) / 100, Full: fullUntil(f) > now.Unix()}
	for _, j := range jobs {
		x := xp[j.key]
		l := levelOf(x)
		to := xpFor[min(l+1, maxLevel)]
		if l == maxLevel {
			to = xpFor[maxLevel]
		}
		w.Levels = append(w.Levels, jobLevel{j.key, l, x, xpFor[l], to})
		w.Total += l
	}
	return w
}

// bestLevels is the pond's total level, its best frog at each job.
func bestLevels(xp jobXP) int {
	best := map[string]int{}
	for _, m := range xp {
		for j, x := range m {
			best[j] = max(best[j], levelOf(x))
		}
	}
	total := 0
	for _, j := range jobs {
		total += max(1, best[j.key])
	}
	return total
}
