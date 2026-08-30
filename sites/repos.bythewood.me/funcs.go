package main

import (
	"fmt"
	"html/template"
	"net/url"
	"strings"
	"time"
)

var templateFuncs = template.FuncMap{
	"dict":       dict,
	"humanBytes": humanBytes,
	"humanTime":  humanTime,
	"shortSHA":   shortSHA,
	"pathSegs":   pathSegs,
	"urlPath":    urlPath,
	"firstLine":  firstLine,
	"pluralize":  pluralize,
	"percentOf":  percentOf,
	"add":        func(a, b int) int { return a + b },
	"sub":        func(a, b int) int { return a - b },
}

// dict lets a partial take more than one value, since a template has one dot.
func dict(pairs ...any) (map[string]any, error) {
	if len(pairs)%2 != 0 {
		return nil, fmt.Errorf("dict: odd number of arguments (%d)", len(pairs))
	}
	out := make(map[string]any, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		key, ok := pairs[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict: key %d is %T, want string", i, pairs[i])
		}
		out[key] = pairs[i+1]
	}
	return out, nil
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// humanTime is the relative form; templates keep the absolute date in a title attribute.
func humanTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return ago(int(d.Minutes()), "minute")
	case d < 24*time.Hour:
		return ago(int(d.Hours()), "hour")
	case d < 30*24*time.Hour:
		return ago(int(d.Hours()/24), "day")
	case d < 365*24*time.Hour:
		return ago(int(d.Hours()/24/30), "month")
	default:
		return ago(int(d.Hours()/24/365), "year")
	}
}

func ago(n int, unit string) string {
	if n == 1 {
		return "1 " + unit + " ago"
	}
	return fmt.Sprintf("%d %ss ago", n, unit)
}

func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// Crumb is one element of the breadcrumb above a tree or blob view.
type Crumb struct {
	Name string
	Path string
}

func pathSegs(p string) []Crumb {
	if p == "" {
		return nil
	}
	var out []Crumb
	var acc []string
	for _, seg := range strings.Split(p, "/") {
		if seg == "" {
			continue
		}
		acc = append(acc, seg)
		out = append(out, Crumb{Name: seg, Path: strings.Join(acc, "/")})
	}
	return out
}

// urlPath percent-encodes a path for an href segment by segment, so separators
// survive and a "?", "#" or space does not break the link.
func urlPath(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func pluralize(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// percentOf drives the push size meter, clamped to 100.
func percentOf(n, total int64) int {
	if total <= 0 {
		return 0
	}
	p := int(n * 100 / total)
	if p > 100 {
		return 100
	}
	return p
}
