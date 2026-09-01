package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// The container name on the orchard-edge bridge, never the public hostname:
// Caddy refuses every publish route on ntfy.bythewood.me, so publishing is
// reachable only from the bridge even holding the write token.
//
// A topic of its own, and a write token only this site holds. The alert topics
// would not do: the token in status' and logging' .env files can publish to
// those, so a copy of either would be able to mint its own login codes.
const (
	ntfyURL   = "http://orchard-ntfy:8000"
	ntfyTopic = "auth"
)

const ntfyTimeout = 5 * time.Second

type Notifier struct {
	client *http.Client
	base   string
	topic  string
	token  string
}

func NewNotifier() *Notifier {
	token := os.Getenv("AUTH_NTFY_TOKEN")
	if token == "" {
		slog.Warn("AUTH_NTFY_TOKEN is unset; login codes cannot be delivered, so only recovery codes will work",
			slog.String("component", "auth"))
	}
	return &Notifier{
		client: &http.Client{Timeout: ntfyTimeout},
		base:   ntfyURL,
		topic:  ntfyTopic,
		token:  token,
	}
}

func (n *Notifier) configured() bool { return n.token != "" }

type message struct {
	Title    string
	Body     string
	Priority string
	Tags     string
	Click    string
}

// codeMessage carries where the login was asked from, which is the phishing
// defence: a code arriving from somewhere the phone is not is a code not to
// type. CISA and Microsoft's answer to MFA fatigue is the same idea.
//
// Priority low, so a flood of these lands silently in the drawer instead of
// buzzing. The alert for a session that actually opened is the loud one.
func codeMessage(code string, c reqContext) message {
	return message{
		Title:    "Login code " + code,
		Body:     fmt.Sprintf("Asked for from %s (%s).\nIf that is not you, ignore this and the code expires on its own.", c.Where(), c.IP),
		Priority: "low",
		Tags:     "closed_lock_with_key",
	}
}

func sessionMessage(c reqContext, how string) message {
	return message{
		Title:    "New login to bythewood.me",
		Body:     fmt.Sprintf("Signed in with %s from %s (%s).\n%s", how, c.Where(), c.IP, c.UA),
		Priority: "high",
		Tags:     "key",
		Click:    baseURL + "/sessions",
	}
}

func recoveryMessage(c reqContext, remaining int) message {
	return message{
		Title: "A recovery code was used",
		Body: fmt.Sprintf("Signed in from %s (%s) without the phone.\n%d codes left, and regenerating replaces all of them.",
			c.Where(), c.IP, remaining),
		Priority: "high",
		Tags:     "rotating_light",
		Click:    baseURL + "/security",
	}
}

// publish posts to ntfy: the message is the body and everything else a header.
func (n *Notifier) publish(ctx context.Context, m message) error {
	ctx, cancel := context.WithTimeout(ctx, ntfyTimeout)
	defer cancel()

	endpoint := strings.TrimSuffix(n.base, "/") + "/" + n.topic
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(m.Body))
	if err != nil {
		return err
	}
	// ntfy is deny-all, so an unauthenticated publish is refused even from
	// inside the bridge.
	if n.token != "" {
		req.Header.Set("Authorization", "Bearer "+n.token)
	}
	req.Header.Set("Title", m.Title)
	req.Header.Set("Priority", m.Priority)
	req.Header.Set("Tags", m.Tags)
	if m.Click != "" {
		req.Header.Set("Click", m.Click)
	}

	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ntfy returned %d", resp.StatusCode)
	}
	return nil
}

// notify publishes without a caller to return to, for the alerts that must
// never fail the request that triggered them.
func (n *Notifier) notify(m message) {
	if err := n.publish(context.Background(), m); err != nil {
		slog.Error("publishing an alert failed",
			slog.String("component", "auth"),
			slog.Any("err", err))
	}
}

// ntfyPublicURL is what a phone points at, as opposed to ntfyURL, which is the
// bridge address this process publishes to.
const ntfyPublicURL = "https://ntfy.bythewood.me"
