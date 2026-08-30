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

// ntfy runs on the shared orchard-edge network and is reached by container name,
// which keeps the notification off the tunnel. It is deny-all, so publishing
// needs the write-only token in NTFY_TOKEN.
const (
	ntfyURL   = "http://orchard-ntfy:8000"
	ntfyTopic = "status"
)

const ntfyTimeout = 5 * time.Second

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
	client *http.Client
	base   string
	topic  string
	token  string
}

func NewNotifier() *Notifier {
	token := os.Getenv("NTFY_TOKEN")
	if token == "" {
		// Not fatal: refusing to start over a missing alert credential would
		// turn a missed notification into an outage.
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

// alertBody is split out so it can be rendered without being sent, for
// -preview-alert.
type alertBody struct {
	Title    string
	Message  string
	Priority string
	Tags     string
	Click    string
}

// renderAlert builds the notification for a transition, false for an unknown
// kind.
func renderAlert(kind string, ctx AlertContext) (alertBody, bool) {
	// Absolute, because it is opened from a phone with no idea of the origin.
	click := baseURL + "/" + ctx.ID

	switch kind {
	case "down":
		return alertBody{
			Title: ctx.Name + " is down",
			Message: fmt.Sprintf(
				"%s\nTwo consecutive checks failed. Latest status: %d.\nRolling average response: %d ms.",
				ctx.URL, ctx.CurrentStatus, ctx.AvgResponseMS),
			// high, not urgent: urgent bypasses do-not-disturb.
			Priority: "high",
			Tags:     "rotating_light",
			Click:    click,
		}, true

	case "recovery":
		return alertBody{
			Title: ctx.Name + " is back up",
			Message: fmt.Sprintf(
				"%s\nThe latest check returned %d.\nRolling average response: %d ms.",
				ctx.URL, ctx.CurrentStatus, ctx.AvgResponseMS),
			Priority: "default",
			Tags:     "white_check_mark",
			Click:    click,
		}, true
	}
	return alertBody{}, false
}

// Fire publishes one transition. Errors are logged and swallowed: it runs in a
// goroutine off the scheduler's path with nobody to return to.
func (n *Notifier) Fire(kind string, ctx AlertContext) {
	body, ok := renderAlert(kind, ctx)
	if !ok {
		slog.Info(fmt.Sprintf("alert: unknown kind %q for %s", kind, ctx.URL))
		return
	}
	if err := n.publish(context.Background(), body); err != nil {
		slog.Error(fmt.Sprintf("alert: publishing %s for %s failed: %v", kind, ctx.URL, err))
		return
	}
	slog.Info(fmt.Sprintf("alert: published %s for %s", kind, ctx.URL))
}

// publish posts to ntfy, whose interface is the message as the body and
// everything else as a header.
func (n *Notifier) publish(ctx context.Context, body alertBody) error {
	ctx, cancel := context.WithTimeout(ctx, ntfyTimeout)
	defer cancel()

	endpoint := strings.TrimSuffix(n.base, "/") + "/" + n.topic
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		strings.NewReader(body.Message))
	if err != nil {
		return err
	}
	// ntfy is deny-all, so an unauthenticated publish is refused on the bridge too.
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
	// Drain so the connection can be reused.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ntfy returned %s", strconv.Itoa(resp.StatusCode))
	}
	return nil
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
	fmt.Printf("POST %s/%s\n", strings.TrimSuffix(ntfyURL, "/"), ntfyTopic)
	fmt.Printf("Title:    %s\n", body.Title)
	fmt.Printf("Priority: %s\n", body.Priority)
	fmt.Printf("Tags:     %s\n", body.Tags)
	fmt.Printf("Click:    %s\n\n", body.Click)
	fmt.Println(body.Message)
	return nil
}
