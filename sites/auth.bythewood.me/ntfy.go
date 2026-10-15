package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"auth.bythewood.me/ntfy"
)

// The topic and its write token are auth's alone so the alert token in status'
// and logging's .env can't mint login codes.
const ntfyTopic = "auth"

type Notifier struct {
	client *ntfy.Client
}

func NewNotifier() *Notifier {
	token := os.Getenv("AUTH_NTFY_TOKEN")
	if token == "" {
		slog.Warn("AUTH_NTFY_TOKEN is unset; login codes cannot be delivered, so only recovery codes will work",
			slog.String("component", "auth"))
	}
	return &Notifier{client: ntfy.New(ntfyTopic, token)}
}

func (n *Notifier) configured() bool { return n.client.Configured() }

// codeMessage says where the login was asked from, so a code from somewhere the
// phone isn't is a code not to type. Priority low so a flood lands silently, and
// only a session that actually opened buzzes.
func codeMessage(code string, c reqContext) ntfy.Message {
	return ntfy.Message{
		Title:    "Login code " + code,
		Body:     fmt.Sprintf("Asked for from %s (%s).\nIf that is not you, ignore this and the code expires on its own.", c.Where(), c.IP),
		Priority: "low",
		Tags:     "closed_lock_with_key",
	}
}

func sessionMessage(c reqContext, how string) ntfy.Message {
	return ntfy.Message{
		Title:    "New login to bythewood.me",
		Body:     fmt.Sprintf("Signed in with %s from %s (%s).\n%s", how, c.Where(), c.IP, c.UA),
		Priority: "high",
		Tags:     "key",
		Link:     baseURL + "/sessions",
		Label:    "Sessions",
	}
}

func recoveryMessage(c reqContext, remaining int) ntfy.Message {
	return ntfy.Message{
		Title: "A recovery code was used",
		Body: fmt.Sprintf("Signed in from %s (%s) without the phone.\n%d codes left, and regenerating replaces all of them.",
			c.Where(), c.IP, remaining),
		Priority: "high",
		Tags:     "rotating_light",
		Link:     baseURL + "/security",
		Label:    "Security",
	}
}

func lockoutMessage(c reqContext) ntfy.Message {
	return ntfy.Message{
		Title: "Login codes locked for a day",
		Body: fmt.Sprintf("%d wrong codes in 24 hours, the last from %s (%s).\nRecovery codes still work.",
			dailyFailures, c.Where(), c.IP),
		Priority: "high",
		Tags:     "rotating_light",
		Link:     baseURL + "/activity",
		Label:    "Activity",
	}
}

func (n *Notifier) publish(ctx context.Context, m ntfy.Message) error {
	return n.client.Publish(ctx, m)
}

// notify publishes without a caller to return to, for the alerts that must
// never fail the request that triggered them.
func (n *Notifier) notify(m ntfy.Message) {
	if err := n.publish(context.Background(), m); err != nil {
		slog.Error("publishing an alert failed",
			slog.String("component", "auth"),
			slog.Any("err", err))
	}
}

// ntfyPublicURL is what a phone points at, as opposed to ntfy.URL, which is the
// bridge address this process publishes to.
const ntfyPublicURL = "https://ntfy.bythewood.me"
