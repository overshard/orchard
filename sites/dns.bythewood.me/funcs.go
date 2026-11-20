package main

import (
	"fmt"
	"html/template"
	"strconv"
	"strings"
	"time"

	"github.com/miekg/dns"
)

func funcs() template.FuncMap {
	return template.FuncMap{
		"ago": ago, "comma": comma, "asset": assetURL, "dict": dict, "pct": pct, "count": count,
		"utc":   func(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05 UTC") },
		"clock": func(t time.Time) string { return t.UTC().Format("15:04:05") },
		"short": func(s string, n int) string {
			if len(s) <= n {
				return s
			}
			return s[:n] + "…"
		},
		"pctOf": func(n, of int) string {
			if of == 0 {
				return "0%"
			}
			return fmt.Sprintf("%.1f%%", 100*float64(n)/float64(of))
		},
		"qtype":      func(t uint16) string { return dns.TypeToString[t] },
		"statusKind": statusKind,
		"statusWord": statusWord,
		"listName":   listName,
		"parents":    parents,
		"rcode":      func(c int) string { return dns.RcodeToString[c] },
		"since":      time.Since,
		"deviceName": func(names map[int64]string, id int64) string {
			if id == 0 {
				return "every device"
			}
			if n := names[id]; n != "" {
				return n
			}
			return "a removed device"
		},
		"days": func(d time.Duration) int {
			return int(d.Hours() / 24)
		},
	}
}

func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%s %s", comma(n), many)
}

func ago(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

func comma(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		return "-" + s
	}
	return s
}

func dict(kv ...any) map[string]any {
	m := map[string]any{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1]
	}
	return m
}

func pct(n, top int) int {
	if top <= 0 {
		return 0
	}
	return max(2, 100*n/top)
}

func statusKind(s string) string {
	switch s {
	case "block", "deny", "fail":
		return "bad"
	case "allow":
		return "ok"
	}
	return ""
}

func statusWord(s string) string {
	switch s {
	case "block":
		return "blocked"
	case "deny":
		return "blocked"
	case "allow":
		return "allowed"
	case "fail":
		return "failed"
	case "local":
		return "local"
	case "rewrite":
		return "rewritten"
	}
	return "answered"
}

// listName is a reason as a reader wants it, a list's short name or the rule.
func listName(reason string) string {
	if l, ok := listByKey(reason); ok {
		return l.Name
	}
	return reason
}

// parents is a name and each domain above it down to two labels, the choices
// for how wide an allow or block should be.
func parents(name string) []string {
	var out []string
	for n := name; strings.Count(n, ".") >= 1; {
		out = append(out, n)
		i := strings.IndexByte(n, '.')
		n = n[i+1:]
	}
	return out
}
