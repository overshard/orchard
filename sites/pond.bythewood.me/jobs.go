package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"sync"
	"time"
)

// A frog's job is a run of actions on the server's clock, each one putting
// its thing in the basket and paying xp for it. A basket holds workHold of
// actions and then the frog stops until it's taken.
const (
	workHold = 12 * time.Hour
	maxLevel = 99
	jobCraft = "craft"
	// What a stick, a reed or a lump of clay goes for on the stall.
	materialWorth = 3
	// A miss pays a fifth of the xp the thing would have.
	missShare = 5
	// A frog works this much faster at the job its nature suits.
	natureSpeed = 1.25
	// How many actions past the one in progress the view deals out, so the
	// page can play them out between fetches.
	dealAhead = 3
	// A garden target that brings whatever is in season.
	inSeason = "season"
)

// The new things jobs bring in. Crafting uses them up.
var materials = map[string]string{"sticks": "Sticks", "reeds": "Reeds", "clay": "Clay"}

func init() {
	for k, n := range materials {
		itemNames[k] = n
	}
	for i := range jobs {
		if jobs[i].key == jobCraft {
			jobs[i].targets = craftTargets()
		}
	}
}

func isMaterial(kind string) bool { _, ok := materials[kind]; return ok }

// A find a job turns up now and then once a frog is good enough at it, at
// about one in every `per` hours of work whatever the target.
type jobFind struct {
	level int
	kind  string
	per   float64
}

// A target is one thing a job can be set to, opened at a level. An action
// takes mins at level 1, quicker as the frog gets better, and brings n of
// kind ("" is pebbles) unless it misses, which pays a fifth of the xp.
type target struct {
	key   string
	name  string
	level int
	mins  float64
	kind  string
	n     int
	xp    int
	miss  float64
	why   string
	// What a crafter uses up for each one made.
	in map[string]int
}

type job struct {
	key, name, verb string
	// The word for keeping at it, as in collect to keep catching.
	keep string
	// Who's best at it, and works it faster.
	nature  string
	targets []target
	finds   []jobFind
}

var jobs = []job{
	{"garden", "Gardening", "tending the plots", "tending", "GLUTTON", []target{
		{inSeason, "What's in season", 1, 60, inSeason, 1, 5000, 0, "", nil},
		{"watercress", "Watercress", 10, 45, "watercress", 1, 4100, 0, "", nil},
		{"lotus", "Lotus seeds", 25, 80, "lotus", 1, 8300, 0.2, "the seed head was empty", nil},
		{"morel", "Morels", 45, 180, "morel", 1, 21700, 0.5, "no morels under the log yet", nil},
	}, []jobFind{{25, "t_button", 40}, {50, "t_chanterelle", 90}, {99, "t_goldmorel", 2500}}},
	{"bugs", "Bug catching", "catching bugs", "catching", "NIGHT OWL", []target{
		{"cricket", "Crickets", 1, 30, "cricket", 1, 2500, 0.5, "the cricket hopped off", nil},
		{"mayfly", "Mayflies", 10, 40, "mayfly", 1, 3650, 0.4, "the mayfly got away", nil},
		{"firefly", "Fireflies", 25, 120, "firefly", 1, 12500, 0.5, "the firefly went dark", nil},
		{"snail", "Snails", 40, 45, "snail", 1, 5200, 0.2, "the snail pulled in and dropped off", nil},
	}, []jobFind{{75, "t_jar", 1500}}},
	{"sticks", "Stick gathering", "gathering sticks", "gathering", "DRIFTER", []target{
		{"twigs", "Twigs", 1, 20, "sticks", 1, 1650, 0, "", nil},
		{"branches", "Fallen branches", 15, 40, "sticks", 2, 3800, 0, "", nil},
		{"driftwood", "Driftwood", 30, 60, "sticks", 3, 6500, 0, "", nil},
		{"fence", "An old fence", 50, 100, "sticks", 5, 12500, 0, "", nil},
	}, []jobFind{{10, "t_acorn", 30}, {25, "t_pinecone", 30}, {50, "wood", 150}}},
	{"reeds", "Reed cutting", "cutting reeds", "cutting", "SINGER", []target{
		{"green", "Green reeds", 1, 20, "reeds", 1, 1650, 0, "", nil},
		{"tall", "Tall reeds", 15, 40, "reeds", 2, 3800, 0, "", nil},
		{"bulrush", "Bulrushes", 30, 60, "reeds", 3, 6500, 0, "", nil},
		{"farbank", "The far bank", 50, 100, "reeds", 5, 12500, 0, "", nil},
	}, []jobFind{{10, "feather", 40}, {25, "t_boat", 60}, {75, "t_duck", 300}}},
	{"mud", "Mud digging", "digging in the mud", "digging", "SHY", []target{
		{"soft", "Soft mud", 1, 30, "clay", 1, 2500, 0, "", nil},
		{"bank", "Bank clay", 15, 60, "clay", 2, 5700, 0, "", nil},
		{"grey", "Grey clay", 30, 90, "clay", 3, 9700, 0, "", nil},
		{"deep", "Deep clay", 50, 150, "clay", 5, 18700, 0, "", nil},
	}, []jobFind{{10, "agate", 10}, {25, "quartz", 20}, {40, "calcite", 40}, {50, "fossil", 200}, {75, "geode", 100}, {99, "moonstone", 1500}}},
	{"pebbles", "Pebble diving", "diving for pebbles", "diving", "BOLD", []target{
		{"shallows", "The shallows", 1, 30, "", 1, 2500, 0.5, "only mud down there", nil},
		{"pads", "Under the pads", 20, 60, "", 2, 6000, 0.5, "a fish got there first", nil},
		{"deep", "The deep end", 50, 120, "", 4, 15000, 0.5, "too deep this time", nil},
	}, []jobFind{{25, "geode", 150}, {50, "t_marble", 80}, {75, "t_earring", 400}, {99, "t_pearl", 3000}}},
	{jobCraft, "Crafting", "at the craft bench", "crafting", "", nil, nil},
}

func petFor(jobKey string) string {
	for k, j := range pets {
		if j == jobKey {
			return k
		}
	}
	return ""
}

func jobOf(key string) (job, bool) {
	for _, j := range jobs {
		if j.key == key {
			return j, true
		}
	}
	return job{}, false
}

// title is a target's name, a recipe's being the thing it makes.
func (t target) title() string {
	if t.name == "" {
		return thingName(t.kind)
	}
	return t.name
}

func (j job) target(key string) (target, bool) {
	for _, t := range j.targets {
		if t.key == key {
			return t, true
		}
	}
	return target{}, false
}

// best is the highest target a frog at this level can do.
func (j job) best(level int) target {
	t := j.targets[0]
	for _, x := range j.targets {
		if x.level <= level {
			t = x
		}
	}
	return t
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

// craftXP is what a recipe's hour of work at level 1 pays.
const craftXP = 5000

func craftTargets() []target {
	out := make([]target, 0, len(recipes))
	for _, r := range recipes {
		out = append(out, target{r.key, "", r.level, r.hours * 60, r.key, 1, int(r.hours * craftXP), 0, "", r.in})
	}
	return out
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

// A meal is the potion. A frog stays full longer on a better dish and half
// as long again on its favourite, and while it's full each thing it brings
// in has a chance of coming with a second.
var (
	mealHours = [tierCount]float64{8, 12, 18, 24, 48}
	mealBoost = [tierCount]float64{0.1, 0.2, 0.3, 0.4, 0.5}
)

func fullUntil(f frogRow) int64 {
	d, ok := dishOfKind(f.Fed)
	if !ok {
		return 0
	}
	hours := mealHours[d.tier]
	if f.Fed == f.FavDish {
		hours *= favFull
	}
	return f.FedAt + int64(hours*3600)
}

// mealAt is the meal's boost for an action that finished at `at`.
func mealAt(f frogRow, at int64) float64 {
	d, ok := dishOfKind(f.Fed)
	if !ok || at < f.FedAt || at >= fullUntil(f) {
		return 0
	}
	return mealBoost[d.tier]
}

func speedAt(level int) float64 { return 1 + float64(level-1)/98 }

// actSecs is how long one action takes, whole seconds so the page counts
// actions off the same numbers.
func actSecs(t target, level int, suited bool) int64 {
	s := t.mins * 60 / speedAt(level)
	if suited {
		s /= natureSpeed
	}
	return max(60, int64(math.Round(s)))
}

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

// ---------------------------------------------------------------- a run of work

// deal is what one action brought in.
type deal struct {
	miss  bool
	kind  string
	n     int
	xp    int
	finds []string
}

// run is a frog's work since it was last paid, worked out from when the
// unpaid actions began. Nothing about it is stored but that start and how
// many actions came before it, so reading it and paying it agree.
type run struct {
	f     frogRow
	j     job
	t     target
	level int
	act   int64
	cap   int
	deals []deal
	// "full" or "short" once the frog has stopped, and what a crafter ran out of.
	stop  string
	short string
	ahead []deal
	// The frog runs short after the actions dealt ahead.
	aheadShort bool
}

// seedOf keys an action's luck with the pepper, so nobody can work out from
// the code what a frog's next actions bring.
func seedOf(key []byte, frogID int64, target string, n int) (uint64, uint64) {
	mac := hmac.New(sha256.New, key)
	fmt.Fprintf(mac, "work|%d|%s|%d", frogID, target, n)
	sum := mac.Sum(nil)
	return binary.LittleEndian.Uint64(sum[:8]), binary.LittleEndian.Uint64(sum[8:16])
}

// dealAt works out action i of the run, counted from its start. Every draw is
// made whether it's used or not, so each one stays in its place.
func (r *run) dealAt(key []byte, i int) deal {
	end := r.f.WorkAt + int64(i+1)*r.act
	rng := rand.New(rand.NewPCG(seedOf(key, r.f.ID, r.t.key, r.f.WorkN+i)))
	d := deal{kind: r.t.kind, n: r.t.n}
	d.miss = rng.Float64() < r.t.miss
	boost := mealAt(r.f, end)
	if rng.Float64() < boost {
		d.n *= 2
	}
	if r.t.kind == inSeason {
		d.kind = ingredients[cropOf(time.Unix(end, 0), false, rng.IntN)].kind
	}
	hours := float64(r.act) / 3600
	for _, x := range r.j.finds {
		if rng.Float64() < hours*(1+boost)/x.per && r.level >= x.level {
			d.finds = append(d.finds, x.kind)
		}
	}
	if pet := petFor(r.j.key); rng.Float64() < hours*speedAt(r.level)*(1+boost)/petEvery && pet != "" {
		d.finds = append(d.finds, pet)
	}
	if d.miss {
		d.n = 0
		d.xp = r.t.xp / missShare
	} else {
		d.xp = r.t.xp * d.n / r.t.n
	}
	return d
}

// afford makes a crafter's deal fit the materials left, one where a meal's
// second can't be covered, and false when not even one can.
func (r *run) afford(d *deal, left map[string]int) bool {
	if r.t.in == nil || d.miss {
		return true
	}
	for d.n > 0 {
		ok := true
		for k, n := range r.t.in {
			if left[k] < n*d.n {
				ok = false
			}
		}
		if ok {
			for k, n := range r.t.in {
				left[k] -= n * d.n
			}
			d.xp = r.t.xp * d.n
			return true
		}
		d.n--
	}
	return false
}

// runOf works out a frog's basket as it stands now. left is the materials
// not yet spoken for by a crafter earlier in the pond, and comes back less
// what this one takes.
func runOf(key []byte, f frogRow, xp map[string]int, left map[string]int, now time.Time) (*run, bool) {
	j, ok := jobOf(f.Job)
	if !ok || f.WorkAt == 0 || !canWork(f, now) {
		return nil, false
	}
	t, ok := j.target(f.Target)
	if !ok {
		return nil, false
	}
	r := &run{f: f, j: j, t: t, level: levelOf(xp[f.Job])}
	suited := j.nature != "" && genomeOf(f.Seed, f.Mods).Nature == j.nature
	r.act = actSecs(t, r.level, suited)
	r.cap = max(1, int(int64(workHold/time.Second)/r.act))
	done := min(r.cap, int(max(0, now.Unix()-f.WorkAt)/r.act))
	for i := range done {
		d := r.dealAt(key, i)
		if !r.afford(&d, left) {
			r.stop, r.short = "short", shortOf(t, left)
			return r, true
		}
		r.deals = append(r.deals, d)
	}
	if done == r.cap {
		r.stop = "full"
		return r, true
	}
	spare := map[string]int{}
	for k, n := range left {
		spare[k] = n
	}
	for i := done; i < min(r.cap, done+dealAhead); i++ {
		d := r.dealAt(key, i)
		if !r.afford(&d, spare) {
			r.aheadShort, r.short = true, shortOf(t, spare)
			break
		}
		r.ahead = append(r.ahead, d)
	}
	return r, true
}

func shortOf(t target, left map[string]int) string {
	for _, k := range []string{"sticks", "reeds", "clay"} {
		if left[k] < t.in[k] {
			return k
		}
	}
	return ""
}

// startOf is when the action in progress began.
func (r *run) startOf() int64 { return r.f.WorkAt + int64(len(r.deals))*r.act }

// runs works out every frog's basket in the pond's order, crafters sharing
// what the pond holds first come first.
func runs(key []byte, frogs []frogRow, xp jobXP, held map[string]int, now time.Time) map[int64]*run {
	left := map[string]int{}
	for k := range materials {
		left[k] = held[k]
	}
	out := map[int64]*run{}
	for _, f := range frogs {
		if r, ok := runOf(key, f, xpOf(xp, f.ID), left, now); ok {
			out[f.ID] = r
		}
	}
	return out
}

type workDone struct {
	Frog    string         `json:"frog"`
	Job     string         `json:"job"`
	Target  string         `json:"target"`
	Actions int            `json:"actions"`
	Misses  int            `json:"misses"`
	Got     map[string]int `json:"got"`
	Pebbles int            `json:"pebbles"`
	// Finds and pets, apart from what the target brings.
	Finds map[string]int `json:"finds"`
	// Everything that came in as the basket showed it, and why a miss missed.
	Things []thingView `json:"things"`
	Why    string      `json:"why,omitempty"`
	XP     int         `json:"xp"`
	Level  int         `json:"level"`
	Up     int         `json:"up"`
	Stop   string      `json:"stop,omitempty"`
}

type workCollected struct {
	Done []workDone `json:"done"`
	Pond *view      `json:"pond"`
}

// pay hands over everything in a frog's basket and starts the next one where
// the action in progress began, or from now when the frog had stopped.
func pay(tx *sql.Tx, p *pond, r *run, xp map[string]int, now time.Time) (workDone, error) {
	before := levelOf(xp[r.f.Job])
	w := workDone{Frog: titled(frogViewOf(r.f, now).Name), Job: r.j.name, Target: r.t.title(), Actions: len(r.deals),
		Got: map[string]int{}, Finds: map[string]int{}, Things: basketOf(r.deals), Why: r.t.why, Level: before, Stop: r.stop}
	for _, d := range r.deals {
		w.XP += d.xp
		if d.miss {
			w.Misses++
		} else if d.kind == "" {
			w.Pebbles += d.n
		} else {
			w.Got[d.kind] += d.n
		}
		for _, k := range d.finds {
			w.Finds[k]++
		}
	}
	for k, n := range r.t.in {
		if made := w.Got[r.t.kind]; made > 0 {
			if err := takeItem(tx, p.ID, k, n*made); err != nil {
				return w, err
			}
		}
	}
	p.Pebbles += w.Pebbles
	for _, m := range []map[string]int{w.Got, w.Finds} {
		for k, n := range m {
			if err := giveN(tx, p, k, n); err != nil {
				return w, err
			}
		}
	}
	at := r.startOf()
	if r.stop != "" {
		at = now.Unix()
	}
	if _, err := tx.Exec(`UPDATE frogs SET work_at = ?, work_n = work_n + ? WHERE id = ?`, at, len(r.deals), r.f.ID); err != nil {
		return w, err
	}
	if err := addXP(tx, p.ID, r.f.ID, r.f.Job, w.XP); err != nil {
		return w, err
	}
	xp[r.f.Job] += w.XP
	w.Level = levelOf(xp[r.f.Job])
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

func canWork(f frogRow, now time.Time) bool {
	stage, _ := stageOf(now.Sub(time.Unix(f.HatchedAt, 0)))
	return stage != "egg"
}

// collectJobs takes every basket with something in it at once.
func (s *store) collectJobs(id int64) (*workCollected, error) {
	out := &workCollected{Done: []workDone{}}
	v, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		frogs, xp, held, err := jobState(tx, p.ID)
		if err != nil {
			return err
		}
		all := runs(s.pepper, frogs, xp, held, now)
		for _, f := range frogs {
			r := all[f.ID]
			if r == nil || len(r.deals) == 0 {
				continue
			}
			w, err := pay(tx, p, r, xpOf(xp, f.ID), now)
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

// settle pays one frog's basket, for when something is about to change what
// its unpaid actions would bring.
func (s *store) settle(tx *sql.Tx, p *pond, frogID int64, now time.Time) (*workDone, error) {
	frogs, xp, held, err := jobState(tx, p.ID)
	if err != nil {
		return nil, err
	}
	r := runs(s.pepper, frogs, xp, held, now)[frogID]
	if r == nil || len(r.deals) == 0 {
		return nil, nil
	}
	w, err := pay(tx, p, r, xpOf(xp, frogID), now)
	return &w, err
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

var errLevel = errors.New("that frog isn't good enough at it for that yet")

// setJob pays out whatever a frog had done and sets it to a target. With no
// target it takes the best one the frog can do, apart from crafting, which
// has to be told what to make.
func (s *store) setJob(id, frogID int64, key, tkey string) (*workCollected, error) {
	j, ok := jobOf(key)
	if !ok && key != "" {
		return nil, errNotHeld
	}
	out := &workCollected{Done: []workDone{}}
	v, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		frogs, xp, _, err := jobState(tx, p.ID)
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
		level := levelOf(xpOf(xp, f.ID)[key])
		if key != "" {
			if tkey == "" && key != jobCraft {
				tkey = j.best(level).key
			}
			t, ok := j.target(tkey)
			if !ok {
				return errNotHeld
			}
			if level < t.level {
				return errLevel
			}
		} else {
			tkey = ""
		}
		w, err := s.settle(tx, p, f.ID, now)
		if err != nil {
			return err
		}
		if w != nil {
			out.Done = append(out.Done, *w)
		}
		if key == f.Job && tkey == f.Target {
			return nil
		}
		at := now.Unix()
		if key == "" {
			at = 0
		}
		_, err = tx.Exec(`UPDATE frogs SET job = ?, target = ?, work_at = ? WHERE id = ?`, key, tkey, at, f.ID)
		return err
	})
	out.Pond = v
	return out, err
}

// migrateJobs moves a frog still working by the hour onto the first target of
// its job, with its held hours counted as actions from when it started and a
// crafter's part made thing carried over.
func migrateJobs(db *sql.DB, now time.Time) error {
	type old struct {
		id         int64
		job, rec   string
		at         int64
		carry      float64
		xp         int
		target     string
		hasTargets bool
	}
	rows, err := db.Query(`SELECT f.id, f.job, f.recipe, f.job_at, f.carry, COALESCE(x.xp, 0) FROM frogs f
		LEFT JOIN job_xp x ON x.frog_id = f.id AND x.job = f.job WHERE f.job != '' AND f.target = ''`)
	if err != nil {
		return err
	}
	var todo []old
	for rows.Next() {
		var o old
		if err := rows.Scan(&o.id, &o.job, &o.rec, &o.at, &o.carry, &o.xp); err != nil {
			rows.Close()
			return err
		}
		todo = append(todo, o)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, o := range todo {
		j, ok := jobOf(o.job)
		if !ok {
			continue
		}
		t := j.targets[0]
		at := o.at
		if at == 0 {
			at = now.Unix()
		}
		if j.key == jobCraft {
			if x, ok := j.target(o.rec); ok {
				t = x
			}
			at -= int64(o.carry * 3600 / speedAt(levelOf(o.xp)))
		}
		if _, err := db.Exec(`UPDATE frogs SET target = ?, work_at = ? WHERE id = ?`, t.key, at, o.id); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------- the view

type hoodView struct {
	hat
	Name string `json:"name"`
	Job  string `json:"job"`
	Got  bool   `json:"got"`
}

type jobLevel struct {
	Job   string `json:"job"`
	Level int    `json:"level"`
	XP    int    `json:"xp"`
	// Where this level starts and the next begins, for a bar.
	From int `json:"from"`
	To   int `json:"to"`
}

// thingView is a thing in a basket, "peb" for pebbles.
type thingView struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	Tier int    `json:"tier"`
	N    int    `json:"n"`
	Find bool   `json:"find,omitempty"`
}

type dealView struct {
	Miss bool        `json:"miss"`
	Got  []thingView `json:"got"`
	XP   int         `json:"xp"`
}

type workView struct {
	Job    string     `json:"job"`
	Target string     `json:"target,omitempty"`
	Levels []jobLevel `json:"levels"`
	Total  int        `json:"total"`
	// Seconds an action takes, seconds since the unpaid actions began and
	// how many of them are done, against how many the basket holds.
	Act   int64 `json:"act"`
	Since int64 `json:"since"`
	Done  int   `json:"done"`
	Cap   int   `json:"cap"`
	// Seconds until the basket is full, 0 once it is.
	FullIn int64       `json:"full_in"`
	Stop   string      `json:"stop,omitempty"`
	Short  string      `json:"short,omitempty"`
	Basket []thingView `json:"basket"`
	Misses int         `json:"misses"`
	XP     int         `json:"xp"`
	// The next actions as the server dealt them, which the page plays out
	// on time, and whether the frog runs short after them.
	Ahead      []dealView `json:"ahead"`
	AheadShort bool       `json:"ahead_short,omitempty"`
}

type findView struct {
	Level int    `json:"level"`
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	Tier  int    `json:"tier"`
}

type targetView struct {
	Key   string  `json:"key"`
	Name  string  `json:"name"`
	Level int     `json:"level"`
	Mins  float64 `json:"mins"`
	// What it brings, "season" for whatever's in season.
	Kind string         `json:"kind"`
	Tier int            `json:"tier"`
	N    int            `json:"n"`
	XP   int            `json:"xp"`
	Miss float64        `json:"miss"`
	Why  string         `json:"why,omitempty"`
	In   map[string]int `json:"in,omitempty"`
}

type jobDef struct {
	Key     string       `json:"key"`
	Name    string       `json:"name"`
	Verb    string       `json:"verb"`
	Keep    string       `json:"keep"`
	Nature  string       `json:"nature"`
	Targets []targetView `json:"targets"`
	Finds   []findView   `json:"finds"`
}

type jobsView struct {
	Defs []jobDef `json:"defs"`
	Hold int      `json:"hold"`
	// Frogs with something in their basket.
	Baskets int `json:"baskets"`
	// The pond's best level at each job, added up.
	Total int `json:"total"`
}

func tierOf(kind string) int {
	t, _ := wizardTier(kind)
	return t
}

// jobDefs waits for its first call, since a recipe's name is set by another
// file's init.
var jobDefs = sync.OnceValue(func() []jobDef {
	out := make([]jobDef, 0, len(jobs))
	for _, j := range jobs {
		d := jobDef{j.key, j.name, j.verb, j.keep, j.nature, []targetView{}, []findView{}}
		for _, t := range j.targets {
			kind := t.kind
			if kind == "" {
				kind = "peb"
			}
			d.Targets = append(d.Targets, targetView{t.key, t.title(), t.level, t.mins, kind, tierOf(t.kind), t.n, t.xp, t.miss, t.why, t.in})
		}
		for _, x := range j.finds {
			d.Finds = append(d.Finds, findView{x.level, x.kind, thingName(x.kind), tierOf(x.kind)})
		}
		out = append(out, d)
	}
	return out
})

func thingOf(kind string, n int, find bool) thingView {
	if kind == "" {
		return thingView{"peb", "Pebbles", 0, n, false}
	}
	return thingView{kind, thingName(kind), tierOf(kind), n, find}
}

// basketOf lists what came in, the target's things first and finds after.
func basketOf(deals []deal) []thingView {
	out := []thingView{}
	at := map[string]int{}
	add := func(kind string, n int, find bool) {
		if i, ok := at[kind]; ok {
			out[i].N += n
			return
		}
		at[kind] = len(out)
		out = append(out, thingOf(kind, n, find))
	}
	for _, d := range deals {
		if !d.miss {
			add(d.kind, d.n, false)
		}
	}
	for _, d := range deals {
		for _, k := range d.finds {
			add(k, 1, true)
		}
	}
	return out
}

func workOf(f frogRow, xp map[string]int, r *run, now time.Time) *workView {
	w := &workView{Job: f.Job, Target: f.Target, Basket: []thingView{}, Ahead: []dealView{}}
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
	if r == nil {
		return w
	}
	w.Act, w.Cap, w.Done, w.Stop, w.Short, w.AheadShort = r.act, r.cap, len(r.deals), r.stop, r.short, r.aheadShort
	w.Since = now.Unix() - f.WorkAt
	if r.stop == "" {
		w.FullIn = f.WorkAt + int64(r.cap)*r.act - now.Unix()
	}
	w.Basket = basketOf(r.deals)
	for _, d := range r.deals {
		w.XP += d.xp
		if d.miss {
			w.Misses++
		}
	}
	for _, d := range r.ahead {
		w.Ahead = append(w.Ahead, dealView{d.miss, basketOf([]deal{d}), d.xp})
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
