package main

import (
	"fmt"
	"html/template"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

var templateFuncs = template.FuncMap{
	"dict":       dict,
	"footerFor":  footerFor,
	"humanBytes": humanBytes,
	"humanTime":  humanTime,
	"shortSHA":   shortSHA,
	"pathSegs":   pathSegs,
	"urlPath":    urlPath,
	"firstLine":  firstLine,
	"pluralize":  pluralize,
	"percentOf":  percentOf,
	"num":        formatNum,
	"shortAgo":   shortAgo,
	"parentPath": parentPath,
	"base":       path.Base,
	"blobState":  blobState,
	"sumOf": func(v []float64) float64 {
		t := 0.0
		for _, x := range v {
			t += x
		}
		return t
	},
	"add": func(a, b int) int { return a + b },
	"sub": func(a, b int) int { return a - b },
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

// humanTime is the relative form, templates keep the absolute date in a title attribute.
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
	for seg := range strings.SplitSeq(p, "/") {
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
	if before, _, ok := strings.Cut(s, "\n"); ok {
		return before
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

// formatNum is the thousands separator. It takes any, since a template handing
// an int to an int64 parameter only fails at render time.
func formatNum(v any) string {
	var n int64
	switch t := v.(type) {
	case int:
		n = int64(t)
	case int64:
		n = t
	case float64:
		n = int64(t)
	default:
		return fmt.Sprint(v)
	}
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, d := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(d)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// parentPath is the directory above p, empty at the top.
func parentPath(p string) string {
	i := strings.LastIndex(p, "/")
	if i < 0 {
		return ""
	}
	return p[:i]
}

// blobState is the end of a file page's title.
func blobState(d map[string]any) string {
	switch {
	case d["TooLarge"] == true:
		return "too large to show"
	case d["Binary"] == true:
		return "binary"
	}
	if n, ok := d["Lines"].(int); ok {
		return formatNum(n) + " " + pluralize(n, "line", "lines")
	}
	return ""
}
