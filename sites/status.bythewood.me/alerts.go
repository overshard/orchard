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

// Outage notifications, by publishing to ntfy.
//
// Email is not an option from a residential address. Delivering direct to MX
// fails three independent ways, any one of them fatal: consumer ISPs block
// outbound port 25, there is no control over the PTR record on a dynamic
// address, and consumer ranges sit on the Spamhaus PBL by default, so Gmail and
// Microsoft reject on that alone.
//
// ntfy rather than a hosted webhook, because the one message that has to arrive
// when this infrastructure is broken should not depend on a third party's
// servers. It is a self-hosted Go binary, reached over the tailnet by an
// Android client holding a WebSocket, with no Firebase in the path.
//
// The limit this accepts: an alerter running at home cannot report that the
// house lost power or its internet.

// ntfyURL is where a notification is published: the base, then the topic.
//
// ntfy runs as a container on the shared orchard-edge network and is reached by
// container name, which is how every other container-to-container call in this
// repo works and is what keeps the notification off the tunnel. Tailscale is
// how a phone reaches ntfy, not how this does: the sidecar next to it shares
// its network namespace and serves the same port to the tailnet.
//
// ntfy is deny-all: this publishes with a write-only token from NTFY_TOKEN, and
// the account behind that token can publish to the alert topics and read
// nothing. Caddy separately refuses every publish route on the public hostname,
// so publishing is reachable only from this bridge even with the token in hand.
//
// If the container is missing every publish fails, logged once per transition
// and changing nothing else: an alert that cannot be delivered must never break
// the check loop that noticed the outage.
const (
	ntfyURL   = "http://orchard-ntfy:8000"
	ntfyTopic = "status"
)

const ntfyTimeout = 5 * time.Second

// AlertContext is the property snapshot as of the moment the alert fired, so
// the numbers in the notification are the ones that triggered it rather than
// whatever the next probe finds.
type AlertContext struct {
	ID            string
	Name          string
	URL           string
	CurrentStatus int64
	AvgResponseMS int64
}

// Notifier publishes transitions. Unlike the prober it holds a client, since it
// talks to the same host every time and the handshake is not being measured.
type Notifier struct {
	client *http.Client
	base   string
	topic  string
	token  string
}

func NewNotifier() *Notifier {
	token := os.Getenv("NTFY_TOKEN")
	if token == "" {
		// Said once, loudly, at startup. The alternative is a site that looks
		// healthy and silently cannot tell anyone when it is not, which is the
		// exact failure this whole path exists to prevent. It is not fatal:
		// refusing to start over a missing alert credential would turn a
		// quiet notification into an outage.
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

// alertBody is what gets published, split out so it can be rendered without
// being sent (see the -preview-alert flag).
type alertBody struct {
	Title    string
	Message  string
	Priority string
	Tags     string
	Click    string
}

// render builds the notification for a transition. Returns false for an
// unknown kind rather than inventing one.
func renderAlert(kind string, ctx AlertContext) (alertBody, bool) {
	// The link goes to the property dashboard, absolute, because it is opened
	// from a phone that has no idea what the origin is.
	click := baseURL + "/" + ctx.ID

	switch kind {
	case "down":
		return alertBody{
			Title: ctx.Name + " is down",
			Message: fmt.Sprintf(
				"%s\nTwo consecutive checks failed. Latest status: %d.\nRolling average response: %d ms.",
				ctx.URL, ctx.CurrentStatus, ctx.AvgResponseMS),
			// high, not urgent. Urgent bypasses the phone's do-not-disturb,
			// which none of these sites being down is worth.
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
			// Quieter than the outage: good news does not need to interrupt
			// anything.
			Priority: "default",
			Tags:     "white_check_mark",
			Click:    click,
		}, true
	}
	return alertBody{}, false
}

// Fire publishes one transition. Errors are logged and swallowed, since this
// runs in a goroutine off the scheduler's path with nobody to return to.
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

// publish posts to ntfy. The message is the request body and everything else is
// a header, which is ntfy's plain-HTTP publishing interface and the same shape
// as a `curl -d` from anything else.
func (n *Notifier) publish(ctx context.Context, body alertBody) error {
	ctx, cancel := context.WithTimeout(ctx, ntfyTimeout)
	defer cancel()

	endpoint := strings.TrimSuffix(n.base, "/") + "/" + n.topic
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		strings.NewReader(body.Message))
	if err != nil {
		return err
	}
	// Write-only token, from this site's .env. ntfy is deny-all, so an
	// unauthenticated publish is refused even from inside the bridge.
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

// previewAlert prints what would be published, for checking the wording without
// waiting for something to break.
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
