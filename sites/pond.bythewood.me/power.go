package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"time"
)

// Frog power is a gauge of everything a pond has done. Each part has a
// weight, and a pond holds the share of it it has found, played or raised,
// so the whole game comes to powerWhole and no one part can swamp the rest.
const powerWhole = 100_000

// powerWeights is every part and its weight. Each game in Play and Dive
// counts gameWeight on top of these, and a test holds the lot to powerWhole.
var powerWeights = []struct {
	key, name, group string
	weight           int
}{
	{"hats", "Hats", "finds", 13000},
	{"trinkets", "Trinkets", "finds", 6000},
	{"hoods", "Hoods", "finds", 4000},
	{"sites", "Site hats", "finds", 1500},
	{"rocks", "Rocks", "finds", 2500},
	{"keepsakes", "Keepsakes", "finds", 4000},
	{"mementos", "Mementos", "finds", 3000},
	{"fish", "Fish", "finds", 5000},
	{"looks", "Frog looks", "finds", 3000},
	{"cookbook", "Cookbook", "finds", 4000},
	{"furniture", "Furniture", "finds", 1500},
	{"sets", "Sets", "finds", 2700},
	{"badges", "Badges", "finds", 5000},
	{"frogs", "Frogs", "frogs", 3000},
	{"rarity", "Rarest frog", "frogs", 3000},
	{"bond", "Bond", "frogs", 2000},
	{"jobs", "Job levels", "frogs", 10000},
	{"mastery", "Mastery", "frogs", 4000},
	{"front", "Out front", "frogs", 3000},
}

const gameWeight = 1100

// A thing counts by its tier inside its part, so a legendary hat is four
// commons and the rare end of a shelf doesn't take all of it.
var tierWeight = [tierCount]float64{2, 3, 4, 6, 8}

// powerGames is Dive and every side game, in the order the breakdown lists them.
func powerGames() []arcadeGame {
	return append([]arcadeGame{{key: "dive", name: "Dive"}}, arcadeGames...)
}

// The most frogs a pond can keep, every pad bought and a burrow dug at every level.
const mostFrogs = mostPads + buildLevels

// The commonest and rarest frog a plain egg hatches, which the rarity part
// runs between on a log scale.
var commonestEgg, rarestEgg = func() (int, int) {
	lo, hi := morphs[0].W, morphs[0].W
	for _, m := range morphs {
		lo, hi = min(lo, m.W), max(hi, m.W)
	}
	return int(math.Floor(1/(float64(hi)/1000*2/7*0.5) + 0.5)), int(math.Floor(1/(float64(lo)/1000/7*0.25) + 0.5))
}()

// The register's tiers, the same as collect.js gives each colour, back and pupil.
var (
	morphTier   = map[string]int{"GREEN": 0, "OLIVE": 0, "WOOD BROWN": 0, "GREY": 1, "GOLDEN": 2, "RED EYE": 2, "STRAWBERRY": 3, "BLUE": 3, "ALBINO": 4}
	patternTier = map[string]int{"PLAIN": 0, "SPOTS": 0, "BANDS": 1, "SPECKLED": 1, "MASK": 1, "BACK STRIPE": 2}
	pupilTier   = map[string]int{"BAR": 0, "ROUND": 1, "SLIT": 1}
)

type piece struct {
	tier int
	got  bool
}

// powerIn is everything power reads, counted once off the view.
type powerIn struct {
	finds map[string][]piece
	// Bests by game key, Dive as "dive", and what each game counts against.
	games, refs map[string]int
	frogs       int
	rarest      int
	bond        int
	levels      map[string]int
	// The best mastery any frog has at each target, by job/target.
	mastery map[string]int
	// The tier of what the frog out front wears and has round its pad, -1 for nothing.
	front []int
}

type powerPart struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Group string `json:"group"`
	Pts   int    `json:"pts"`
	Of    int    `json:"of"`
	// How much of the part there is to have, in things, levels or points,
	// and that said as a line.
	Have int    `json:"have"`
	All  int    `json:"all"`
	Says string `json:"says"`
}

func (x powerPart) says() string {
	switch {
	case x.Key == "rarity":
		return "1 in " + commas(x.Have)
	case x.Key == "bond":
		return fmt.Sprintf("%d of %d hearts", x.Have, x.All)
	case x.Key == "jobs":
		return "level " + commas(x.Have) + " of " + commas(x.All)
	case strings.HasPrefix(x.Key, "game_"):
		return "best " + commas(x.Have) + " of " + commas(x.All)
	}
	return commas(x.Have) + " of " + commas(x.All)
}

type rankView struct {
	At   int    `json:"at"`
	Name string `json:"name"`
	Got  bool   `json:"got"`
}

type power struct {
	Total int    `json:"total"`
	Rank  string `json:"rank"`
	// The whole ladder, how far to the next rung and how full the bar is, in
	// a hundred, with every step the same width and the top rung filling it.
	Ranks    []rankView  `json:"ranks"`
	Next     int         `json:"next"`
	NextName string      `json:"next_name"`
	Fill     int         `json:"fill"`
	Parts    []powerPart `json:"parts"`
	// Rise is what went up since the pond last saw its power, on a move only.
	Rise *powerRise `json:"rise,omitempty"`

	// The rung the total stands on and the highest the pond has reached.
	at, top int
}

func (p power) part(key string) powerPart {
	for _, x := range p.Parts {
		if x.Key == key {
			return x
		}
	}
	return powerPart{}
}

// weighed is the share of a part a shelf of things gives, by tier.
func weighed(ps []piece) (have, all int, share float64) {
	var got, of float64
	for _, x := range ps {
		of += tierWeight[x.tier]
		if x.got {
			got += tierWeight[x.tier]
			have++
		}
	}
	if of == 0 {
		return have, len(ps), 0
	}
	return have, len(ps), got / of
}

func powerOf(in powerIn, top int) power {
	var p power
	add := func(key, name, group string, weight, have, all int, share float64) {
		pts := int(math.Round(float64(weight) * clamp(share, 0, 1)))
		x := powerPart{key, name, group, pts, weight, have, all, ""}
		x.Says = x.says()
		p.Parts = append(p.Parts, x)
		p.Total += pts
	}
	for _, w := range powerWeights {
		var have, all int
		var share float64
		switch w.key {
		case "frogs":
			have, all = in.frogs, mostFrogs
			share = float64(have) / float64(all)
		case "rarity":
			have, all = max(in.rarest, commonestEgg), rarestEgg
			share = math.Log(float64(have)/float64(commonestEgg)) / math.Log(float64(rarestEgg)/float64(commonestEgg))
		case "bond":
			have, all = min(in.bond, bondHearts*bondHeart)/bondHeart, bondHearts
			share = float64(min(in.bond, bondHearts*bondHeart)) / float64(bondHearts*bondHeart)
		case "jobs":
			all = len(jobs) * maxLevel
			for _, j := range jobs {
				have += min(maxLevel, max(1, in.levels[j.key]))
			}
			share = float64(have-len(jobs)) / float64(all-len(jobs))
		case "mastery":
			n := 0
			for _, j := range jobs {
				for _, t := range j.targets {
					have += min(masteryTop, max(1, in.mastery[masteryKey(j.key, t.key)])) - 1
					n++
				}
			}
			all = n * (masteryTop - 1)
			share = float64(have) / float64(all)
		case "front":
			var got float64
			for _, t := range in.front {
				if t >= 0 {
					got += tierWeight[t]
					have++
				}
			}
			all = 1 + len(trinketSpots)
			share = got / (float64(all) * tierWeight[tierCount-1])
		default:
			have, all, share = weighed(in.finds[w.key])
		}
		add(w.key, w.name, w.group, w.weight, have, all, share)
	}
	for _, g := range powerGames() {
		best, ref := in.games[g.key], max(1, in.refs[g.key])
		add("game_"+g.key, g.name, "games", gameWeight, best, ref, float64(best)/float64(ref))
	}
	p.climb(top)
	return p
}

// ---------------------------------------------------------------- the ladder

// The ladder runs to a pond that has played every game as well as most
// players who keep at it and found, raised and done everything else, with a
// long tail so the last rungs take months.
var ranks = []struct {
	at   int
	name string
}{
	{0, "NEW TO THE POND"}, {12000, "POND REGULAR"}, {28000, "LILY LORD"}, {42000, "REED WARDEN"},
	{52000, "MARSH BARON"}, {62000, "FEN DUKE"}, {72000, "BOG MONARCH"}, {82000, "MIRE SOVEREIGN"},
	{92000, "SWAMP LEGEND"},
}

func rungOf(total int) int {
	at := 0
	for i, k := range ranks {
		if total >= k.at {
			at = i
		}
	}
	return at
}

func rungNamed(name string) int {
	for i, k := range ranks {
		if k.name == name {
			return i
		}
	}
	return 0
}

// climb finds the rung a total stands on. A pond keeps the highest rung it
// has reached, top, so a game's reference rising never takes a rank away.
func (p *power) climb(top int) {
	p.at, p.top = rungOf(p.Total), max(top, rungOf(p.Total))
	i := p.top
	p.Ranks = p.Ranks[:0]
	for j, k := range ranks {
		p.Ranks = append(p.Ranks, rankView{k.at, k.name, j <= i})
	}
	p.Rank = ranks[i].name
	p.Next, p.NextName, p.Fill = 0, "", 100
	if i+1 < len(ranks) {
		k, nx := ranks[i], ranks[i+1]
		p.Next, p.NextName = nx.at-p.Total, nx.name
		step := clamp(float64(p.Total-k.at)/float64(nx.at-k.at), 0, 1)
		p.Fill = int((100*float64(i) + 100*step) / float64(len(ranks)-1))
	}
}

// ---------------------------------------------------------------- counting

// powerInOf counts what power reads off a finished view. bond is the best
// bond any of the pond's frogs has.
func powerInOf(v *view, refs map[string]int, bond int) powerIn {
	in := powerIn{finds: map[string][]piece{}, games: map[string]int{"dive": v.Dive.Best}, refs: refs,
		frogs: len(v.Frogs), bond: bond, levels: map[string]int{}, mastery: map[string]int{}}
	f := in.finds
	for _, h := range v.Hats {
		f["hats"] = append(f["hats"], piece{h.Tier, v.hatsEver[h.hat]})
	}
	for _, h := range v.Hoods {
		f["hoods"] = append(f["hoods"], piece{h.tier(), h.Got})
	}
	for _, s := range v.Sites {
		f["sites"] = append(f["sites"], piece{s.tier(), s.Got})
	}
	for _, t := range v.Trinkets.All {
		f["trinkets"] = append(f["trinkets"], piece{t.Tier, t.Found})
	}
	for _, r := range v.Rocks {
		f["rocks"] = append(f["rocks"], piece{r.Tier, r.Found})
	}
	for _, k := range v.Keepsakes {
		f["keepsakes"] = append(f["keepsakes"], piece{k.Tier, k.First != ""})
	}
	for _, m := range v.Mementos {
		f["mementos"] = append(f["mementos"], piece{m.Tier, m.Day != ""})
	}
	for _, x := range v.Fishing.All {
		f["fish"] = append(f["fish"], piece{x.Tier, x.Count > 0})
	}
	f["looks"] = looksOf(v.Frogs)
	cooked := map[int]int{}
	for _, d := range v.Kitchen.Cookbook {
		cooked[d.Tier]++
	}
	for _, t := range dishTiers {
		f["cookbook"] = append(f["cookbook"], piece{t, cooked[t] > 0})
		cooked[t]--
	}
	for _, x := range furniture {
		f["furniture"] = append(f["furniture"], piece{0, slices.Contains(v.Furniture, x.kind)})
	}
	for _, s := range v.Sets {
		f["sets"] = append(f["sets"], piece{0, s.Done})
	}
	for _, b := range v.Badges {
		f["badges"] = append(f["badges"], piece{0, b.Got})
	}
	for _, g := range v.Arcade {
		in.games[g.Key] = g.Best
	}
	for _, fr := range v.Frogs {
		in.rarest = max(in.rarest, fr.Rarity.N)
		if fr.Work == nil {
			continue
		}
		for _, l := range fr.Work.Levels {
			in.levels[l.Job] = max(in.levels[l.Job], l.Level)
		}
		for _, m := range fr.Work.Targets {
			k := masteryKey(m.Job, m.Target)
			in.mastery[k] = max(in.mastery[k], m.Level)
		}
	}
	in.front = []int{-1}
	if h := v.Frog.Hat; h.Shape != 0 {
		in.front[0] = h.tier()
	}
	for _, k := range v.Trinkets.Spots {
		t, ok := trinketOf(k)
		if !ok || v.held[k] < 1 {
			in.front = append(in.front, -1)
			continue
		}
		in.front = append(in.front, t.tier)
	}
	return in
}

// looksOf is the register, every colour, back and pupil a hatched frog of the
// pond's has shown.
func looksOf(frogs []reedView) []piece {
	seen := map[string]bool{}
	for _, f := range frogs {
		if f.Stage != "egg" {
			seen[f.Morph], seen[f.Pattern], seen[f.Pupil] = true, true, true
		}
	}
	var out []piece
	for _, set := range []map[string]int{morphTier, patternTier, pupilTier} {
		keys := make([]string, 0, len(set))
		for k := range set {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			out = append(out, piece{set[k], seen[k]})
		}
	}
	return out
}

// ---------------------------------------------------------------- references

// A game counts against the median best of the ponds with refRuns runs of
// it, once there are refPonds of them. A higher percentile would leave the
// games' share to the few best players.
const (
	refRuns  = 5
	refPonds = 10
)

// refFloor is what a game counts against until enough ponds play it, and
// never under. It's about what a decent bot or a careful week of play scores.
var refFloor = map[string]int{
	"dive":   25,
	"flick":  40,
	"cross":  80,
	"raft":   60,
	"leap":   60,
	"school": 30,
	"fish":   100,
	"stack":  40,
	"shell":  36,
	"skip":   40,
	"drop":   85,
	"sweep":  bugSafe,
	"chorus": 110,
	"putt":   80,
	"jar":    300,
	"lash":   250,
	"swarm":  1300,
	"whirl":  3000,
}

// gameRefs reads the references the hourly sweep kept.
func gameRefs(q querier) (map[string]int, error) {
	out := map[string]int{}
	for k, f := range refFloor {
		out[k] = f
	}
	rows, err := q.Query(`SELECT game, best, ponds FROM refs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var game string
		var best, ponds int
		if err := rows.Scan(&game, &best, &ponds); err != nil {
			return nil, err
		}
		if ponds >= refPonds {
			out[game] = max(out[game], best)
		}
	}
	return out, rows.Err()
}

// keepRefs works out each game's median best over the ponds that have played
// it refRuns times, for the hourly sweep.
func keepRefs(tx *sql.Tx) error {
	for _, g := range powerGames() {
		query := `SELECT best FROM arcade WHERE game = ? AND plays >= ? AND best > 0 ORDER BY best`
		args := []any{g.key, refRuns}
		if g.key == "dive" {
			query = `SELECT dive_best FROM ponds WHERE dive_plays >= ? AND dive_best > 0 ORDER BY dive_best`
			args = args[1:]
		}
		rows, err := tx.Query(query, args...)
		if err != nil {
			return err
		}
		var bests []int
		for rows.Next() {
			var b int
			if err := rows.Scan(&b); err != nil {
				rows.Close()
				return err
			}
			bests = append(bests, b)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		mid := 0
		if n := len(bests); n > 0 {
			mid = (bests[(n-1)/2] + bests[n/2]) / 2
		}
		if _, err := tx.Exec(`INSERT INTO refs (game, best, ponds) VALUES (?, ?, ?)
			ON CONFLICT (game) DO UPDATE SET best = excluded.best, ponds = excluded.ponds`, g.key, mid, len(bests)); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------- notices

type powerRise struct {
	From int    `json:"from"`
	To   int    `json:"to"`
	Line string `json:"line"`
	// What is the line without the number, for a page that adds rises up.
	What string `json:"what"`
	// Rank is the rung's name when this rise reached a new one.
	Rank string `json:"rank,omitempty"`
}

// What each part says when it's the one that went up.
var riseWords = map[string]string{
	"hats": "a new hat", "trinkets": "a new trinket", "hoods": "a hood", "sites": "a site hat",
	"rocks": "a new rock", "keepsakes": "a keepsake", "mementos": "a memento", "fish": "a new fish",
	"looks": "a new look in the reeds", "cookbook": "a new dish", "furniture": "something new from Tansy",
	"sets": "a finished set", "badges": "a badge", "frogs": "a new frog", "rarity": "a rarer frog",
	"bond": "a closer bond", "jobs": "a level up at work", "mastery": "mastery at work", "front": "what's out front",
}

func riseWord(key string) string {
	if g, ok := strings.CutPrefix(key, "game_"); ok {
		for _, x := range powerGames() {
			if x.key == g {
				return "a best at " + x.name
			}
		}
	}
	return riseWords[key]
}

// seenPower is what a pond last saw of its power, kept as JSON in ponds.power_seen.
type seenPower struct {
	Total int            `json:"t"`
	Parts map[string]int `json:"p"`
}

// risen is what went up from seen to now, or nil when the total didn't rise.
// A part that fell, like a game whose reference went up, is left out.
func risen(seen seenPower, p power) *powerRise {
	if p.Total <= seen.Total {
		return nil
	}
	best, n := "", 0
	for _, x := range p.Parts {
		if up := x.Pts - seen.Parts[x.Key]; up > 0 {
			n++
			if best == "" || up > x.Pts-seen.Parts[best] {
				best = x.Key
			}
		}
	}
	r := &powerRise{From: seen.Total, To: p.Total}
	if best != "" {
		r.What = riseWord(best)
		if n > 1 {
			r.What += " and more"
		}
		r.Line = "+" + commas(p.Total-seen.Total) + ", " + r.What
	}
	return r
}

// notePower keeps what the pond has now seen of its power, and sends what
// went up since. A new rung reached is kept with the day, for the diary.
func notePower(tx *sql.Tx, v *view, p *pond) error {
	var raw string
	var top int
	if err := tx.QueryRow(`SELECT power_seen, rank_top FROM ponds WHERE id = ?`, p.ID).Scan(&raw, &top); err != nil {
		return err
	}
	pw := &v.Share.Power
	var seen seenPower
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &seen); err != nil {
			return err
		}
		pw.Rise = risen(seen, *pw)
	}
	day := ""
	if pw.at > top {
		if pw.Rise != nil {
			pw.Rise.Rank = ranks[pw.at].name
		}
		top, day = pw.at, p.today
	}
	pw.climb(top)
	if raw != "" && pw.Total == seen.Total && day == "" {
		return nil
	}
	seen = seenPower{pw.Total, map[string]int{}}
	for _, x := range pw.Parts {
		seen.Parts[x.Key] = x.Pts
	}
	b, err := json.Marshal(seen)
	if err != nil {
		return err
	}
	if day != "" {
		_, err = tx.Exec(`UPDATE ponds SET power_seen = ?, rank_top = ?, rank_day = ? WHERE id = ?`, string(b), top, day, p.ID)
		return err
	}
	_, err = tx.Exec(`UPDATE ponds SET power_seen = ? WHERE id = ?`, string(b), p.ID)
	return err
}

// rankTop is the highest rung a pond has reached.
func rankTop(q querier, id int64) (int, error) {
	var top int
	err := q.QueryRow(`SELECT rank_top FROM ponds WHERE id = ?`, id).Scan(&top)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errNoPond
	}
	return top, err
}

// ---------------------------------------------------------------- old ranks

// seedRanks gives every pond the rung its power had reached on the ladder
// before power counted everything, so respreading the ladder takes no rank
// away. It runs once, when rank_top is added.
func seedRanks(db *sql.DB, now time.Time) error {
	old := []struct {
		at   int
		name string
	}{{2000, "POND REGULAR"}, {5000, "LILY LORD"}, {9000, "MARSH BARON"}, {14000, "FEN DUKE"}, {21000, "BOG MONARCH"}, {30000, "SWAMP LEGEND"}}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ids, err := tx.Query(`SELECT id FROM ponds`)
	if err != nil {
		return err
	}
	var all []int64
	for ids.Next() {
		var id int64
		if err := ids.Scan(&id); err != nil {
			ids.Close()
			return err
		}
		all = append(all, id)
	}
	if err := ids.Close(); err != nil {
		return err
	}
	for _, id := range all {
		total, err := oldPower(tx, id, now)
		if err != nil {
			return err
		}
		top := 0
		for _, k := range old {
			if total >= k.at {
				top = rungNamed(k.name)
			}
		}
		if top == 0 {
			continue
		}
		if _, err := tx.Exec(`UPDATE ponds SET rank_top = ? WHERE id = ?`, top, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// oldPower is a pond's power as it was counted before, hats and the rest by
// tier, the frog's rarity, jobs, Dive and bond.
func oldPower(q querier, id int64, now time.Time) (int, error) {
	p, err := loadPond(q, id)
	if err != nil {
		return 0, err
	}
	held, err := heldHats(q, id)
	if err != nil {
		return 0, err
	}
	xp, err := loadXP(q, id)
	if err != nil {
		return 0, err
	}
	been, err := sitesSeen(q, id)
	if err != nil {
		return 0, err
	}
	had, err := everHad(q, id)
	if err != nil {
		return 0, err
	}
	b, err := loadBond(q, id)
	if err != nil {
		return 0, err
	}
	hatPower := [tierCount]int{20, 50, 120, 300, 800}
	total := int(math.Round(120 * math.Log2(float64(frogOf(p, now).Rarity.N))))
	for _, h := range append(append(held, hoodsEarned(xp)...), been...) {
		total += hatPower[h.tier()]
	}
	for _, t := range trinkets {
		if had[t.kind] {
			total += hatPower[t.tier] / 2
		}
	}
	for _, k := range rocks {
		if had[k.kind] {
			total += hatPower[k.tier]
		}
	}
	for _, k := range keepsakes {
		if had[k.kind] {
			total += hatPower[k.tier]
		}
	}
	for _, f := range fishKinds {
		if had[fishHad+f.kind] {
			total += hatPower[f.tier]
		}
	}
	for _, m := range mementos {
		if had[m.kind] {
			total += hatPower[mementoTier] / 2
		}
	}
	total += (max(bestLevels(xp), len(jobs)) - len(jobs)) * 3
	total += min(p.DiveBest, 60) * 25
	total += min(b.points/bondHeart, bondHearts) * 60
	return total, nil
}

type partGroup struct {
	Name  string
	Parts []powerPart
}

// powerGroups is the breakdown as the share page lists it.
func powerGroups(parts []powerPart) []partGroup {
	out := []partGroup{{"Finds", nil}, {"Frogs and work", nil}, {"Games", nil}}
	at := map[string]int{"finds": 0, "frogs": 1, "games": 2}
	for _, x := range parts {
		out[at[x.Group]].Parts = append(out[at[x.Group]].Parts, x)
	}
	return out
}
