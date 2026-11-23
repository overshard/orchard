// Package ntfy publishes notifications to the ntfy in the edge. It is copied
// into every site that sends one and is byte identical in each.
package ntfy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// URL is the bridge address, never the public hostname, since Caddy refuses
// every publish route on ntfy.bythewood.me even to a request holding the token.
const URL = "http://orchard-ntfy:8000"

const Timeout = 5 * time.Second

// Waits are the pauses between Deliver's attempts, long enough to ride out ntfy
// or the bridge stalling for a few minutes.
var Waits = []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute}

// titleMax keeps a title to what fits on a lock screen.
const titleMax = 200

// Message is one notification. ntfy takes the body as the message and
// everything else as a header.
type Message struct {
	Title    string
	Body     string
	Priority string
	Tags     string

	// Link and Label make a button instead of a Click, so tapping the
	// notification only opens ntfy. No link, no button.
	Link  string
	Label string

	// Buttons are more actions in ntfy's short form, after the link's.
	Buttons []string
}

// Action is the Actions header, empty when there is no link or button.
func (m Message) Action() string {
	var out []string
	if m.Link != "" {
		out = append(out, "view, "+m.Label+", "+m.Link)
	}
	return strings.Join(append(out, m.Buttons...), "; ")
}

// Client publishes to one topic.
type Client struct {
	HTTP  *http.Client
	Base  string
	Topic string
	Token string
	Waits []time.Duration
}

// New publishes to topic over the bridge. ntfy is deny-all, so an empty token
// still sends and the publish is refused.
func New(topic, token string) *Client {
	return &Client{
		HTTP:  &http.Client{Timeout: Timeout},
		Base:  URL,
		Topic: topic,
		Token: token,
		Waits: Waits,
	}
}

func (c *Client) Configured() bool { return c.Token != "" }

func (c *Client) endpoint() string {
	return strings.TrimSuffix(c.Base, "/") + "/" + c.Topic
}

// header makes a value safe to send, since Go refuses a request whose header
// holds a CR or LF and a title can come from a model.
func header(v string) string {
	return strings.TrimSpace(strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(v))
}

func title(v string) string {
	v = header(v)
	if utf8.RuneCountInString(v) <= titleMax {
		return v
	}
	return string([]rune(v)[:titleMax-1]) + "…"
}

// refusal is ntfy answering with a status no retry will change.
type refusal struct{ status int }

func (r refusal) Error() string { return fmt.Sprintf("ntfy returned %d", r.status) }

// Publish makes one attempt, for a caller that is waiting on the answer.
func (c *Client) Publish(ctx context.Context, m Message) error {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(), strings.NewReader(m.Body))
	if err != nil {
		return err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	req.Header.Set("Title", title(m.Title))
	req.Header.Set("Priority", header(m.Priority))
	req.Header.Set("Tags", header(m.Tags))
	if a := header(m.Action()); a != "" {
		req.Header.Set("Actions", a)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// Drained so the connection can be reused.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return refusal{resp.StatusCode}
	}
	return nil
}

// Deliver publishes and tries again after each of c.Waits when the publish
// failed in a way that might pass later. It gives up the moment ctx ends, so
// a caller shutting down cancels it and is not held up.
func (c *Client) Deliver(ctx context.Context, m Message) error {
	err := c.Publish(ctx, m)
	for _, wait := range c.Waits {
		if err == nil || !retryable(err) {
			return err
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return errors.Join(err, ctx.Err())
		case <-t.C:
		}
		err = c.Publish(ctx, m)
	}
	return err
}

// retryable is false for a 4xx other than 429, since a bad token or a refused
// topic answers the same way every time.
func retryable(err error) bool {
	var r refusal
	if errors.As(err, &r) {
		return r.status == http.StatusTooManyRequests || r.status >= 500
	}
	return true
}

// Preview writes what Publish would send to topic, for checking the wording
// without sending anything.
func Preview(w io.Writer, topic string, m Message) {
	fmt.Fprintf(w, "POST %s/%s\n", URL, topic)
	fmt.Fprintf(w, "Title:    %s\n", title(m.Title))
	fmt.Fprintf(w, "Priority: %s\n", header(m.Priority))
	fmt.Fprintf(w, "Tags:     %s\n", header(m.Tags))
	if a := header(m.Action()); a != "" {
		fmt.Fprintf(w, "Actions:  %s\n", a)
	}
	fmt.Fprintf(w, "\n%s\n", m.Body)
}
