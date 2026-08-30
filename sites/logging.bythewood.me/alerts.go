package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// The container name on the orchard-edge bridge, never the public hostname:
// Caddy refuses every publish route on ntfy.bythewood.me, so publishing is
// reachable only from the bridge even holding the write token.
const (
	ntfyURL   = "http://orchard-ntfy:8000"
	ntfyTopic = "logging"
)

const ntfyTimeout = 5 * time.Second

// AlertContext is the snapshot as of the moment the alert fired.
type AlertContext struct {
	Source string
	// Silent is how long the source had been quiet, zero where it means nothing.
	Silent time.Duration
	// Detail is one extra clause, already phrased, for kinds that have one.
	Detail string
}

type Notifier struct {
	client *http.Client
	base   string
	topic  string
	token  string
}

func NewNotifier() *Notifier {
	token := os.Getenv("NTFY_TOKEN")
	if token == "" {
		// Not fatal: refusing to start over a missing alert credential would
		// turn a quiet notification into an outage.
		slog.Warn("NTFY_TOKEN is unset; alerts will be rendered and refused, not delivered",
			slog.String("component", "alerts"))
	}
	return &Notifier{
		client: &http.Client{Timeout: ntfyTimeout},
		base:   ntfyURL,
		topic:  ntfyTopic,
		token:  token,
	}
}

// alertBody is split out so -preview-alert can render one without sending it.
type alertBody struct {
	Title    string
	Message  string
	Priority string
	Tags     string
	Click    string
}

// renderAlert builds the notification for a transition, reporting false for an
// unknown kind.
func renderAlert(kind string, ctx AlertContext) (alertBody, bool) {
	// Absolute: it is opened from a phone that has no idea what the origin is.
	click := baseURL + "/sources/" + ctx.Source

	switch kind {
	case "silence":
		return alertBody{
			Title: ctx.Source + " has stopped logging",
			Message: fmt.Sprintf(
				"No records for %s.\nThe container may be down, wedged, or unable to reach ingest. Its own stdout is still the source of truth: docker logs orchard-%s",
				humanDuration(ctx.Silent), ctx.Source),
			// Not urgent: that bypasses do-not-disturb.
			Priority: "high",
			Tags:     "mute",
			Click:    click,
		}, true

	case "resumed":
		return alertBody{
			Title: ctx.Source + " is logging again",
			Message: fmt.Sprintf(
				"Records are arriving after %s of silence.",
				humanDuration(ctx.Silent)),
			Priority: "default",
			Tags:     "white_check_mark",
			Click:    click,
		}, true

	case "restart":
		return alertBody{
			Title: ctx.Source + " restarted without a clean shutdown",
			Message: fmt.Sprintf(
				"It started listening with no shutting-down record before it, so the previous process did not exit on a signal.\n%s",
				ctx.Detail),
			// A crash that repeats also trips the silence rule, which is loud.
			Priority: "default",
			Tags:     "warning",
			Click:    click,
		}, true
	}
	return alertBody{}, false
}

// humanDuration renders a gap for a person to read.
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

// Fire publishes one transition. Errors are logged and swallowed: it runs off
// the watchdog's timer with nobody to return to.
func (n *Notifier) Fire(kind string, ctx AlertContext) {
	body, ok := renderAlert(kind, ctx)
	if !ok {
		slog.Info(fmt.Sprintf("alert: unknown kind %q for %s", kind, ctx.Source),
			slog.String("component", "alerts"))
		return
	}
	if err := n.publish(context.Background(), body); err != nil {
		slog.Error(fmt.Sprintf("alert: publishing %s for %s failed: %v", kind, ctx.Source, err),
			slog.String("component", "alerts"))
		return
	}
	slog.Info(fmt.Sprintf("alert: published %s for %s", kind, ctx.Source),
		slog.String("component", "alerts"))
}

// publish posts to ntfy: the message is the body and everything else a header.
func (n *Notifier) publish(ctx context.Context, body alertBody) error {
	ctx, cancel := context.WithTimeout(ctx, ntfyTimeout)
	defer cancel()

	endpoint := strings.TrimSuffix(n.base, "/") + "/" + n.topic
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		strings.NewReader(body.Message))
	if err != nil {
		return err
	}
	// ntfy is deny-all, so an unauthenticated publish is refused even from
	// inside the bridge.
	if n.token != "" {
		req.Header.Set("Authorization", "Bearer "+n.token)
	}
	req.Header.Set("Title", body.Title)
	req.Header.Set("Priority", body.Priority)
	req.Header.Set("Tags", body.Tags)
	req.Header.Set("Click", body.Click)

	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// Drain so the connection can be reused rather than abandoned.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ntfy returned %s", strconv.Itoa(resp.StatusCode))
	}
	return nil
}

// previewAlert prints what would be published, without sending it.
func previewAlert(kind string) error {
	body, ok := renderAlert(kind, AlertContext{
		Source: "blog",
		Silent: 7 * time.Minute,
		Detail: "Previous record seen 12 minutes ago.",
	})
	if !ok {
		return fmt.Errorf("unknown alert kind %q (use 'silence', 'resumed' or 'restart')", kind)
	}
	fmt.Printf("POST %s/%s\n", strings.TrimSuffix(ntfyURL, "/"), ntfyTopic)
	fmt.Printf("Title:    %s\n", body.Title)
	fmt.Printf("Priority: %s\n", body.Priority)
	fmt.Printf("Tags:     %s\n", body.Tags)
	fmt.Printf("Click:    %s\n\n", body.Click)
	fmt.Println(body.Message)
	return nil
}
