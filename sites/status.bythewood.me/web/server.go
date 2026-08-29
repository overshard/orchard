package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// SetupLogging installs the process-wide structured logger. JSON, because these
// logs are read by machine before they are read by a person. Source positions
// are off: they are noise in a request log, and the message and attributes
// already say where a record came from.
func SetupLogging() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		// UTC, which is not slog's default. Local time in a container
		// silently differs from the host's.
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey && len(groups) == 0 {
				a.Value = slog.TimeValue(a.Value.Time().UTC())
			}
			return a
		},
	})))
}

// Serve runs h on addr until SIGINT or SIGTERM, then drains in-flight requests
// before returning. Every deploy is a `docker compose up --build`, so these
// processes are killed and restarted often and a half-written response is
// something somebody sees.
func Serve(addr string, h http.Handler) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		// A whole-request read bound, so a slow body cannot hold a
		// connection open indefinitely.
		ReadTimeout: 30 * time.Second,
		// And a write bound. Without one, a handler that takes minutes just
		// hangs: the client waits, the connection is held, and nothing in the
		// process ever notices. Generous enough for a Typst report, which has
		// its own 30 second ceiling.
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	errs := make(chan error, 1)
	go func() {
		slog.Info("listening", slog.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
	}()

	select {
	case err := <-errs:
		return err
	case sig := <-stop:
		slog.Info("shutting down", slog.String("signal", sig.String()))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

// HealthCheck probes a running server over the loopback and reports whether it
// answered 200, so that a container can check itself. Two of these images are
// FROM scratch: no shell, no curl, nothing a HEALTHCHECK can call except the
// binary. The Alpine images could use wget, but one behaviour everywhere beats
// two.
//
// The timeout is short. Docker treats a timed out check as a failure anyway, so
// a slow check only delays finding out.
func HealthCheck(url string, timeout time.Duration) error {
	client := &http.Client{Timeout: timeout}

	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	// Drained as well as closed: an undrained body leaks the connection out
	// of the pool.
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	return nil
}
