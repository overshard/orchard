package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// Being first to see a new listing is most of what a dashboard like this is
// worth, and a dashboard only tells you when you open it. So a new listing that
// clears the filters and scores well pushes to a phone through the ntfy that
// already sits in the edge, the same way status and logging publish outages.
//
// The token is write only and comes from .env. Unset means alerts are logged and
// never delivered, and the refresh still runs: an alerter that cannot deliver
// must never stop the thing that noticed.
const (
	ntfyURL   = "http://orchard-ntfy:8000"
	ntfyTopic = "house"
)

type Alerter struct {
	client *http.Client
	token  string
	base   string
}

func NewAlerter() *Alerter {
	return &Alerter{
		client: &http.Client{Timeout: 5 * time.Second},
		token:  os.Getenv("NTFY_TOKEN"),
		base:   ntfyURL,
	}
}

// Publish sends one notification. Priority is the caller's call: a new listing
// worth driving past is worth a buzz, and a price drop on something already seen
// is not.
func (a *Alerter) Publish(ctx context.Context, title, body, priority string, clickURL string) {
	if a.token == "" {
		slog.Info("alert not delivered, no ntfy token",
			slog.String("title", title), slog.String("body", body))
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.base+"/"+ntfyTopic,
		bytes.NewReader([]byte(body)))
	if err != nil {
		slog.Error("alert request failed", slog.Any("err", err))
		return
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	// ntfy reads these as ASCII, and a listing address is ASCII, but a stray
	// non-ASCII character in an agent's remarks would otherwise 400 the publish.
	req.Header.Set("Title", ascii(title))
	req.Header.Set("Priority", priority)
	req.Header.Set("Tags", "house")
	if clickURL != "" {
		req.Header.Set("Click", clickURL)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		slog.Error("alert not delivered", slog.Any("err", err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		slog.Error("alert refused", slog.Int("status", resp.StatusCode))
	}
}

func ascii(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 128 {
			b.WriteRune(r)
		} else {
			b.WriteRune('?')
		}
	}
	return b.String()
}

// NewListing is the alert that matters. It says the score, the price, the all-in
// monthly and the detour, because those four decide whether to call the agent
// this morning or read it tonight.
func (a *Alerter) NewListing(ctx context.Context, l Listing, score float64, m Monthly, detour float64) {
	title := fmt.Sprintf("New, scores %d: %s", int(score), l.Address)
	body := fmt.Sprintf("%s list, %s a month all in, %s drop-off detour. %s, %s acres, %s beds.",
		money(l.Price), money(m.Total), fmtMinutes(detour),
		orUnnamed(l.City, "unknown town"), trimFloat(l.Acres), trimFloat(l.Beds))
	a.Publish(ctx, title, body, "high", baseURL+"/")
}

// PriceDrop is low priority on purpose. A drop is worth knowing and is not worth
// a buzz at 6am.
func (a *Alerter) PriceDrop(ctx context.Context, l Listing, from, to int) {
	title := "Price drop: " + l.Address
	body := fmt.Sprintf("%s down to %s", money(from), money(to))
	a.Publish(ctx, title, body, "low", baseURL+"/")
}
