package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Outage notifications, by publishing to ntfy.
//
// Both of the Rust version's channels are gone, and neither was cut for
// tidiness.
//
// **Email could not survive the move home.** alerts.rs delivered direct to MX
// with lettre: an MX lookup, then SMTP on port 25 with opportunistic STARTTLS
// and a HELO of bythewood.me. From a residential address that fails three
// independent ways, any one of them fatal: consumer ISPs block outbound 25,
// there is no control over the PTR record on a dynamic address, and consumer
// ranges sit on the Spamhaus PBL by default so Gmail and Microsoft reject on
// that alone. None are fixable from this side. decisions/0007 settled it.
//
// **Discord went with it**, which decisions/0007 did not require and Isaac
// chose on 2026-08-26. It worked, so this is worth being honest about: it is a
// third party in the path of the one message that has to arrive when his own
// infrastructure is broken, and the whole migration was about not depending on
// other people's servers. ntfy is a Go binary he runs, reached over the
// tailnet by an Android client holding a WebSocket, with no Firebase anywhere
// in it.
//
// The honest limit, accepted in 0007: an alerter running at home cannot tell
// him the house lost power or its internet. Nothing in this file changes that.

// ntfyURL is where a notification is published: the base, then the topic.
//
// ASSUMPTION, and the one line to change when it is settled. The ntfy server
// does not exist yet; 0007 decided it and nothing has been stood up. This
// assumes it lands as a container on the same Docker network as this app,
// reached by its compose service name, which is how every other
// container-to-container call in this repo works and which keeps the
// notification off the tunnel exactly as 0007 requires. Tailscale is how his
// phone reaches ntfy, not how this reaches it.
//
// Until that container exists every publish fails, which is logged once per
// transition and changes nothing else. That is the same best-effort contract
// the Rust version had: an alert that cannot be delivered must never break the
// check loop that noticed the outage.
const (
	ntfyURL   = "http://ntfy:8000"
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

// Notifier publishes transitions. It holds a client because, unlike the
// prober, this one wants connection reuse: it is talking to the same host
// every time and the handshake is not the thing being measured.
type Notifier struct {
	client *http.Client
	base   string
	topic  string
}

func NewNotifier() *Notifier {
	return &Notifier{
		client: &http.Client{Timeout: ntfyTimeout},
		base:   ntfyURL,
		topic:  ntfyTopic,
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
			// high, not urgent. Urgent bypasses the phone's do-not-disturb, and
			// these are hobby sites whose downtime decisions/0007 explicitly
			// says does not matter. Reserve the interrupt for something that
			// deserves waking him up.
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
			// Deliberately quieter than the outage. The recovery is good news
			// and does not need to interrupt anything.
			Priority: "default",
			Tags:     "white_check_mark",
			Click:    click,
		}, true
	}
	return alertBody{}, false
}

// Fire publishes one transition. Errors are logged and swallowed: this is
// called from a goroutine off the scheduler's path and there is nobody to
// return an error to.
func (n *Notifier) Fire(kind string, ctx AlertContext) {
	body, ok := renderAlert(kind, ctx)
	if !ok {
		log.Printf("alert: unknown kind %q for %s", kind, ctx.URL)
		return
	}
	if err := n.publish(context.Background(), body); err != nil {
		log.Printf("alert: publishing %s for %s failed: %v", kind, ctx.URL, err)
		return
	}
	log.Printf("alert: published %s for %s", kind, ctx.URL)
}

// publish posts to ntfy.
//
// The message is the request body and everything else is a header, which is
// ntfy's plain-HTTP publishing interface: the same shape as the `curl -d`
// decisions/0007 wanted to be able to use from any future project.
func (n *Notifier) publish(ctx context.Context, body alertBody) error {
	ctx, cancel := context.WithTimeout(ctx, ntfyTimeout)
	defer cancel()

	endpoint := strings.TrimSuffix(n.base, "/") + "/" + n.topic
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		strings.NewReader(body.Message))
	if err != nil {
		return err
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

// previewAlert prints what would be published, for eyeballing the wording
// without waiting for something to break.
//
// It replaces the Rust version's `status preview-email <down|recovery>`, which
// existed for exactly the same reason and rendered an HTML mail to stdout.
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
