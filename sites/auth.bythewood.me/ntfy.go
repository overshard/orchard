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

// The bridge address, never the public hostname, since Caddy refuses publishing
// on ntfy.bythewood.me. The topic and its write token are auth's alone so the
// alert token in status' and logging's .env can't mint login codes.
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
	Link     string
	Label    string
}

// codeMessage says where the login was asked from, so a code from somewhere the
// phone isn't is a code not to type. Priority low so a flood lands silently, and
// only a session that actually opened buzzes.
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
		Link:     baseURL + "/sessions",
		Label:    "Sessions",
	}
}

func recoveryMessage(c reqContext, remaining int) message {
	return message{
		Title: "A recovery code was used",
		Body: fmt.Sprintf("Signed in from %s (%s) without the phone.\n%d codes left, and regenerating replaces all of them.",
			c.Where(), c.IP, remaining),
		Priority: "high",
		Tags:     "rotating_light",
		Link:     baseURL + "/security",
		Label:    "Security",
	}
}

func lockoutMessage(c reqContext) message {
	return message{
		Title: "Login codes locked for a day",
		Body: fmt.Sprintf("%d wrong codes in 24 hours, the last from %s (%s).\nRecovery codes still work.",
			dailyFailures, c.Where(), c.IP),
		Priority: "high",
		Tags:     "rotating_light",
		Link:     baseURL + "/activity",
		Label:    "Activity",
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
	// A button instead of Click, so tapping the notification only opens ntfy.
	if m.Link != "" {
		req.Header.Set("Actions", "view, "+m.Label+", "+m.Link)
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
