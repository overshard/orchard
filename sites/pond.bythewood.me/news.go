package main

import (
	"math"
	"time"
)

// A line of work that had a big day makes the next morning's paper, and its
// share follows the story through the morning. The paper and the price both
// read the same full day of flow, so the story always comes first.
const (
	newsHigh = 1.5
	newsLow  = 0.6
	newsBump = 0.03
	// The bump comes in over the first hours of the day.
	newsHours = 6
)

// What the paper says when a business's day before was big, and when it was
// dead. A business with no lines here never makes the news.
var newsLines = map[string][2]string{
	"guild":     {"Bug catchers report a record haul", "Bug catchers come home with empty jars"},
	"reedcut":   {"Reed cutters can't keep up with the reeds", "Reed cutters down tools for the day"},
	"claypit":   {"The clay pits hit a rich seam", "Nobody dug anything at the clay pits"},
	"gatherers": {"Stick gatherers bring home a forest", "A slow day for the stick gatherers"},
	"divers":    {"Pebble divers come up with their pockets full", "Pebble divers find the bottom bare"},
	"fishery":   {"The fishery has its best day in ages", "The fishery's nets come up empty"},
	"gardens":   {"The gardeners can't pick fast enough", "The gardeners spend the day weeding"},
	"dow":       {"Every shop in town sold out by lunch", "The shops sat quiet all day"},
	"nasdaq":    {"Ribbit Labs says everybody's playing", "Ribbit Labs says nobody played much"},
	"russell":   {"Tadpole Ventures counts a crowd of new eggs", "Tadpole Ventures has a quiet nursery"},
	"gold":      {"Golden Fly Mines turns up a glittering haul", "Golden Fly Mines finds nothing shiny"},
	"oil":       {"Bog Oil & Peat says every pot in town is on", "Kitchens go cold and Bog Oil & Peat sulks"},
	"vix":       {"Storm Shelter Supply can't make things fast enough", "The craft benches stand empty"},
	"tackle":    {"Sedge sells out of bait and won't say why he's smiling", "Sedge sold no bait and says he prefers it"},
	"frames":    {"Marsh's Frames has a queue out the door", "Marsh waits all day for somebody to want a frame"},
}

type news struct {
	Key  string `json:"key"`
	Up   bool   `json:"up"`
	Line string `json:"line"`
}

// newsOn is the story in the paper on day, off the day before's flow.
func (b flowBook) newsOn(day string) *news {
	before := dayShift(day, -1)
	var best *news
	most := 0.0
	for _, a := range assets {
		lines, ok := newsLines[a.Key]
		if a.Kind != kindShare || a.flow == "" || !ok {
			continue
		}
		r := b.ratio(a.flow, before, time.Time{}, true)
		if r < newsHigh && r > newsLow {
			continue
		}
		if m := math.Abs(math.Log(r)); m > most {
			best, most = &news{Key: a.Key, Up: r >= newsHigh}, m
			best.Line = lines[0]
			if !best.Up {
				best.Line = lines[1]
			}
		}
	}
	return best
}

// newsMove is the log move a story adds to a share by a moment of its day.
func newsMove(n *news, key string, frac float64) float64 {
	if n == nil || n.Key != key {
		return 0
	}
	in := min(1, frac*24/newsHours)
	if n.Up {
		return newsBump * in
	}
	return -newsBump * in
}

// newsFor is today's story for the view, or nothing.
func newsFor(q querier, day string) (*news, error) {
	book, err := loadFlows(q, day)
	if err != nil {
		return nil, err
	}
	return book.newsOn(day), nil
}
