package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// The desktop on its own sits around 10 to 40% and 1.6GB of the 3070 with a
// video playing, and a game takes most of both. 3000MiB is also about where
// Ornith's 7.3GB stops fitting beside it.
const (
	busyUtil = 55
	busyMiB  = 3000
)

type gpuReading struct {
	UtilAvg  int `json:"util_avg"`
	UtilMax  int `json:"util_max"`
	UsedMiB  int `json:"used_mib"`
	TotalMiB int `json:"total_mib"`
}

type gpuAnswer struct {
	Busy   bool   `json:"busy"`
	Reason string `json:"reason"`
	Loaded bool   `json:"loaded"`
	gpuReading
}

// judge decides from a reading taken with none of our models resident, since
// with one on the card the numbers are our own and say nothing about the desktop.
func judge(r gpuReading) (bool, string) {
	switch {
	case r.UtilAvg >= busyUtil:
		return true, fmt.Sprintf("card at %d%%", r.UtilAvg)
	case r.UsedMiB >= busyMiB:
		return true, fmt.Sprintf("%d MiB of %d in use", r.UsedMiB, r.TotalMiB)
	}
	return false, "card idle"
}

// gpu tells a background caller whether loading a model now would land on top
// of whatever the desktop is doing. It loads nothing and is not logged.
func (s *site) gpu(w http.ResponseWriter, r *http.Request, _ Key) {
	var running struct {
		Running []swapModel `json:"running"`
	}
	if err := s.swap(r.Context(), swapRunning, &running); err != nil {
		http.Error(w, "the model server is not answering", http.StatusBadGateway)
		return
	}
	var out gpuAnswer
	for _, m := range running.Running {
		out.Loaded = out.Loaded || m.State != "stopping"
	}

	if out.Loaded {
		out.Reason = "a model is already on the card"
	} else {
		reading, err := s.probeGPU(r.Context())
		if err != nil {
			slog.Error("probing the card", "err", err)
			http.Error(w, "the gpu probe is not answering", http.StatusBadGateway)
			return
		}
		out.gpuReading = reading
		out.Busy, out.Reason = judge(reading)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *site) probeGPU(ctx context.Context) (gpuReading, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var r gpuReading
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.gpuProbe+"/gpu", nil)
	if err != nil {
		return r, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return r, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return r, fmt.Errorf("the gpu probe answered %d", resp.StatusCode)
	}
	return r, json.NewDecoder(io.LimitReader(resp.Body, 4<<10)).Decode(&r)
}
