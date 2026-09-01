package main

import (
	"net/http"
	"strings"

	"auth.bythewood.me/web"
)

// reqContext is where a request came from, as far as the edge can tell.
//
// The country and city headers are trustworthy here only because the tunnel is
// the only way in: no container in this repo publishes a host port, so nothing
// reaches this process without passing cloudflared and Caddy first. The day one
// of them is directly reachable, a client can set CF-IPCountry to whatever it
// likes and the session list starts lying.
//
// CF-IPCountry arrives by default. The rest need the "Add visitor location
// headers" managed transform turned on for the zone, and stay empty without it,
// which is why nothing below treats an empty one as an error.
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
		// Truncated: a user agent is display-only here and some are enormous.
		UA:  truncate(r.UserAgent(), 300),
		Ray: r.Header.Get("CF-Ray"),
	}
}

// Where reads as a place a person recognises, or as the address when the
// transform is off and there is nothing better to say.
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
