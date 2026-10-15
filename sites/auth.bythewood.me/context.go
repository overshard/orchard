package main

import (
	"net/http"
	"strings"

	"auth.bythewood.me/web"
)

// reqContext is where a request came from, as far as Cloudflare can tell. The
// location headers are only trustworthy because nothing publishes a host port, so
// every request passes cloudflared first. Otherwise a client sets its own country.
type reqContext struct {
	IP      string
	Country string
	City    string
	UA      string
	Ray     string
}

func requestContext(r *http.Request) reqContext {
	return reqContext{
		IP:      web.ClientIP(r),
		Country: strings.TrimSpace(r.Header.Get("CF-IPCountry")),
		City:    strings.TrimSpace(r.Header.Get("CF-IPCity")),
		// A user agent is display only here and some are enormous.
		UA:  truncate(r.UserAgent(), 300),
		Ray: r.Header.Get("CF-Ray"),
	}
}

// Where reads as a place a person recognises, or the address when there's
// nothing better. City needs the zone's "Add visitor location headers" managed
// transform and is empty without it, so an empty one is never an error here.
func (c reqContext) Where() string {
	switch {
	case c.City != "" && c.Country != "":
		return c.City + ", " + c.Country
	case c.Country != "":
		return c.Country
	default:
		return c.IP
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
