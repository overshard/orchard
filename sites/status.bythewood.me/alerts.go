package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"status.bythewood.me/ntfy"
)

const ntfyTopic = "status"

// AlertContext is the property snapshot as of the moment the alert fired, so the
// numbers in the notification are the ones that triggered it.
type AlertContext struct {
	ID            string
	Name          string
	URL           string
	CurrentStatus int64
	AvgResponseMS int64
}

// Notifier publishes transitions. It holds a client, unlike the prober, since it
// talks to one host and is not measuring the handshake.
type Notifier struct {
	client  *ntfy.Client
	sending sync.WaitGroup
}

func NewNotifier() *Notifier {
	token := os.Getenv("NTFY_TOKEN")
	if token == "" {
		// Not fatal, since refusing to start over a missing alert credential
		// would turn a missed notification into an outage.
		slog.Warn("NTFY_TOKEN is unset; alerts will be rendered and refused, not delivered",
			slog.String("component", "alerts"))
	}
	return &Notifier{client: ntfy.New(ntfyTopic, token)}
}

// renderAlert builds the notification for a transition, false for an unknown
// kind.
func renderAlert(kind string, ctx AlertContext) (ntfy.Message, bool) {
	// Absolute, because it is opened from a phone with no idea of the origin.
	link := baseURL + "/" + ctx.ID

	switch kind {
	case "down":
		return ntfy.Message{
			Title: ctx.Name + " is down",
			Body: fmt.Sprintf(
				"%s\nTwo consecutive checks failed. Latest status: %d.\nRolling average response: %d ms.",
				ctx.URL, ctx.CurrentStatus, ctx.AvgResponseMS),
			// high since urgent bypasses do-not-disturb.
			Priority: "high",
			Tags:     "rotating_light",
			Link:     link,
			Label:    "Open status",
		}, true

	case "recovery":
		return ntfy.Message{
			Title: ctx.Name + " is back up",
			Body: fmt.Sprintf(
				"%s\nThe latest check returned %d.\nRolling average response: %d ms.",
				ctx.URL, ctx.CurrentStatus, ctx.AvgResponseMS),
			Priority: "default",
			Tags:     "white_check_mark",
			Link:     link,
			Label:    "Open status",
		}, true
	}
	return ntfy.Message{}, false
}

// Send fires in the background, so a slow ntfy does not hold up the caller.
func (n *Notifier) Send(kind string, ctx AlertContext) {
	n.sending.Go(func() { n.Fire(kind, ctx) })
}

// Wait returns once every alert handed to Send has gone out or failed.
func (n *Notifier) Wait() { n.sending.Wait() }

// Fire publishes one transition. Errors are logged and swallowed since it runs
// in a goroutine off the scheduler's path with nobody to return to.
func (n *Notifier) Fire(kind string, ctx AlertContext) {
	body, ok := renderAlert(kind, ctx)
	if !ok {
		slog.Info(fmt.Sprintf("alert: unknown kind %q for %s", kind, ctx.URL))
		return
	}
	if err := n.client.Publish(context.Background(), body); err != nil {
		slog.Error(fmt.Sprintf("alert: publishing %s for %s failed: %v", kind, ctx.URL, err))
		return
	}
	slog.Info(fmt.Sprintf("alert: published %s for %s", kind, ctx.URL))
}

// previewAlert prints what would be published, for checking the wording.
func previewAlert(kind string) error {
	body, ok := renderAlert(kind, AlertContext{
		ID:            "00000000-0000-0000-0000-000000000000",
		Name:          "example.com",
		URL:           "https://example.com",
		CurrentStatus: map[string]int64{"down": 503, "recovery": 200}[kind],
		AvgResponseMS: 184,
	})
	if !ok {
		return fmt.Errorf("unknown alert kind %q (use 'down' or 'recovery')", kind)
	}
	ntfy.Preview(os.Stdout, ntfyTopic, body)
	return nil
}
