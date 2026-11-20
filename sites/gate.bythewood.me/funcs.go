package main

import (
	"fmt"
	"html/template"
	"strconv"
	"strings"
	"time"
)

func funcs() template.FuncMap {
	return template.FuncMap{
		"ago": ago, "until": until, "comma": comma, "asset": assetURL, "dict": dict, "pct": pct,
		"oneLine": oneLine, "lines": lines, "hoursOf": func(d time.Duration) int { return int(d / time.Hour) },
		"utc": func(t time.Time) string { return t.UTC().Format("2006-01-02 15:04 UTC") },
		"short": func(s string, n int) string {
			if len(s) <= n {
				return s
			}
			return s[:n] + "…"
		},
		"actionKind": actionKind,
		"count":      count,
		"clock":      func(t time.Time) string { return t.UTC().Format("15:04:05") },
		"pctOf": func(n, of int) string {
			if of == 0 {
				return "0%"
			}
			return fmt.Sprintf("%.1f%%", 100*float64(n)/float64(of))
		},
		"add":  func(a, b int) int { return a + b },
		"list": func(v ...string) []string { return v },
		"sparkKind": func(a string) string {
			if k := actionKind(a); k != "" {
				return k
			}
			return "flat"
		},
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

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

func until(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Until(t)
	switch {
	case d <= 0:
		return "ended " + ago(t)
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes())+1)
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
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
	return max(8, 100*n/top)
}

// oneLine is a rule's conditions joined with and, with long regexes cut so a
// table row stays one line.
func oneLine(when string) string {
	var parts []string
	for _, l := range lines(when) {
		if len(l) > 90 {
			l = l[:87] + "..."
		}
		parts = append(parts, l)
	}
	return strings.Join(parts, " and ")
}

func lines(when string) []string {
	var out []string
	for _, l := range strings.Split(when, "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	return out
}

// actionKind is the chip colour for an action.
func actionKind(a string) string {
	switch a {
	case actBlock, vBanned:
		return "bad"
	case actLimit, actChallenge:
		return "warn"
	case actAllow, vPass, vSolved, vExempt:
		return "ok"
	}
	return ""
}
