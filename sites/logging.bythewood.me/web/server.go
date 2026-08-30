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
// logs are read by machine before they are read by a person.
func SetupLogging() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		// UTC is not slog's default, and local time in a container
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
// before returning.
func Serve(addr string, h http.Handler) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		// Whole-request bounds, so a slow body or a wedged handler cannot
		// hold a connection open forever. The write bound is generous enough
		// for a Typst report, which has its own 30 second ceiling.
		ReadTimeout:  30 * time.Second,
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
// answered 200, so a container can check itself. Two of these images are FROM
// scratch, with no shell and no curl for HEALTHCHECK to call.
func HealthCheck(url string, timeout time.Duration) error {
	client := &http.Client{Timeout: timeout}

	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	// Drained as well as closed, or the connection leaks out of the pool.
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	return nil
}
