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

// The health strip. The page is public, so a row carries a name, up or down, a
// latency and an error count, and never a path, a message, a status code or an
// address. logging hands over only that, and both ends hold the limit.

// probeEvery is slower than the markets poll, since a site that stayed up isn't
// news.
const probeEvery = 60 * time.Second

// aggregateURL is a container name on the orchard-edge bridge, never the public
// hostname. Caddy refuses this path from outside.
const aggregateURL = "http://orchard-logging:8000/aggregate"

// Monitored is one row. Source is the label it ships its logs under and, for a
// site, the first hostname label, so it is also half the container name.
type Monitored struct {
	Label  string
	Source string

	// Host is the public hostname, empty for something that has none. Without
	// one there's no fallback probe, so it reads unknown instead of down
	// wherever the bridge isn't reachable.
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
	{Label: "Auth", Source: "auth", Host: "auth.bythewood.me"},
	{Label: "Status", Source: "status", Host: "status.bythewood.me"},
	{Label: "Logging", Source: "logging", Host: "logging.bythewood.me"},
	{Label: "Repos", Source: "repos", Host: "repos.bythewood.me"},
	{Label: "Chat", Source: "chat", Host: "chat.bythewood.me"},
	{Label: "LLM", Source: "llm", Host: "llm.bythewood.me"},
	{Label: "Dash", Source: "dash", Host: "dash.bythewood.me"},

	// Caddy is the only part of the edge on the strip, since it ships its
	// access log and so has an error count to show. cloudflared and ntfy only
	// log to stdout.
	{Label: "Caddy", Source: "caddy", Bridge: "http://orchard-caddy:80/", AnyStatus: true},
}

type SystemRow struct {
	Label     string `json:"label"`
	Host      string `json:"host"`
	URL       string `json:"url"`
	State     string `json:"state"`
	Errors    int64  `json:"errors"`
	KnowError bool   `json:"know_error"`

	// How long this site takes to answer at the slow end of a day, read off its
	// own access log instead of timed here, since a probe over the Docker
	// bridge only times the bridge.
	Response string `json:"response"`

	// Traffic over the same window as Errors, and where it sits against this
	// site's own preceding week, since a dead day for a news site is a good one
	// for a blog.
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

// probeClient is separate from the shared client because a probe that reuses a
// pooled socket to a container that just died reports it up.
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

// probeResult separates the three things a probe attempt can mean. The probes
// start at once and the guard paces all but one, so a paced probe read as
// unreachable would call a serving site down.
type probeResult int

const (
	probeAnswered probeResult = iota
	// The name doesn't resolve, so this is the wrong route and not a failure,
	// and the caller tries the next one.
	probeNoRoute
	// The guard said no, so nothing went out and nothing was learned.
	probeSkipped
)

// probe asks the container on the bridge first and the public hostname only as
// a fallback, since Cloudflare serves a cached 200 for /healthz long after the
// origin stops. A row the fallback answered says cached instead of up.
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
			// Nothing was measured, so the row stays unknown instead of calling
			// a site down on the strength of a paced request.
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

// humanMS keeps a response time to six characters across four orders of
// magnitude. Under a millisecond is still printed instead of "<1ms", or half
// the strip reads the same when the sites are ten times apart.
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
// here, so the bars compare like with like instead of against some idea of
// busy.
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
