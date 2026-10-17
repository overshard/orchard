package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"logging.bythewood.me/ntfy"
)

const ntfyTopic = "logging"

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

	switch kind {
	case "silence":
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
		slog.Info(fmt.Sprintf("alert: unknown kind %q for %s", kind, ctx.Source),
			slog.String("component", "alerts"))
		return
	}
	if err := n.client.Publish(context.Background(), body); err != nil {
		slog.Error(fmt.Sprintf("alert: publishing %s for %s failed: %v", kind, ctx.Source, err),
			slog.String("component", "alerts"))
		return
	}
	slog.Info(fmt.Sprintf("alert: published %s for %s", kind, ctx.Source),
		slog.String("component", "alerts"))
}

func previewAlert(kind string) error {
	body, ok := renderAlert(kind, AlertContext{
		Source: "blog",
		Silent: 7 * time.Minute,
		Detail: "Previous record seen 12 minutes ago.",
	})
	if !ok {
		return fmt.Errorf("unknown alert kind %q (use 'silence', 'resumed' or 'restart')", kind)
	}
	ntfy.Preview(os.Stdout, ntfyTopic, body)
	return nil
}
