package main

import (
	"errors"
	"fmt"
	"html/template"
	"strconv"
	"time"
)

func funcs() template.FuncMap {
	return template.FuncMap{
		"ago": ago, "analyticsID": func() string { return analyticsID }, "comma": comma, "asset": assetURL, "dict": dict, "pct": pct, "size": size,
		"kind": deviceKind, "keep": func() int { return keepDays }, "since": time.Since, "word": eventWord, "evKind": eventKind,
		"utc":   func(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05 UTC") },
		"stamp": func(t time.Time) string { return t.UTC().Format("Jan 2 15:04") },
		"day":   func(t time.Time) string { return t.UTC().Format("Jan 2, 2006") },
	}
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

func comma(v any) string {
	var n int64
	switch x := v.(type) {
	case int:
		n = int64(x)
	case int64:
		n = x
	case float64:
		n = int64(x)
	}
	s := strconv.FormatInt(n, 10)
	neg := n < 0
	if neg {
		s = s[1:]
	}
	out := ""
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out += ","
		}
		out += string(c)
	}
	if neg {
		return "-" + out
	}
	return out
}

func pct(n, of int) int {
	if of <= 0 {
		return 0
	}
	p := n * 100 / of
	if p < 2 && n > 0 {
		return 2
	}
	return p
}

func size(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(b)/(1<<10))
	}
	return fmt.Sprintf("%d B", b)
}

func dict(kv ...any) (map[string]any, error) {
	if len(kv)%2 != 0 {
		return nil, errors.New("dict wants pairs")
	}
	m := make(map[string]any, len(kv)/2)
	for i := 0; i < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok {
			return nil, errors.New("dict keys are strings")
		}
		m[k] = kv[i+1]
	}
	return m, nil
}
