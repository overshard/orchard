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

// Notifications, by publishing to ntfy.
//
// This is the second publisher in the repo and a deliberate copy of status'
// rather than a shared package. The 2026-08-28 split gave every site its own
// module and its own copy of web/, and the whole argument for shipping logs per
// site rather than from a sidecar was that a shared component is the thing this
// repo keeps deleting. Forty lines of HTTP is a smaller cost than the coupling.
//
// What differs from status' copy is the whole point of having two: status
// answers "is the site reachable from the internet", checked from outside on a
// timer. This answers "is the site still telling me what it is doing", which is
// a different failure and one status cannot see. A container that is up,
// answering probes, and has silently stopped logging looks perfect from
// outside.

// ntfyURL is the base; the topic is appended.
//
// A separate topic from status' on purpose. ntfy subscribes and mutes per
// topic, so two topics mean an outage alert and a "blog went quiet" alert can
// be treated differently on the phone without a rule engine.
//
// Container name on the orchard-edge bridge, never the public hostname. ntfy is
// deny-all, so this publishes with a write-only token from NTFY_TOKEN; that
// account can publish to the alert topics and read nothing. Caddy separately
// refuses every publish route on ntfy.bythewood.me, so publishing is reachable
// only from this bridge even with the token in hand.
const (
	ntfyURL   = "http://orchard-ntfy:8000"
	ntfyTopic = "logging"
)

const ntfyTimeout = 5 * time.Second

// AlertContext is the snapshot as of the moment the alert fired, so the numbers
// in the notification are the ones that triggered it rather than whatever is
// true by the time it is read.
type AlertContext struct {
	Source string
	// Silent is how long the source had been quiet. Zero for kinds where it
	// means nothing.
	Silent time.Duration
	// Detail is one extra clause, already phrased, for kinds that have one.
	Detail string
}

// Notifier publishes transitions.
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

// renderAlert builds the notification for a transition. Returns false for an
// unknown kind rather than inventing one.
func renderAlert(kind string, ctx AlertContext) (alertBody, bool) {
	// Absolute, because it is opened from a phone that has no idea what the
	// origin is. Deep link to the source rather than the overview: the alert
	// already named which one, and the next question is always "doing what".
	click := baseURL + "/sources/" + ctx.Source

	switch kind {
	case "silence":
		return alertBody{
			Title: ctx.Source + " has stopped logging",
			Message: fmt.Sprintf(
				"No records for %s.\nThe container may be down, wedged, or unable to reach ingest. Its own stdout is still the source of truth: docker logs orchard-%s",
				humanDuration(ctx.Silent), ctx.Source),
			// high, not urgent. Urgent bypasses do-not-disturb, which none of
			// these sites is worth. Same call status made.
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
			// Quieter than the alert: good news does not need to interrupt.
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
			// Worth knowing, not worth waking up for. A crash that repeats
			// will also trip the silence rule, which is the loud one.
			Priority: "default",
			Tags:     "warning",
			Click:    click,
		}, true
	}
	return alertBody{}, false
}

// humanDuration renders a gap the way a person reads one. Minutes are the unit
// that matters here: nothing fires under the silence threshold, and past an
// hour the exact figure stops changing the response.
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

// Fire publishes one transition. Errors are logged and swallowed: this runs off
// the watchdog's timer with nobody to return to, and an alert that cannot be
// delivered must never break the loop that noticed the problem.
//
// It logs through slog, which means the record about a failed alert is itself
// shipped into this site's own database. That is intentional and is not a loop:
// LocalSink writes it to the queue, it never becomes an HTTP request, and
// nothing here alerts on it.
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
