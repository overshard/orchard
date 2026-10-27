package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"dash.bythewood.me/guard"
)

// userAgent has to look like a browser, since Yahoo answers Go's default with a
// block page instead of JSON.
const userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

// The RSS feeds want the opposite. An outlet behind AWS WAF bot control
// challenges a string claiming to be Chrome without Chrome's headers, answering
// 202 with an empty body, and naming the program passes every feed here.
const feedAgent = "dash.bythewood.me (+https://dash.bythewood.me)"

// One client for every upstream. These are all plain JSON GETs over TLS to
// hosts that keep connections alive, so pooling makes a 30 second poll cost a
// round trip instead of a handshake.
var client = &http.Client{
	Timeout: 12 * time.Second,
	Transport: &http.Transport{
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     90 * time.Second,
	},
}

// maxBody caps what a compromised or confused upstream can make this process
// allocate.
const maxBody = 4 << 20

// getJSON runs one guarded request and decodes it into out. A refusal from the
// guard comes back as an error without a request going out, which is the point
// of it.
func getJSON(ctx context.Context, g *Guard, endpoint, url string, out any) error {
	return getJSONHeaders(ctx, g, endpoint, url, nil, out)
}

// parseRetryAfter reads both forms in RFC 9110 section 10.2.3, a delay in
// seconds or an HTTP date. An unparseable value is no value.
func parseRetryAfter(v string) time.Duration {
	return guard.RetryAfter(v, time.Now())
}

// getJSONHeaders is getJSON with extra request headers. Nasdaq answers an
// Accept it doesn't recognise with an HTML challenge page instead of JSON.
func getJSONHeaders(ctx context.Context, g *Guard, endpoint, url string, headers map[string]string, out any) error {
	if err := g.Reserve(ctx, endpoint); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		g.Fail(endpoint, 0, 0)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		g.Fail(endpoint, resp.StatusCode, parseRetryAfter(resp.Header.Get("Retry-After")))
		return fmt.Errorf("%s: http %d", endpoint, resp.StatusCode)
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(out); err != nil {
		g.Fail(endpoint, resp.StatusCode, 0)
		return fmt.Errorf("%s: %w", endpoint, err)
	}

	g.Succeed(endpoint)
	return nil
}

// getPage is getJSON for the one upstream that only publishes HTML. It names
// the program, since that's a county's web server on the other end.
func getPage(ctx context.Context, g *Guard, endpoint, url string) (string, error) {
	if err := g.Reserve(ctx, endpoint); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", feedAgent)
	req.Header.Set("Accept", "text/html")

	resp, err := client.Do(req)
	if err != nil {
		g.Fail(endpoint, 0, 0)
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		g.Fail(endpoint, resp.StatusCode, parseRetryAfter(resp.Header.Get("Retry-After")))
		return "", fmt.Errorf("%s: http %d", endpoint, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		g.Fail(endpoint, resp.StatusCode, 0)
		return "", fmt.Errorf("%s: %w", endpoint, err)
	}

	g.Succeed(endpoint)
	return string(body), nil
}

// postJSON is getJSON for the upstreams that only speak GraphQL. Guarded
// identically, since a POST to somebody else's endpoint is no cheaper than a
// GET.
func postJSON(ctx context.Context, g *Guard, endpoint, url string, body, out any) error {
	return postJSONHeaders(ctx, g, endpoint, url, nil, body, out)
}

func postJSONHeaders(ctx context.Context, g *Guard, endpoint, url string, headers map[string]string, body, out any) error {
	if err := g.Reserve(ctx, endpoint); err != nil {
		return err
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		g.Fail(endpoint, 0, 0)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		g.Fail(endpoint, resp.StatusCode, parseRetryAfter(resp.Header.Get("Retry-After")))
		return fmt.Errorf("%s: http %d", endpoint, resp.StatusCode)
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(out); err != nil {
		g.Fail(endpoint, resp.StatusCode, 0)
		return fmt.Errorf("%s: %w", endpoint, err)
	}

	g.Succeed(endpoint)
	return nil
}
