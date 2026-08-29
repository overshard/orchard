package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"
)

// TestProbeTiming is a diagnostic rather than an assertion, and is skipped
// unless asked for, since it reaches the live internet.
//
// It runs the real phasedHop against a target so its numbers can be compared
// with an independent tool measuring the same phases, which is the only way to
// separate "the prober is wrong" from "the network is different".
//
//	STATUS_PROBE_BENCH=https://blog.bythewood.me go test -run TestProbeTiming -v
func TestProbeTiming(t *testing.T) {
	target := os.Getenv("STATUS_PROBE_BENCH")
	if target == "" {
		t.Skip("set STATUS_PROBE_BENCH=<url> to run this diagnostic")
	}

	u, err := parseHTTPURL(target)
	if err != nil {
		t.Fatal(err)
	}

	n := 15
	var dns, tcp, tls, ttfb, total []int64
	for i := 0; i < n; i++ {
		hop, err := phasedHop(context.Background(), u)
		if err != nil {
			t.Fatalf("probe %d: %v", i, err)
		}
		dns = append(dns, *hop.timings.DNSMS)
		tcp = append(tcp, *hop.timings.TCPMS)
		tls = append(tls, *hop.timings.TLSMS)
		ttfb = append(ttfb, *hop.timings.TTFBMS)
		total = append(total, hop.timings.TotalMS)
		time.Sleep(time.Second)
	}

	med := func(v []int64) int64 {
		s := append([]int64(nil), v...)
		sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
		return s[len(s)/2]
	}
	fmt.Printf("phasedHop against %s, %d samples\n", target, n)
	for _, p := range []struct {
		name string
		v    []int64
	}{{"dns", dns}, {"tcp", tcp}, {"tls", tls}, {"ttfb", ttfb}, {"total", total}} {
		fmt.Printf("  %-6s median %4dms\n", p.name, med(p.v))
	}
}
