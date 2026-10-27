package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestSourceSchedule(t *testing.T) {
	want := []struct {
		name   string
		every  time.Duration
		idle   time.Duration
		primed bool
	}{
		{"market", 30 * time.Second, 5 * time.Minute, true},
		{"onair", 10 * time.Minute, 0, false},
		{"broadcasts", time.Hour, 0, false},
		{"news", 5 * time.Minute, 0, false},
		{"signal", time.Hour, 0, true},
		{"board", 5 * time.Minute, 0, false},
		{"earnings", 6 * time.Hour, 0, false},
		{"reports", 15 * time.Minute, 0, false},
		{"alerts", 10 * time.Minute, 0, false},
		{"air", 30 * time.Minute, 0, false},
		{"pollen", 3 * time.Hour, 0, false},
		{"outdoors", 3 * time.Hour, 0, false},
		{"steam", time.Hour, 0, false},
		{"streaming", 3 * time.Hour, 0, false},
		{"weather", 15 * time.Minute, 0, false},
		{"systems", time.Minute, 0, false},
		{"calendar", 12 * time.Hour, 0, false},
	}
	if len(sources) != len(want) {
		t.Fatalf("%d sources, want %d", len(sources), len(want))
	}
	for i, w := range want {
		src := sources[i]
		if src.Name != w.name || src.Every != w.every || src.Idle != w.idle || src.Primed != w.primed {
			t.Errorf("source %d is %s every %s idle %s primed %t, want %s every %s idle %s primed %t",
				i, src.Name, src.Every, src.Idle, src.Primed, w.name, w.every, w.idle, w.primed)
		}
		if src.Run == nil {
			t.Errorf("%s has no Run", src.Name)
		}
	}
}

func TestSourceInterval(t *testing.T) {
	market := sources[0]
	if got := market.interval(true); got != 30*time.Second {
		t.Errorf("watched market every %s", got)
	}
	if got := market.interval(false); got != 5*time.Minute {
		t.Errorf("idle market every %s", got)
	}
	news := sources[3]
	if news.interval(true) != news.Every || news.interval(false) != news.Every {
		t.Errorf("news should not slow down when idle")
	}
}

// runLoop starts src and returns how many times it has run so far.
func runLoop(t *testing.T, s *Store, src Source) func() int64 {
	var runs atomic.Int64
	src.Run = func(*Store, context.Context, *Guard) { runs.Add(1) }
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go s.loop(ctx, nil, src)
	return runs.Load
}

func TestLoopRunsAtOnceUnlessPrimed(t *testing.T) {
	fresh := runLoop(t, NewStore(NewHub()), Source{Every: time.Hour})
	primed := runLoop(t, NewStore(NewHub()), Source{Every: time.Hour, Primed: true})
	time.Sleep(50 * time.Millisecond)
	if fresh() != 1 {
		t.Errorf("fresh source ran %d times, want 1", fresh())
	}
	if primed() != 0 {
		t.Errorf("primed source ran %d times, want 0", primed())
	}
}

func TestLoopWakesForTheFirstViewer(t *testing.T) {
	hub := NewHub()
	runs := runLoop(t, NewStore(hub), Source{Every: 20 * time.Millisecond, Idle: time.Hour, Primed: true})
	time.Sleep(50 * time.Millisecond)
	if runs() != 0 {
		t.Fatalf("idle source ran %d times before anyone looked", runs())
	}
	_, done := hub.Subscribe()
	defer done()
	deadline := time.Now().Add(time.Second)
	for runs() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if runs() == 0 {
		t.Error("a viewer arriving did not wake the idle source")
	}
}

func TestLoopSkipsAWakeRightAfterARun(t *testing.T) {
	hub := NewHub()
	runs := runLoop(t, NewStore(hub), Source{Every: time.Hour, Idle: time.Hour})
	time.Sleep(20 * time.Millisecond)
	_, done := hub.Subscribe()
	defer done()
	time.Sleep(50 * time.Millisecond)
	if runs() != 1 {
		t.Errorf("source ran %d times, want only its first run", runs())
	}
}
