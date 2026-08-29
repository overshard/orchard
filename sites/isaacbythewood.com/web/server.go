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

// SetupLogging installs the process-wide structured logger.
//
// JSON, because these run in containers and the logs are read by machine
// before they are read by a person. The one concession to being read by a
// person is that source positions are off: they are noise in a request log
// and the message plus the attributes already say where a record came from.
//
// Called by every site's main before anything else logs, so the four copies of
// this package cannot drift into four different log formats.
func SetupLogging() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		// Timestamps in UTC, which is what the stdlib logger was doing with
		// log.LUTC before this and is not slog's default. Four containers, a
		// host on Eastern time and a reader correlating them against
		// Cloudflare's logs all want one zone, and local time in a container
		// is a trap: it silently differs from the host's.
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
//
// Every container in this workspace listens on 8000, in dev and in prod alike,
// so addr is almost always ":8000". Graceful shutdown matters more than it
// looks: `docker compose up --build` on every deploy means these processes are
// killed and restarted constantly, and a half-written response is a visible
// error to whoever was mid-request.
func Serve(addr string, h http.Handler) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
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
// answered 200. It exists so the container can check itself.
//
// Two of these images are FROM scratch: no shell, no curl, no wget, nothing a
// HEALTHCHECK could shell out to. The binary is the only executable in the
// image, so it has to be the thing that does the probing, which is the usual
// answer for scratch and distroless images. The other two images are Alpine and
// could use wget, but doing it the same way everywhere means one behaviour to
// reason about rather than two.
//
// The timeout is deliberately short. A health check that hangs is worse than
// one that fails: Docker treats a timed out check as a failure anyway, and a
// slow check just delays finding out.
func HealthCheck(url string, timeout time.Duration) error {
	client := &http.Client{Timeout: timeout}

	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	// Drained and closed, not just closed. An undrained body leaks the
	// connection out of the pool, which does not matter for a process that is
	// about to exit but is the kind of thing that gets copied somewhere it
	// does matter.
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	return nil
}
