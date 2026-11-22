package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"slices"
	"strings"
	"sync"
	"time"
)

// Each site's /healthz is fetched by its public URL and timed from the request
// being written to the first byte back, so a sample is the trip through
// Cloudflare and the tunnel without a TLS handshake that changes with idle time.
const (
	probeInterval = 5 * time.Minute
	probeTimeout  = 5 * time.Second
	probeKeep     = 24
)

type sample struct {
	ms int
	ok bool
}

// ProbeCache keeps the last two hours of samples per site and is safe for
// concurrent use.
type ProbeCache struct {
	mu      sync.RWMutex
	samples map[string][]sample
	client  *http.Client
}

func NewProbeCache() *ProbeCache {
	return &ProbeCache{
		samples: make(map[string][]sample),
		client:  &http.Client{Timeout: probeTimeout},
	}
}

func (p *ProbeCache) Start(ctx context.Context, targets []Site) {
	go func() {
		p.round(ctx, targets)
		ticker := time.NewTicker(probeInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.round(ctx, targets)
			}
		}
	}()
}

func (p *ProbeCache) round(ctx context.Context, targets []Site) {
	var wg sync.WaitGroup
	got := make([]sample, len(targets))
	for i, t := range targets {
		if t.Visit == "" {
			continue
		}
		wg.Go(func() { got[i] = p.probe(ctx, t.Visit+"/healthz") })
	}
	wg.Wait()

	p.mu.Lock()
	defer p.mu.Unlock()
	for i, t := range targets {
		if t.Visit == "" {
			continue
		}
		list := append(p.samples[t.Name], got[i])
		if len(list) > probeKeep {
			list = list[len(list)-probeKeep:]
		}
		p.samples[t.Name] = list
	}
}

func (p *ProbeCache) probe(ctx context.Context, url string) sample {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return sample{}
	}
	req.Header.Set("User-Agent", "isaacbythewood.com probe")
	var wrote, first time.Time
	req = req.WithContext(httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		WroteRequest:         func(httptrace.WroteRequestInfo) { wrote = time.Now() },
		GotFirstResponseByte: func() { first = time.Now() },
	}))
	resp, err := p.client.Do(req)
	if err != nil {
		return sample{}
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return sample{ms: int(first.Sub(wrote).Milliseconds()), ok: resp.StatusCode == http.StatusOK}
}

// Health is what a tile shows. Known is false until a site has been probed,
// so a fresh container doesn't draw every dot red.
type Health struct {
	Known  bool
	Up     bool
	Median int
	Spark  string
}

func (p *ProbeCache) Health(name string) Health {
	p.mu.RLock()
	list := slices.Clone(p.samples[name])
	p.mu.RUnlock()
	return healthOf(list)
}

// Edge is up when anything came back through the tunnel, and its line is the
// median of every site per round.
func (p *ProbeCache) Edge() Health {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var rounds []sample
	for i := 0; i < probeKeep; i++ {
		var ms []int
		for _, list := range p.samples {
			j := len(list) - probeKeep + i
			if j >= 0 && list[j].ok {
				ms = append(ms, list[j].ms)
			}
		}
		if len(ms) > 0 {
			rounds = append(rounds, sample{ms: median(ms), ok: true})
		}
	}
	return healthOf(rounds)
}

func healthOf(list []sample) Health {
	if len(list) == 0 {
		return Health{}
	}
	var ms []int
	for _, s := range list {
		if s.ok {
			ms = append(ms, s.ms)
		}
	}
	h := Health{Known: true, Up: list[len(list)-1].ok}
	if len(ms) > 0 {
		h.Median = median(ms)
	}
	if len(ms) > 1 {
		h.Spark = sparkPath(ms)
	}
	return h
}

func median(ms []int) int {
	s := slices.Clone(ms)
	slices.Sort(s)
	return s[len(s)/2]
}

// sparkPath draws onto a 100 by 20 viewBox, scaled to its own min and max so a
// flat line still shows its wobble, with a 2 unit margin for the stroke.
func sparkPath(ms []int) string {
	lo, hi := slices.Min(ms), slices.Max(ms)
	span := float64(hi - lo)
	if span == 0 {
		span = 1
	}
	var b strings.Builder
	step := 100.0 / float64(len(ms)-1)
	for i, v := range ms {
		y := 18 - (float64(v-lo)/span)*16
		cmd := "L"
		if i == 0 {
			cmd = "M"
		}
		fmt.Fprintf(&b, "%s%.1f %.1f ", cmd, float64(i)*step, y)
	}
	return strings.TrimSpace(b.String())
}

// State is the dot's class on a tile and a node.
func (h Health) State() string {
	switch {
	case !h.Known:
		return "unknown"
	case h.Up:
		return "up"
	default:
		return "down"
	}
}
