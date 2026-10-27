package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// UserAgent is a current Chrome on Windows because several of these endpoints
// answer 403 to anything that looks automated, and an old version on Linux is a
// combination almost nothing real sends. Chrome itself zeroes the last two fields.
const UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36"

// Chrome sends these on every request, so a Chrome user agent arriving without
// them contradicts itself, which is cheaper to score than the agent string is.
const (
	clientHintUA       = `"Google Chrome";v="149", "Chromium";v="149", "Not?A_Brand";v="24"`
	clientHintMobile   = "?0"
	clientHintPlatform = `"Windows"`
)

func get(ctx context.Context, d *Deps, url string, accept string) ([]byte, error) {
	return getWith(ctx, d, url, accept, nil)
}

// getWith takes per host headers because Nominatim and Forsyth County want an
// agent that names the program instead of a browser's.
func getWith(ctx context.Context, d *Deps, url string, accept string, extra map[string]string) ([]byte, error) {
	host := hostOf(url)
	if blocked, left := d.Guard.Blocked(host); blocked {
		return nil, refusedErr(host, left)
	}
	// The spend ceiling, checked before the gap so a spent pool costs no wait.
	if err := d.Budgets.Take(host); err != nil {
		return nil, err
	}
	if err := d.Guard.Wait(host); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Sec-Ch-Ua", clientHintUA)
	req.Header.Set("Sec-Ch-Ua-Mobile", clientHintMobile)
	req.Header.Set("Sec-Ch-Ua-Platform", clientHintPlatform)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	client := d.Public
	if client == nil {
		client = d.HTTP
	}
	resp, err := client.Do(req)
	if err != nil {
		// A refusal by the fence is not the host rate limiting us, so it must
		// not trip the breaker and lock out a host that never answered.
		if strings.Contains(err.Error(), "refusing") {
			return nil, fmt.Errorf("%s is not a public address", host)
		}
		d.Guard.Stumble(host)
		return nil, fmt.Errorf("%s did not answer, so try a different source: %s", host, transportReason(err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	// 202 is how both DuckDuckGo and ESPN say no, and it reads as an empty result
	// unless it's turned into an error here. This and a 429 get the long box.
	if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusTooManyRequests {
		d.Guard.Trip(host)
		return nil, fmt.Errorf("%s is rate limiting us (status %d)", host, resp.StatusCode)
	}
	// An empty 200 from a page is a page with nothing on it, not a ban.
	if resp.StatusCode == 200 && len(strings.TrimSpace(string(body))) == 0 {
		d.Guard.Stumble(host)
		return nil, fmt.Errorf("%s returned an empty page, so try a different source", host)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%s answered %d. %s", host, resp.StatusCode, statusAdvice(resp.StatusCode))
	}
	d.Guard.OK(host)
	return body, nil
}

func getJSON(ctx context.Context, d *Deps, url string, into any) error {
	return getJSONHeaders(ctx, d, url, nil, into)
}

func getJSONHeaders(ctx context.Context, d *Deps, url string, extra map[string]string, into any) error {
	b, err := getWith(ctx, d, url, "application/json", extra)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, into)
}

// statusAdvice tells the model what to do next, since a bare status code reads
// as "the tool is broken" and the answer that follows says nothing could be
// found. A wall and a wrong address need opposite responses.
func statusAdvice(code int) string {
	switch code {
	case http.StatusForbidden, http.StatusUnauthorized:
		return "That site blocks automated readers, and asking again will not change it. Use a different source."
	case http.StatusNotFound:
		return "There is no page at that address. Do not guess another one, search for the page and fetch the url the search returns."
	case http.StatusPaymentRequired, http.StatusGone:
		return "That page is not readable without paying or is gone. Use a different source."
	}
	if code >= 500 {
		return "That site is having trouble of its own. Use a different source."
	}
	return "Use a different source."
}

// transportReason keeps the useful half of a transport error. The full text is
// the whole request line, and when a fetch follows a redirect the address in it
// is wherever it ended up, which reads as the wrong host having failed.
func transportReason(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "context deadline exceeded"), strings.Contains(s, "Client.Timeout"):
		return "it took too long to answer"
	case strings.Contains(s, "no such host"):
		return "that name does not resolve"
	case strings.Contains(s, "connection refused"):
		return "nothing is listening there"
	case strings.Contains(s, "certificate"):
		return "its certificate did not check out"
	}
	// A redirect chain names the address it ended on and not the one asked for,
	// so the url is dropped instead of being reported as the host that failed.
	if i := strings.Index(s, ": "); i > 0 && strings.HasPrefix(s, "Get \"") {
		return strings.TrimSpace(s[i+2:])
	}
	return s
}

func hostOf(url string) string {
	s := strings.TrimPrefix(strings.TrimPrefix(url, "https://"), "http://")
	if i := strings.IndexAny(s, "/?"); i >= 0 {
		s = s[:i]
	}
	return s
}
