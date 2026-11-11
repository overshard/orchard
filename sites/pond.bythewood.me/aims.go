package main

import (
	"database/sql"
	"slices"
	"time"
)

// An aim is one of a disk's two goals for the day, met by a run whose counts
// reach need with every count in none at nought, and a score of at least score.
type aim struct {
	key, game, line string
	need            map[string]int
	none            []string
	score           int
}

const (
	aimsADay    = 2
	aimPebbles  = 5
	aimSlack    = 5
	aimCountMax = 10000
)

var aimList = []aim{
	{"dv_clean", "dive", "Clear ten gates without a bug", map[string]int{"gates": 10}, []string{"bugs"}, 0},
	{"dv_bugs", "dive", "Eat six bugs in one dive", map[string]int{"bugs": 6}, nil, 0},
	{"dv_deep", "dive", "Clear twenty gates", map[string]int{"gates": 20}, nil, 0},
	{"dv_win", "dive", "Finish a dive in first place", map[string]int{"first": 1}, nil, 0},

	{"fi_five", "fish", "Land five fish in a round", map[string]int{"landed": 5}, nil, 0},
	{"fi_kinds", "fish", "Land three kinds of fish", map[string]int{"kinds": 3}, nil, 0},
	{"fi_perfect", "fish", "Strike three fish perfectly", map[string]int{"perfect": 3}, nil, 0},
	{"fi_cool", "fish", "Land three without the line going red", map[string]int{"cool": 3}, nil, 0},

	{"fl_fire", "flick", "Catch three fireflies", map[string]int{"fireflies": 3}, nil, 0},
	{"fl_row", "flick", "Catch ten bugs in a row", map[string]int{"row": 10}, nil, 0},
	{"fl_wasp", "flick", "Score 30 and never touch a wasp", nil, []string{"wasps"}, 30},
	{"fl_gold", "flick", "Catch a golden fly", map[string]int{"golden": 1}, nil, 0},

	{"cr_trips", "cross", "Get home three times", map[string]int{"trips": 3}, nil, 0},
	{"cr_peb", "cross", "Pick up two pebbles", map[string]int{"pebbles": 2}, nil, 0},
	{"cr_fly", "cross", "Eat a fly at the pond", map[string]int{"flies": 1}, nil, 0},
	{"cr_clean", "cross", "Get home twice without losing a life", map[string]int{"clean": 2}, nil, 0},

	{"ra_weirs", "raft", "Thread ten reed weirs", map[string]int{"weirs": 10}, nil, 0},
	{"ra_peb", "raft", "Pick up five pebbles", map[string]int{"pebbles": 5}, nil, 0},
	{"ra_clean", "raft", "Score 20 before the first bump", map[string]int{"clean": 20}, nil, 0},
	{"ra_whirl", "raft", "Spin round two whirlpools", map[string]int{"whirls": 2}, nil, 0},

	{"le_high", "leap", "Leap up past 100 M", map[string]int{"top": 100}, nil, 0},
	{"le_bugs", "leap", "Eat four bugs on the way up", map[string]int{"bugs": 4}, nil, 0},
	{"le_fly", "leap", "Get a lift off two dragonflies", map[string]int{"flies": 2}, nil, 10},

	{"sc_fleas", "school", "Eat twenty fleas", map[string]int{"fleas": 20}, nil, 0},
	{"sc_gold", "school", "Eat two golden fleas", map[string]int{"golds": 2}, nil, 0},
	{"sc_long", "school", "Grow the school to fifteen", map[string]int{"long": 15}, nil, 0},

	{"st_pads", "stack", "Stack twenty pads", map[string]int{"pads": 20}, nil, 0},
	{"st_row", "stack", "A perfect stack of five in a row", map[string]int{"row": 5}, nil, 0},
	{"st_perf", "stack", "Place ten perfect pads", map[string]int{"perfect": 10}, nil, 0},

	{"sh_four", "shell", "Find the tadpole four times", map[string]int{"won": 4}, nil, 0},
	{"sh_six", "shell", "Find the tadpole six times", map[string]int{"won": 6}, nil, 0},

	{"sk_eight", "skip", "Skip one pebble eight times", map[string]int{"best": 8}, nil, 0},
	{"sk_pads", "skip", "Skip off two lily pads", map[string]int{"pads": 2}, nil, 0},
	{"sk_far", "skip", "Throw one past 50 M", map[string]int{"far": 50}, nil, 5},

	{"dr_two", "drop", "Two pebbles in the middle pot", map[string]int{"mid": 2}, nil, 0},
	{"dr_three", "drop", "Three in the middle pot", map[string]int{"mid": 3}, nil, 0},
	{"dr_every", "drop", "A pebble in every pot", map[string]int{"pots": 5}, nil, 0},

	{"ch_right", "chorus", "Sing twenty right on the beat", map[string]int{"right": 20}, nil, 0},
	{"ch_row", "chorus", "Sing thirty in a row", map[string]int{"row": 30}, nil, 0},

	{"pu_nine", "putt", "Sink all nine holes", map[string]int{"holes": 9}, nil, 0},
	{"pu_under", "putt", "Beat par on two holes", map[string]int{"under": 2}, nil, 0},
	{"pu_three", "putt", "Three strokes under par for the round", map[string]int{"beat": 3}, nil, 0},

	{"ja_elder", "jar", "An elder in the jar", map[string]int{"elders": 1}, nil, 0},
	{"ja_king", "jar", "A pond king in the jar", map[string]int{"kings": 1}, nil, 0},
	{"ja_frogs", "jar", "Grow three frogs", map[string]int{"frogs": 3}, nil, 0},
	{"ja_froglets", "jar", "Grow eight froglets", map[string]int{"froglets": 8}, nil, 0},

	{"sw_clear", "sweep", "Clear a whole grid", map[string]int{"cleared": 1}, nil, 0},
	{"sw_forty", "sweep", "Open forty pads in one go", map[string]int{"pads": 40}, nil, 0},
}

// The least score each count says a run made, so a page can't report more of
// a thing than its score allows. A count missing here only has to be sane.
var aimWorth = map[string]map[string]int{
	"dive":   {"gates": 1, "bugs": 1},
	"fish":   {"perfect": 2, "cool": 1},
	"flick":  {"fireflies": 3, "row": 1, "golden": 8},
	"cross":  {"trips": 10, "pebbles": 3, "flies": 5, "clean": 10},
	"raft":   {"weirs": 1, "pebbles": 2, "clean": 1, "whirls": 3},
	"leap":   {"top": 1, "bugs": 2},
	"school": {"fleas": 1, "golds": 3, "long": 1},
	"stack":  {"pads": 1, "row": 2, "perfect": 2},
	"shell":  {"won": 1},
	"skip":   {"best": 1, "pads": 3},
	"drop":   {"mid": 8, "pots": 1},
	"chorus": {"right": 3, "row": 2},
	"putt":   {"holes": 2, "under": 12, "beat": 1},
	"jar":    {"kings": 60, "elders": 25, "frogs": 8, "froglets": 4},
}

// aimsOf is a game's two goals for a day, the same in every pond.
func aimsOf(day, game string) []aim {
	var all []aim
	for _, a := range aimList {
		if a.game == game {
			all = append(all, a)
		}
	}
	if len(all) <= aimsADay {
		return all
	}
	h := dayHash(day, "aims "+game)
	first := int(h % uint32(len(all)))
	second := (first + 1 + int(h>>8)%(len(all)-1)) % len(all)
	return []aim{all[first], all[second]}
}

// countsFit holds a run's counts loosely to its score.
func countsFit(game string, score int, counts map[string]int) bool {
	for k, n := range counts {
		if n < 0 || n > aimCountMax || n*aimWorth[game][k] > score+aimSlack {
			return false
		}
	}
	return true
}

func (a aim) met(score int, counts map[string]int) bool {
	if score < a.score {
		return false
	}
	for k, n := range a.need {
		if counts[k] < n {
			return false
		}
	}
	for _, k := range a.none {
		if n, ok := counts[k]; !ok || n != 0 {
			return false
		}
	}
	return true
}

func aimsMet(q querier, id int64, day string) ([]string, error) {
	rows, err := q.Query(`SELECT aim FROM aims WHERE pond_id = ? AND day = ?`, id, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// meetAims keeps each of the day's goals a run met for the first time, and
// says what they're worth. The caller pays it inside the game's cap.
func meetAims(tx *sql.Tx, id int64, game string, score int, counts map[string]int, now time.Time) ([]string, int, error) {
	if !countsFit(game, score, counts) {
		return nil, 0, nil
	}
	day := dayOf(now)
	had, err := aimsMet(tx, id, day)
	if err != nil {
		return nil, 0, err
	}
	var lines []string
	for _, a := range aimsOf(day, game) {
		if slices.Contains(had, a.key) || !a.met(score, counts) {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO aims (pond_id, day, aim) VALUES (?, ?, ?)`, id, day, a.key); err != nil {
			return nil, 0, err
		}
		lines = append(lines, a.line)
	}
	return lines, aimPebbles * len(lines), nil
}

type aimView struct {
	Line string `json:"line"`
	Done bool   `json:"done"`
}

func aimViews(had []string, day, game string) []aimView {
	var out []aimView
	for _, a := range aimsOf(day, game) {
		out = append(out, aimView{a.line, slices.Contains(had, a.key)})
	}
	return out
}

// fishCounts takes what a round landed off the catch the server kept, never
// off the page's word.
func fishCounts(counts map[string]int, catch []caught) map[string]int {
	out := map[string]int{}
	for k, n := range counts {
		out[k] = n
	}
	out["landed"], out["kinds"] = 0, len(catch)
	for _, c := range catch {
		out["landed"] += c.N
	}
	return out
}
