package main

import (
	"database/sql"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"
)

// Frogs in the reeds forage once a day, and what each brings back follows its
// nature. The frog out front is busy greeting, so it doesn't.
type foraged struct {
	Frog  string `json:"frog"`
	Prize prize  `json:"prize"`
}

type foragedAll struct {
	Finds []foraged `json:"finds"`
	Pond  *view     `json:"pond"`
}

func forager(stage string) bool { return stage == "froglet" || stage == "frog" || stage == "elder" }

func rollForage(nature string, now time.Time, rain bool) prize {
	switch nature {
	case "BOLD":
		n := 6 + rand.IntN(7)
		return prize{Pebbles: n, Text: fmt.Sprintf("%d pebbles from under a stone it shouldn't have lifted.", n)}
	case "SHY":
		k := "agate"
		switch r := rand.IntN(10); {
		case r == 0:
			k = "calcite"
		case r < 4:
			k = "quartz"
		}
		return prize{Item: k, Text: itemNames[k] + ", found somewhere quiet."}
	case "NIGHT OWL":
		k := "cricket"
		switch r := rand.IntN(3); {
		case r == 0:
			k = "firefly"
		case r == 1:
			k = "mayfly"
		}
		return prize{Item: k, Text: "A " + strings.ToLower(thingName(k)) + " it caught after dark."}
	case "GLUTTON":
		k := ingredients[rollCrop(now, rain)].kind
		return prize{Item: k, Text: thingName(k) + ", and it only ate a little of it."}
	case "SINGER":
		n := 4 + rand.IntN(5)
		if rain {
			n *= 2
		}
		return prize{Pebbles: n, Text: fmt.Sprintf("%d pebbles thrown its way for singing.", n)}
	}
	if rand.IntN(4) == 0 {
		t := trinketOfTier(0)
		return prize{Item: t.kind, Text: t.name + ", which floated by."}
	}
	k := []string{"snail", "cap", "feather"}[rand.IntN(3)]
	return prize{Item: k, Text: itemNames[k] + ", which floated by."}
}

// foragersReady counts the frogs in the reeds old enough to forage, and
// is 0 once they have today.
func foragersReady(frogs []frogRow, p *pond, now time.Time) int {
	if p.ForageDay == dayOf(now) {
		return 0
	}
	n := 0
	for _, f := range frogs {
		if stage, _ := stageOf(now.Sub(time.Unix(f.HatchedAt, 0))); !f.Front && forager(stage) {
			n++
		}
	}
	return n
}

func (s *store) forage(id int64) (*foragedAll, error) {
	out := &foragedAll{Finds: []foraged{}}
	v, err := s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		frogs, err := loadFrogs(tx, p.ID)
		if err != nil {
			return err
		}
		if foragersReady(frogs, p, now) == 0 {
			return errNotYet
		}
		rain := dayOfReading(s.day.reading(now), now).Rain
		for _, f := range frogs {
			if stage, _ := stageOf(now.Sub(time.Unix(f.HatchedAt, 0))); f.Front || !forager(stage) {
				continue
			}
			g := genomeOf(f.Seed, f.Mods)
			z := rollForage(g.Nature, now, rain)
			if err := award(tx, p, &z); err != nil {
				return err
			}
			out.Finds = append(out.Finds, foraged{titled(frogViewOf(f, now).Name), z})
		}
		p.ForageDay = dayOf(now)
		return nil
	})
	out.Pond = v
	return out, err
}
