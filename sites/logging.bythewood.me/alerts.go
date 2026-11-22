package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"logging.bythewood.me/ntfy"
)

const ntfyTopic = "alerts"

// AlertContext is the snapshot as of the moment the alert fired.
type AlertContext struct {
	Source string
	// Silent is how long the source had been quiet, zero where it means nothing.
	Silent time.Duration
	// Detail is one extra clause, already phrased, for kinds that have one.
	Detail string
}

type Notifier struct {
	client *ntfy.Client
}

func NewNotifier() *Notifier {
	token := os.Getenv("NTFY_TOKEN")
	if token == "" {
		// Not fatal, since refusing to start over a missing alert credential
		// would turn a quiet notification into an outage.
		slog.Warn("NTFY_TOKEN is unset; alerts will be rendered and refused, not delivered",
			slog.String("component", "alerts"))
	}
	return &Notifier{client: ntfy.New(ntfyTopic, token)}
}

// renderAlert builds the notification for a transition, reporting false for an
// unknown kind.
func renderAlert(kind string, ctx AlertContext) (ntfy.Message, bool) {
	// Absolute, since it's opened from a phone that has no idea what the origin is.
	link := baseURL + "/sources/" + ctx.Source

	_, heartbeat := heartbeats[ctx.Source]

	switch kind {
	case "silence":
		if heartbeat {
			return ntfy.Message{
				Title: ctx.Source + " has not checked in",
				Body: fmt.Sprintf(
					"No heartbeat for %s.\nThe job that sends it has failed, stopped running, or can't reach ingest.",
					humanDuration(ctx.Silent)),
				Priority: "high",
				Tags:     "mute",
				Link:     link,
				Label:    "Open logs",
			}, true
		}
		return ntfy.Message{
			Title: ctx.Source + " has stopped logging",
			Body: fmt.Sprintf(
				"No records for %s.\nThe container may be down, wedged, or unable to reach ingest. Its own stdout is still the source of truth: docker logs orchard-%s",
				humanDuration(ctx.Silent), ctx.Source),
			// Not urgent, which bypasses do-not-disturb.
			Priority: "high",
			Tags:     "mute",
			Link:     link,
			Label:    "Open logs",
		}, true

	case "resumed":
		if heartbeat {
			return ntfy.Message{
				Title:    ctx.Source + " checked in again",
				Body:     fmt.Sprintf("A heartbeat arrived after %s without one.", humanDuration(ctx.Silent)),
				Priority: "default",
				Tags:     "white_check_mark",
				Link:     link,
				Label:    "Open logs",
			}, true
		}
		return ntfy.Message{
			Title: ctx.Source + " is logging again",
			Body: fmt.Sprintf(
				"Records are arriving after %s of silence.",
				humanDuration(ctx.Silent)),
			Priority: "default",
			Tags:     "white_check_mark",
			Link:     link,
			Label:    "Open logs",
		}, true

	case "crash":
		body := "It stopped with no shutting-down record, which usually means the whole PC crashed or lost power and every site went down with it."
		if ctx.Silent > 0 {
			body += fmt.Sprintf("\nNothing was logged for %s.", humanDuration(ctx.Silent))
		}
		return ntfy.Message{
			Title:    ctx.Source + " came back after an unclean shutdown",
			Body:     body,
			Priority: "high",
			Tags:     "rotating_light",
			Link:     link,
			Label:    "Open logs",
		}, true

	case "errors":
		return ntfy.Message{
			Title:    ctx.Source + " is logging a burst of errors",
			Body:     ctx.Detail + "\nNot told again for this source for an hour.",
			Priority: "high",
			Tags:     "warning",
			Link:     link,
			Label:    "Open logs",
		}, true

	case "disk":
		return ntfy.Message{
			Title:    "The data disk is nearly full",
			Body:     ctx.Detail + "\nIt's the host filesystem Docker writes to, so every site with a database stops writing when it fills. Check docker system df and build cache first.",
			Priority: "high",
			Tags:     "floppy_disk",
			Link:     link,
			Label:    "Open logs",
		}, true

	case "restart":
		return ntfy.Message{
			Title: ctx.Source + " restarted without a clean shutdown",
			Body: fmt.Sprintf(
				"It started listening with no shutting-down record before it, so the previous process did not exit on a signal.\n%s",
				ctx.Detail),
			// A crash that repeats also trips the silence rule, which is loud.
			Priority: "default",
			Tags:     "warning",
			Link:     link,
			Label:    "Open logs",
		}, true
	}
	return ntfy.Message{}, false
}

func humanDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d seconds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	default:
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if m == 0 {
			return fmt.Sprintf("%d hours", h)
		}
		return fmt.Sprintf("%dh%dm", h, m)
	}
}

// Fire publishes one transition. Errors are logged and swallowed since it runs
// off the watchdog's timer with nobody to return to.
func (n *Notifier) Fire(kind string, ctx AlertContext) {
	body, ok := renderAlert(kind, ctx)
	if !ok {
		slog.Info("alert: unknown kind", slog.Any("kind", kind), slog.Any("source", ctx.Source),
			slog.String("component", "alerts"))
		return
	}
	if err := n.client.Deliver(context.Background(), body); err != nil {
		slog.Error("alert: publishing failed", slog.Any("kind", kind), slog.Any("source", ctx.Source), slog.Any("err", err),
			slog.String("component", "alerts"))
		return
	}
	slog.Info("alert: published", slog.Any("kind", kind), slog.Any("source", ctx.Source),
		slog.String("component", "alerts"))
}

func previewAlert(kind string) error {
	body, ok := renderAlert(kind, AlertContext{
		Source: "blog",
		Silent: 7 * time.Minute,
		Detail: "Previous record seen 12 minutes ago.",
	})
	if !ok {
		return fmt.Errorf("unknown alert kind %q (use 'silence', 'resumed', 'restart', 'crash', 'errors' or 'disk')", kind)
	}
	ntfy.Preview(os.Stdout, ntfyTopic, body)
	return nil
}
