package main

import (
	"bufio"
	"net"
	"path/filepath"
	"testing"
)

// unbound's control protocol on its socket is one UBCT1 line in and the stats
// as key=value lines out, closed by unbound when it's done.
func TestUnboundStatsOverSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unbound.ctl")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	asked := make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		line, _ := bufio.NewReader(c).ReadString('\n')
		asked <- line
		c.Write([]byte("total.num.queries=40\ntotal.num.cachehits=30\nnot a stat\n"))
	}()

	st, err := unboundStats(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := <-asked; got != "UBCT1 stats_noreset\n" {
		t.Errorf("sent %q, want the stats_noreset command", got)
	}
	share, ok := cacheShare(st)
	if !ok || share != 0.75 {
		t.Errorf("cache share = %v, %v, want 0.75", share, ok)
	}
}

func TestUnboundStatsNoSocket(t *testing.T) {
	if _, err := unboundStats(filepath.Join(t.TempDir(), "missing.ctl")); err == nil {
		t.Error("no error with nothing listening")
	}
}
