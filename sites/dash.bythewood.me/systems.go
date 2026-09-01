package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// The health strip. This is a public page, so a row carries a name, an up or
// down, a latency and a count of errors, and never a path, a message, a status
// code or an address. What logging hands over is already limited to that, and
// the limit lives at both ends so neither has to remember for the other.
//
// This is not a second status.bythewood.me. There are no phase timings, no
// history and no alerting here, because status already does all of that
// properly and a strip of dots is what a dashboard wants.

// probeEvery is the strip's cadence. Slower than the markets panel because a
// site that has been up for eight hours is not news, and the seven probes are
// against Isaac's own machine rather than someone else's endpoint.
const probeEvery = 60 * time.Second

// aggregateURL is a container name on the orchard-edge bridge, never the public
// hostname. Caddy refuses this path from outside.
const aggregateURL = "http://orchard-logging:8000/aggregate"

// Monitored is one row. Source is the label it ships its logs under and, for a
// site, the first hostname label, so it is also half the container name.
type Monitored struct {
	Label  string
	Source string

	// Host is the public hostname, and is empty for something that has none.
	// Without one there is no fallback probe, so it reads unknown rather than
	// down wherever the bridge is not reachable.
	Host string

	// Bridge overrides the usual <container>:8000/healthz.
	Bridge string

	// AnyStatus means any HTTP response at all proves the process is serving.
	// Caddy has no health endpoint and answers its catch-all with a 404, which
	// is still Caddy answering.
	AnyStatus bool
}

var monitored = []Monitored{
	{Label: "Portfolio", Source: "isaacbythewood", Host: "isaacbythewood.com"},
	{Label: "Blog", Source: "blog", Host: "blog.bythewood.me"},
	{Label: "Analytics", Source: "analytics", Host: "analytics.bythewood.me"},
	{Label: "Status", Source: "status", Host: "status.bythewood.me"},
	{Label: "Logging", Source: "logging", Host: "logging.bythewood.me"},
	{Label: "Repos", Source: "repos", Host: "repos.bythewood.me"},
	{Label: "Dash", Source: "dash", Host: "dash.bythewood.me"},

	// The edge itself, and the only part of it on this strip: Caddy is the one
	// that ships its access log, so it is the one with an error count to show
	// beside the others. cloudflared and ntfy log to stdout and cannot write to
	// a socket, so neither has anything here to be counted.
	{Label: "Caddy", Source: "caddy", Bridge: "http://orchard-caddy:80/", AnyStatus: true},
}

type SystemRow struct {
	Label     string `json:"label"`
	Host      string `json:"host"`
	URL       string `json:"url"`
	State     string `json:"state"`
	Errors    int64  `json:"errors"`
	KnowError bool   `json:"know_error"`

	// How long this site takes to answer at the slow end of a day, read off
	// its own access log rather than measured here. The probe runs over the
	// Docker bridge, so timing it reported the bridge: every site came back
	// between one and two milliseconds whatever it was actually doing.
	Response string `json:"response"`

	// Traffic over the same window as Errors, plus where it sits against this
	// site's own preceding week. Busy is relative here or it is meaningless:
	// a day that would be dead for a news site is a good day for a personal
	// blog, so the comparison is always against the site itself.
	Requests int64  `json:"requests"`
	KnowTraf bool   `json:"know_traf"`
	Level    int    `json:"level"`
	Trend    string `json:"trend"`
}

type Systems struct {
	Rows     []SystemRow `json:"rows"`
	Up       int         `json:"up"`
	Total    int         `json:"total"`
	Errors   int64       `json:"errors"`
	Requests int64       `json:"requests"`
	Window   int         `json:"window"`
	Checked  string      `json:"checked"`
}

// probeClient is separate from the shared JSON client because these are health
// probes and a pooled connection would report a warm socket rather than a
// reachable site. Nothing here is trying to be a real uptime measurement, that
// is what status is for, but a probe that reuses a socket to a container that
// died two seconds ago is wrong rather than approximate.
var probeClient = &http.Client{
	Timeout: 5 * time.Second,
	Transport: &http.Transport{
		DisableKeepAlives: true,
	},
	// A health check that follows a redirect is checking somewhere else.
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// probeResult separates the three things a probe attempt can mean. Collapsing
// them was a real bug: seven goroutines start at once, the guard paced six of
// them, and a paced probe read as an unreachable site, so the strip reported
// six sites down while every one of them was serving.
type probeResult int

const (
	probeAnswered probeResult = iota
	// The name does not resolve, so this is the wrong route rather than a
	// failure and the caller should try the next one.
	probeNoRoute
	// The guard said no, so nothing went out and nothing was learned.
	probeSkipped
)

// probe answers whether one site is serving, and how fast.
//
// The container on the bridge is asked first and the public hostname is only a
// fallback, because Cloudflare will happily serve a cached 200 for /healthz
// long after the origin behind it has stopped answering. Two of these sites
// return CF-Cache-Status: HIT for that path right now, so a public probe is not
// evidence the site is up. When the fallback is what answered, the row says
// cached rather than up and means it.
func probe(ctx context.Context, g *Guard, m Monitored) SystemRow {
	row := SystemRow{Label: m.Label, Host: m.Host, State: "unknown"}
	if m.Host != "" {
		row.URL = visitURL(m.Host)
	}

	bridge := m.Bridge
	switch {
	case bridge != "":
	case m.Source == selfSource:
		// Asking the bridge for our own name works in the container and not in
		// a dev run, and loopback is the same answer in both.
		bridge = "http://127.0.0.1:8000/healthz"
	default:
		bridge = "http://orchard-" + m.Source + ":8000/healthz"
	}

	attempts := []struct {
		url    string
		public bool
	}{{bridge, false}}
	if m.Host != "" {
		attempts = append(attempts, struct {
			url    string
			public bool
		}{"https://" + m.Host + "/healthz", true})
	}

	for _, attempt := range attempts {
		state, result := hit(ctx, g, attempt.url, attempt.public, m.AnyStatus)
		switch result {
		case probeAnswered:
			row.State = state
			return row
		case probeSkipped:
			// Nothing was measured, so the row stays unknown rather than
			// claiming a site is down on the strength of a paced request.
			return row
		}
	}

	return row
}

// hit runs one request. public marks a request that went over the internet, so
// an edge cache hit can be reported as what it is.
func hit(ctx context.Context, g *Guard, url string, public, anyStatus bool) (state string, result probeResult) {
	if err := g.Allow("uptime"); err != nil {
		return "", probeSkipped
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", probeSkipped
	}
	req.Header.Set("User-Agent", "dash.bythewood.me health strip")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := probeClient.Do(req)
	if err != nil {
		var dns *net.DNSError
		if errors.As(err, &dns) {
			return "", probeNoRoute
		}
		g.Fail("uptime", 0, 0)
		return "down", probeAnswered
	}
	defer resp.Body.Close()

	g.Succeed("uptime")

	state = "down"
	if anyStatus || resp.StatusCode == http.StatusOK {
		state = "up"
		if public && cacheHit(resp.Header.Get("CF-Cache-Status")) {
			state = "cached"
		}
	}
	return state, probeAnswered
}

// humanMS keeps a response time to six characters. These sites answer an
// embedded page in a fraction of a millisecond and repos answers a git pack in a
// fifth of a second, so the column has to carry four orders of magnitude without
// going ragged. Anything under a millisecond is still printed rather than
// rounded to "<1ms", or half the strip reads as the same number when the sites
// are a factor of ten apart.
func humanMS(ms float64) string {
	switch {
	case ms <= 0:
		return ""
	case ms >= 1000:
		return fmt.Sprintf("%.1fs", ms/1000)
	case ms >= 10:
		return fmt.Sprintf("%.0fms", ms)
	case ms >= 1:
		return fmt.Sprintf("%.1fms", ms)
	default:
		return fmt.Sprintf("%.2fms", ms)
	}
}

func cacheHit(status string) bool {
	switch strings.ToUpper(status) {
	case "HIT", "STALE", "UPDATING", "REVALIDATED":
		return true
	}
	return false
}

type aggregatePayload struct {
	WindowHours int `json:"window_hours"`
	Sources     []struct {
		Source        string  `json:"source"`
		Errors        int64   `json:"errors"`
		Requests      int64   `json:"requests"`
		BaselineDaily float64 `json:"baseline_daily"`
		P95MS         float64 `json:"p95_ms"`
	} `json:"sources"`
}

// buildSystems probes every site at once and folds in the error counts logging
// keeps. A logging site that is down costs the strip its error column and
// nothing else.
func buildSystems(ctx context.Context, g *Guard, now time.Time) Systems {
	rows := make([]SystemRow, len(monitored))

	var wg sync.WaitGroup
	for i, m := range monitored {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rows[i] = probe(ctx, g, m)
		}()
	}
	wg.Wait()

	s := Systems{Rows: rows, Total: len(rows), Checked: now.UTC().Format("15:04:05")}

	var payload aggregatePayload
	if err := getJSON(ctx, g, "logging", aggregateURL, &payload); err == nil {
		type counts struct {
			errors, requests int64
			baseline, p95    float64
		}
		by := map[string]counts{}
		var busiest int64
		for _, src := range payload.Sources {
			by[src.Source] = counts{src.Errors, src.Requests, src.BaselineDaily, src.P95MS}
			busiest = max(busiest, src.Requests)
		}

		s.Window = payload.WindowHours
		for i, m := range monitored {
			c, ok := by[m.Source]
			if !ok {
				continue
			}
			rows[i].Errors, rows[i].KnowError = c.errors, true
			rows[i].Requests, rows[i].KnowTraf = c.requests, true
			rows[i].Level = trafficLevel(c.requests, busiest)
			rows[i].Trend = trafficTrend(c.requests, c.baseline)
			rows[i].Response = humanMS(c.p95)
			s.Errors += c.errors
			s.Requests += c.requests
		}
	}

	for _, r := range rows {
		if r.State == "up" || r.State == "cached" {
			s.Up++
		}
	}
	return s
}

// visitURL is what the strip links to. The campaign tag is so the sites can see
// in their own analytics that a visit came from here, which is the only way to
// tell dash traffic apart from anything else arriving at a bare hostname.
func visitURL(host string) string {
	return "https://" + host + "/?utm_source=dash.bythewood.me&utm_medium=referral&utm_campaign=systems"
}

// trafficLevel puts a site's day on a four step scale against the busiest site
// here, so the bars compare like with like on one machine rather than against
// some idea of what a busy site is.
func trafficLevel(requests, busiest int64) int {
	if requests <= 0 || busiest <= 0 {
		return 0
	}
	switch share := float64(requests) / float64(busiest); {
	case share >= 0.6:
		return 4
	case share >= 0.3:
		return 3
	case share >= 0.1:
		return 2
	default:
		return 1
	}
}

// trafficTrend compares the day against this site's own preceding week. The
// band is wide because a personal site's daily traffic is noisy enough that
// anything tighter would call every day unusual.
func trafficTrend(requests int64, baselineDaily float64) string {
	if baselineDaily <= 0 {
		return ""
	}
	switch ratio := float64(requests) / baselineDaily; {
	case ratio >= 1.5:
		return "up"
	case ratio <= 0.5:
		return "down"
	default:
		return "flat"
	}
}
