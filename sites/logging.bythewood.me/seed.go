package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"time"
)

// Seeding: realistic fake records, so the dashboard can be looked at and judged
// before five real sites have accumulated a fortnight of traffic.
//
// A logging site is the worst of all of them to build against an empty
// database, because every panel on it is a distribution and a distribution of
// nothing is a blank rectangle. Every layout decision here was made against
// this data.
//
// It writes through the same commit path the ingest endpoint uses, rollups
// included, rather than a second insert that could drift from it. That is the
// point of doing it this way: seeded data and real data are indistinguishable
// downstream, so a panel that looks right on seeded data is not lying about the
// query it will run in production.

type seedSource struct {
	name      string
	weight    int
	paths     []string
	component []string
	// slow is the multiplier on the base latency, so the site that shells out
	// to typst reads as slower than the one serving a static page, the way
	// the real ones do.
	slow float64
}

var seedSources = []seedSource{
	{
		name: "isaacbythewood", weight: 35, slow: 0.6,
		paths: []string{"/", "/about", "/code", "/art", "/contact", "/static/assets/base-aDClsiBF.js", "/robots.txt"},
	},
	{
		name: "blog", weight: 25, slow: 0.9,
		paths: []string{"/", "/feed.atom", "/latest.json", "/posts/go-instead-of-rust", "/posts/one-repo-many-sites", "/posts/a-tunnel-and-a-desktop"},
	},
	{
		name: "analytics", weight: 20, slow: 2.4,
		paths:     []string{"/collect", "/", "/login", "/properties", "/static/collector.js"},
		component: []string{"geoip"},
	},
	{
		name: "status", weight: 15, slow: 1.8,
		paths:     []string{"/", "/login", "/properties", "/healthz"},
		component: []string{"scheduler", "crawler", "lighthouse", "notifier"},
	},
	{
		name: "logging", weight: 5, slow: 0.8,
		paths:     []string{"/", "/overview", "/search", "/sources/blog"},
		component: []string{"retention"},
	},
}

var seedComponentMessages = map[string][]string{
	"scheduler":  {"cycle complete", "property due", "watchdog reset a wedged job"},
	"crawler":    {"crawl finished", "crawl started", "page fetch failed"},
	"lighthouse": {"audit complete", "audit skipped, already running"},
	"notifier":   {"alert published", "alert suppressed, still in cooldown"},
	"geoip":      {"geoip database refreshed", "geoip refresh skipped"},
	"retention":  {"retention sweep"},
}

// runSeed generates records spread over the last `days` days and commits them.
func runSeed(ctx context.Context, db *sql.DB, count, days int) error {
	if count <= 0 || days <= 0 {
		return fmt.Errorf("seed needs a positive count and day span, got %d over %d", count, days)
	}

	w := &Writer{db: db}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	totalWeight := 0
	for _, s := range seedSources {
		totalWeight += s.weight
	}

	end := time.Now().UTC()
	start := end.AddDate(0, 0, -days)
	span := end.Sub(start)

	batch := make([]row, 0, writeBatch)
	written := 0

	for i := 0; i < count; i++ {
		src := seedSources[0]
		pick := rng.Intn(totalWeight)
		for _, s := range seedSources {
			if pick < s.weight {
				src = s
				break
			}
			pick -= s.weight
		}

		batch = append(batch, seedRow(rng, src, start, span))

		if len(batch) >= writeBatch {
			if err := w.commit(batch); err != nil {
				return err
			}
			written += len(batch)
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		if err := w.commit(batch); err != nil {
			return err
		}
		written += len(batch)
	}

	slog.Info("seeded",
		slog.String("component", "seed"),
		slog.Int("records", written),
		slog.Int("days", days),
		slog.Int("sources", len(seedSources)))
	return nil
}

func seedRow(rng *rand.Rand, src seedSource, start time.Time, span time.Duration) row {
	at := seedTimestamp(rng, start, span)

	// One in twelve records is a subsystem message rather than a request, for
	// the sources that have subsystems. That ratio is roughly what the real
	// scheduler produces against the real request log.
	if len(src.component) > 0 && rng.Intn(12) == 0 {
		component := src.component[rng.Intn(len(src.component))]
		msgs := seedComponentMessages[component]
		msg := "worked"
		if len(msgs) > 0 {
			msg = msgs[rng.Intn(len(msgs))]
		}
		level := "INFO"
		switch {
		case rng.Intn(40) == 0:
			level = "ERROR"
		case rng.Intn(15) == 0:
			level = "WARN"
		}
		return row{
			source:    src.name,
			ts:        at.UnixMilli(),
			level:     level,
			msg:       msg,
			component: component,
			attrs:     seedAttrs(map[string]any{"duration_s": rng.Intn(600)}),
		}
	}

	path := src.paths[rng.Intn(len(src.paths))]
	status := seedStatus(rng)
	level := "INFO"
	if status >= 500 {
		level = "ERROR"
	}

	// Lognormal, so most requests are quick and a long tail is genuinely
	// long. A normal distribution here would make every percentile land on
	// top of the mean and every latency panel look pointless.
	ms := math.Exp(rng.NormFloat64()*0.9+0.2) * src.slow
	if status >= 500 {
		ms *= 4
	}

	cfRay := ""
	// One request in sixty arrives without a CF-Ray, which is the signal the
	// hardening pass instrumented: something reached the origin without
	// crossing the tunnel.
	if rng.Intn(60) != 0 {
		cfRay = fmt.Sprintf("%012x-%s", rng.Int63n(1<<48), "IAD")
	}

	return row{
		source:     src.name,
		ts:         at.UnixMilli(),
		level:      level,
		msg:        "request",
		method:     seedMethod(rng, path),
		path:       path,
		host:       src.name + ".bythewood.me",
		status:     status,
		durationMS: math.Round(ms*1000) / 1000,
		ip:         fmt.Sprintf("%d.%d.%d.%d", 20+rng.Intn(200), rng.Intn(256), rng.Intn(256), 1+rng.Intn(254)),
		cfRay:      cfRay,
		attrs:      seedAttrs(map[string]any{"bytes": 400 + rng.Intn(60000)}),
	}
}

// seedTimestamp puts a record somewhere in the window, with a daily rhythm.
//
// Uniform noise would give every hour the same height and make the volume chart
// a flat bar, which is exactly the thing a volume chart exists to disprove.
func seedTimestamp(rng *rand.Rand, start time.Time, span time.Duration) time.Time {
	for {
		at := start.Add(time.Duration(rng.Int63n(int64(span))))
		// A cosine over the day, peaking mid-afternoon UTC, accepted by
		// rejection sampling. Cheap, and it needs no inverse.
		hour := float64(at.Hour()) + float64(at.Minute())/60
		p := 0.35 + 0.65*(0.5+0.5*math.Cos((hour-15)/24*2*math.Pi))
		if rng.Float64() < p {
			return at
		}
	}
}

func seedMethod(rng *rand.Rand, path string) string {
	switch path {
	case "/collect", "/login":
		if rng.Intn(3) > 0 {
			return "POST"
		}
	}
	return "GET"
}

// seedStatus weights the codes the way a small site actually sees them: mostly
// 200, a steady trickle of 404 from scanners, and 500 rare enough that finding
// one is worth a click.
func seedStatus(rng *rand.Rand) int64 {
	switch n := rng.Intn(1000); {
	case n < 860:
		return 200
	case n < 900:
		return 304
	case n < 930:
		return 303
	case n < 985:
		return 404
	case n < 993:
		return 429
	case n < 998:
		return 500
	default:
		return 502
	}
}

func seedAttrs(m map[string]any) string {
	buf, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(buf)
}
