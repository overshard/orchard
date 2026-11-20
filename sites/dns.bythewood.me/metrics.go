package main

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// unboundCtl is unbound's control port on the inner network. It takes plain
// text with no certificate, since only this app can reach it.
const unboundCtl = "orchard-dns-unbound:8953"

// counts is lookups by status and protocol since the app started.
type counts struct {
	mu sync.Mutex
	n  map[[2]string]int64
}

func (c *counts) add(status, proto string) {
	c.mu.Lock()
	if c.n == nil {
		c.n = map[[2]string]int64{}
	}
	c.n[[2]string{status, proto}]++
	c.mu.Unlock()
}

func (c *counts) snapshot() map[[2]string]int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[[2]string]int64, len(c.n))
	for k, v := range c.n {
		out[k] = v
	}
	return out
}

// unboundStats asks unbound for its counters since it started.
func unboundStats(addr string) (map[string]float64, error) {
	c, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := c.Write([]byte("UBCT1 stats_noreset\n")); err != nil {
		return nil, err
	}
	out := map[string]float64{}
	sc := bufio.NewScanner(c)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			out[k] = f
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("unbound sent no stats")
	}
	return out, sc.Err()
}

// cacheShare is how much of what unbound was asked came out of its cache.
func cacheShare(st map[string]float64) (float64, bool) {
	q := st["total.num.queries"]
	if q == 0 {
		return 0, false
	}
	return st["total.num.cachehits"] / q, true
}

// metrics is the app's counters and unbound's in Prometheus' text format, on
// the bridge only.
func (s *site) metrics(w http.ResponseWriter, r *http.Request) {
	if !fromFront(r) {
		plainNoStore(w, http.StatusForbidden, "not from the front")
		return
	}
	var b strings.Builder
	b.WriteString("# TYPE dns_lookups_total counter\n")
	snap := s.res.counts.snapshot()
	keys := make([][2]string, 0, len(snap))
	for k := range snap {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i][0]+keys[i][1] < keys[j][0]+keys[j][1] })
	for _, k := range keys {
		fmt.Fprintf(&b, "dns_lookups_total{status=%q,proto=%q} %d\n", k[0], k[1], snap[k])
	}
	fmt.Fprintf(&b, "# TYPE dns_fallback_total counter\ndns_fallback_total %d\n", s.res.fellBack.Load())
	fmt.Fprintf(&b, "# TYPE dns_not_logged_total counter\ndns_not_logged_total %d\n", s.log.dropped.Load())
	if tab := s.lists.tab.Load(); tab != nil {
		fmt.Fprintf(&b, "# TYPE dns_listed gauge\ndns_listed %d\n", len(tab.off))
	}
	if st, err := unboundStats(unboundCtl); err == nil {
		names := make([]string, 0, len(st))
		for k := range st {
			if strings.HasPrefix(k, "total.") || strings.HasPrefix(k, "num.answer.rcode.") || strings.HasPrefix(k, "mem.cache.") {
				names = append(names, k)
			}
		}
		sort.Strings(names)
		for _, k := range names {
			fmt.Fprintf(&b, "unbound_%s %g\n", strings.NewReplacer(".", "_", "-", "_").Replace(k), st[k])
		}
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(b.String()))
}
