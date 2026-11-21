package main

import (
	"html/template"
	"net/http"
	"strings"
	"time"
)

var templateFuncs = template.FuncMap{
	"ago":        ago,
	"when":       when,
	"eventLabel": eventLabel,
	"eventClass": eventClass,
	"browser":    browser,
	"pips":       pips,
	"dict":       dict,
	"until":      until,
	"method":     methodLabel,
}

// dict builds a map inline, so a partial can take named arguments instead of
// one positional value.
func dict(pairs ...any) map[string]any {
	out := make(map[string]any, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		key, ok := pairs[i].(string)
		if !ok {
			continue
		}
		out[key] = pairs[i+1]
	}
	return out
}

// pips renders the recovery code meter as ten lit or unlit marks, because eight
// of ten is read at a glance and the number 8 is not.
func pips(remaining int) []bool {
	out := make([]bool, recoveryCount)
	for i := range out {
		out[i] = i < remaining
	}
	return out
}

// when is the one timestamp format on this site. Everything here is UTC,
// because a container's local time silently differs from the host's.
func when(t time.Time) string { return t.UTC().Format("2006-01-02 15:04 UTC") }

func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return itoa(int(d.Minutes())) + "m ago"
	case d < 24*time.Hour:
		return itoa(int(d.Hours())) + "h ago"
	default:
		return itoa(int(d.Hours()/24)) + "d ago"
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// eventLabel turns a stored kind into something readable without changing the
// stored value, which stays machine-facing so a query written today still runs.
func eventLabel(kind string) string {
	switch kind {
	case evCodeRequested:
		return "code requested"
	case evCodeSent:
		return "code sent"
	case evCodeFailed:
		return "wrong code"
	case evCodeExpired:
		return "code expired"
	case evLogin:
		return "signed in with"
	case evLogout:
		return "signed out"
	case evSessionRevoked:
		return "session revoked"
	case evRecoveryUsed:
		return "recovery code used"
	case evRecoveryFailed:
		return "wrong recovery code"
	case evRecoveryRotated:
		return "recovery codes replaced"
	case evRateLimited:
		return "rate limited"
	case evCeilingHit:
		return "send ceiling hit"
	case evUsernameChanged:
		return "username changed"
	case evConnectorAdded:
		return "agent connected"
	}
	return strings.ReplaceAll(kind, "_", " ")
}

// eventClass colours the ones worth noticing on the activity page.
func eventClass(kind string) string {
	switch kind {
	case evCodeFailed, evRecoveryFailed, evRateLimited, evCeilingHit:
		return "is-warn"
	case evLogin, evRecoveryUsed, evRecoveryRotated, evUsernameChanged, evConnectorAdded:
		return "is-note"
	}
	return ""
}

// browser reduces a user agent to something that fits in a table cell. It is
// display only and guesses, so it never claims more than the family.
func browser(ua string) string {
	switch {
	case ua == "":
		return "unknown"
	case strings.HasPrefix(ua, connectorUA):
		return strings.TrimPrefix(ua, connectorUA) + " (connector)"
	case strings.Contains(ua, "Firefox/"):
		return "Firefox"
	case strings.Contains(ua, "Edg/"):
		return "Edge"
	case strings.Contains(ua, "OPR/"):
		return "Opera"
	case strings.Contains(ua, "Chrome/"):
		return "Chrome"
	case strings.Contains(ua, "Safari/"):
		return "Safari"
	case strings.Contains(ua, "curl/"):
		return "curl"
	}
	return "other"
}

// robots refuses the whole site. There is nothing here worth indexing and the
// login form is the only page a stranger can reach at all.
func robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	// Explicit, because Cloudflare stamps its own TTL on a header-less response
	// and a stale robots.txt at the edge is four hours of the wrong answer.
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
}
