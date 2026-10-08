// Command gpuprobe answers how busy the card is, for callers that would rather
// not load a model onto it while the desktop is using it. It runs beside the
// model server with the driver's utility capability and nothing else, since
// the gateway is a scratch image and cannot run nvidia-smi itself.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const addr = ":8092"

// Utilisation is an instant reading and a game's frame pacing swings it, so
// one sample can land in a gap. Several over a couple of seconds can't.
const (
	samples = 5
	gap     = 400 * time.Millisecond
)

type reading struct {
	UtilAvg  int `json:"util_avg"`
	UtilMax  int `json:"util_max"`
	UsedMiB  int `json:"used_mib"`
	TotalMiB int `json:"total_mib"`
}

func sample(ctx context.Context) (util, used, total int, err error) {
	out, err := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=utilization.gpu,memory.used,memory.total", "--format=csv,noheader,nounits").Output()
	if err != nil {
		return 0, 0, 0, err
	}
	f := strings.Split(strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0]), ",")
	if len(f) != 3 {
		return 0, 0, 0, fmt.Errorf("unexpected nvidia-smi output %q", out)
	}
	n := make([]int, 3)
	for i, v := range f {
		if n[i], err = strconv.Atoi(strings.TrimSpace(v)); err != nil {
			return 0, 0, 0, fmt.Errorf("unexpected nvidia-smi output %q", out)
		}
	}
	return n[0], n[1], n[2], nil
}

func probe(ctx context.Context) (reading, error) {
	var r reading
	sum := 0
	for i := 0; i < samples; i++ {
		if i > 0 {
			time.Sleep(gap)
		}
		util, used, total, err := sample(ctx)
		if err != nil {
			return r, err
		}
		sum += util
		r.UtilMax = max(r.UtilMax, util)
		r.UsedMiB = max(r.UsedMiB, used)
		r.TotalMiB = total
	}
	r.UtilAvg = sum / samples
	return r, nil
}

func main() {
	healthcheck := flag.Bool("healthcheck", false, "probe a running server on this host and exit")
	flag.Parse()

	if *healthcheck {
		c := &http.Client{Timeout: 3 * time.Second}
		resp, err := c.Get("http://127.0.0.1" + addr + "/healthz")
		if err != nil || resp.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /gpu", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		got, err := probe(ctx)
		if err != nil {
			slog.Error("nvidia-smi failed", "err", err)
			http.Error(w, "nvidia-smi failed", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(got)
	})
	// Says the process is up and nothing about the card, so the healthcheck
	// does not run nvidia-smi every thirty seconds.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	slog.Info("gpuprobe serving " + addr)
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("gpuprobe stopped", "err", err)
		os.Exit(1)
	}
}
