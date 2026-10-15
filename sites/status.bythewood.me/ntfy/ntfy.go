// Package ntfy publishes notifications to the ntfy in the edge. It is copied
// into every site that sends one and is byte identical in each.
package ntfy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// URL is the bridge address, never the public hostname, since Caddy refuses
// every publish route on ntfy.bythewood.me even to a request holding the token.
const URL = "http://orchard-ntfy:8000"

const Timeout = 5 * time.Second

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
}

// Action is the Actions header, empty when there is no link.
func (m Message) Action() string {
	if m.Link == "" {
		return ""
	}
	return "view, " + m.Label + ", " + m.Link
}

// Client publishes to one topic.
type Client struct {
	HTTP  *http.Client
	Base  string
	Topic string
	Token string
}

// New publishes to topic over the bridge. ntfy is deny-all, so an empty token
// still sends and the publish is refused.
func New(topic, token string) *Client {
	return &Client{
		HTTP:  &http.Client{Timeout: Timeout},
		Base:  URL,
		Topic: topic,
		Token: token,
	}
}

func (c *Client) Configured() bool { return c.Token != "" }

func (c *Client) endpoint() string {
	return strings.TrimSuffix(c.Base, "/") + "/" + c.Topic
}

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
	req.Header.Set("Title", m.Title)
	req.Header.Set("Priority", m.Priority)
	req.Header.Set("Tags", m.Tags)
	if a := m.Action(); a != "" {
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
		return fmt.Errorf("ntfy returned %d", resp.StatusCode)
	}
	return nil
}

// Preview writes what Publish would send to topic, for checking the wording
// without sending anything.
func Preview(w io.Writer, topic string, m Message) {
	fmt.Fprintf(w, "POST %s/%s\n", URL, topic)
	fmt.Fprintf(w, "Title:    %s\n", m.Title)
	fmt.Fprintf(w, "Priority: %s\n", m.Priority)
	fmt.Fprintf(w, "Tags:     %s\n", m.Tags)
	if a := m.Action(); a != "" {
		fmt.Fprintf(w, "Actions:  %s\n", a)
	}
	fmt.Fprintf(w, "\n%s\n", m.Body)
}
