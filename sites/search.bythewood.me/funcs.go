package main

import (
	"fmt"
	"strconv"
	"strings"
)

// formatNum groups thousands with commas. It takes any, because a template
// handing an int to a function declared int64 fails at render time rather than
// at build time, which 500s a page every test still passes.
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
	negative := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")

	var b strings.Builder
	for i, digit := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(digit)
	}
	if negative {
		return "-" + b.String()
	}
	return b.String()
}
