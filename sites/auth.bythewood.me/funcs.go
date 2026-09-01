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
}

// dict builds a map inline, so a partial can take named arguments rather than
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
		return "signed in"
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
	}
	return strings.ReplaceAll(kind, "_", " ")
}

// eventClass colours the ones worth noticing on the activity page.
func eventClass(kind string) string {
	switch kind {
	case evCodeFailed, evRecoveryFailed, evRateLimited, evCeilingHit:
		return "is-warn"
	case evLogin, evRecoveryUsed, evRecoveryRotated, evUsernameChanged:
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

// Inline rather than a file so it cannot drift from the navbar logo, which is
// the same markup. A north star with three companions, which is the same
// language as the field behind the sign in form and still reads at 16px, where
// a full constellation turns to mush.
const faviconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64">
  <g stroke="#6b9e78" stroke-width="1.6" opacity="0.55" stroke-linecap="round">
    <path d="M46 15 L34 28"/>
    <path d="M18 47 L29 36"/>
    <path d="M46 15 L52 40"/>
  </g>
  <path d="M32 4 L36.4 26.2 L58 32 L36.4 37.8 L32 60 L27.6 37.8 L6 32 L27.6 26.2 Z"
        fill="#6b9e78"/>
  <circle cx="46" cy="15" r="3.4" fill="#c9d9cb"/>
  <circle cx="18" cy="47" r="2.6" fill="#7eaab8"/>
  <circle cx="52" cy="40" r="2.2" fill="#c9a84c"/>
</svg>`

func favicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write([]byte(faviconSVG))
}

// robots refuses the whole site. There is nothing here worth indexing and the
// login form is the only page a stranger can reach at all.
func robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
}
