package main

import (
	"regexp"
	"strconv"
	"strings"
)

// A query is words and field:value tokens. A plain word must appear in the
// message, path, host, component or attrs and -word must not. A field matches a
// column or an attrs key, a comma means any of, and * in path or msg is a wildcard.

type term struct {
	Neg    bool
	Field  string
	Values []string
	Text   string
}

// columnFields map a query field to its column. Anything else that looks like
// a field is an attrs key.
var columnFields = map[string]string{
	"source":    "source",
	"level":     "level",
	"status":    "status",
	"component": "component",
	"method":    "method",
	"host":      "host",
	"path":      "path",
	"ip":        "ip",
	"cf_ray":    "cf_ray",
	"msg":       "msg",
}

var fieldAliases = map[string]string{
	"src":  "source",
	"lvl":  "level",
	"comp": "component",
	"ray":  "cf_ray",
}

var attrKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,39}$`)

// tokenize splits on spaces outside double quotes. The quotes stay in, so a
// comma inside them can still be told from one between values.
func tokenize(s string) []string {
	var out []string
	var b strings.Builder
	quoted := false
	for _, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
			b.WriteRune(r)
		case !quoted && (r == ' ' || r == '\t' || r == '\n'):
			if b.Len() > 0 {
				out = append(out, b.String())
			}
			b.Reset()
		default:
			b.WriteRune(r)
		}
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}

func unquote(s string) string { return strings.ReplaceAll(s, `"`, "") }

// splitValues splits on commas outside quotes.
func splitValues(s string) []string {
	var out []string
	quoted, start := false, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			quoted = !quoted
		case ',':
			if !quoted {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// parseQuery reads a query string into terms. It caps the count, since every
// term is another clause on a scan.
func parseQuery(s string) []term {
	var out []term
	for _, tok := range tokenize(s) {
		if len(out) >= 24 {
			break
		}
		t := term{}
		if len(tok) > 1 && tok[0] == '-' {
			t.Neg = true
			tok = tok[1:]
		}
		if i := strings.IndexByte(tok, ':'); i > 0 && i < len(tok)-1 && !strings.Contains(tok[:i], `"`) {
			name := strings.ToLower(tok[:i])
			if a, ok := fieldAliases[name]; ok {
				name = a
			}
			_, isCol := columnFields[name]
			if isCol || attrKey.MatchString(tok[:i]) {
				if !isCol {
					name = tok[:i]
				}
				t.Field = name
				for _, v := range splitValues(tok[i+1:]) {
					if v = strings.TrimSpace(unquote(v)); v != "" && len(t.Values) < 12 {
						t.Values = append(t.Values, normalizeValue(name, v))
					}
				}
				if len(t.Values) > 0 {
					out = append(out, t)
				}
				continue
			}
		}
		if t.Text = unquote(tok); t.Text != "" {
			out = append(out, t)
		}
	}
	return out
}

func normalizeValue(field, v string) string {
	switch field {
	case "level":
		v = strings.ToUpper(v)
		if v == "WARNING" {
			v = "WARN"
		}
	case "source":
		v = normalizeSource(v)
	case "method":
		v = strings.ToUpper(v)
	case "status", "component":
		v = strings.ToLower(v)
	}
	return v
}

// String writes a term back the way a person would type it.
func (t term) String() string {
	s := ""
	if t.Neg {
		s = "-"
	}
	if t.Field == "" {
		return s + quoteIfNeeded(t.Text)
	}
	vals := make([]string, len(t.Values))
	for i, v := range t.Values {
		vals[i] = quoteIfNeeded(v)
	}
	return s + t.Field + ":" + strings.Join(vals, ",")
}

func quoteIfNeeded(v string) string {
	if strings.ContainsAny(v, " \t,\"") || v == "" {
		return `"` + strings.ReplaceAll(v, `"`, "") + `"`
	}
	return v
}

func joinTerms(ts []term) string {
	parts := make([]string, len(ts))
	for i, t := range ts {
		parts[i] = t.String()
	}
	return strings.Join(parts, " ")
}

// toggleValue adds field:value as an include, or takes it away if it's already
// there, merging into an existing include on the same field as an or.
func toggleValue(ts []term, field, value string, neg bool) []term {
	value = normalizeValue(field, value)
	out := make([]term, 0, len(ts)+1)
	done := false
	for _, t := range ts {
		if t.Field == field && t.Neg == neg && !done {
			idx := -1
			for i, v := range t.Values {
				if v == value {
					idx = i
				}
			}
			done = true
			if idx >= 0 {
				t.Values = append(append([]string{}, t.Values[:idx]...), t.Values[idx+1:]...)
				if len(t.Values) == 0 {
					continue
				}
			} else {
				t.Values = append(append([]string{}, t.Values...), value)
			}
		}
		out = append(out, t)
	}
	if !done {
		out = append(out, term{Neg: neg, Field: field, Values: []string{value}})
	}
	return out
}

// without drops terms[i].
func without(ts []term, i int) []term {
	return append(append([]term{}, ts[:i]...), ts[i+1:]...)
}

// textMatch is where a plain word is looked for.
const textMatch = "(msg LIKE ? ESCAPE '\\' OR path LIKE ? ESCAPE '\\' OR attrs LIKE ? ESCAPE '\\' OR component LIKE ? ESCAPE '\\' OR host LIKE ? ESCAPE '\\')"

// termClauses turns terms into SQL, all values as placeholders. skip leaves out the
// includes on one field, which is how a facet shows its other values.
func termClauses(ts []term, skip string) ([]string, []any) {
	var cl []string
	var args []any
	for _, t := range ts {
		if t.Field == "" {
			like := "%" + escapeLike(t.Text) + "%"
			c := textMatch
			if t.Neg {
				c = "NOT " + c
			}
			cl = append(cl, c)
			args = append(args, like, like, like, like, like)
			continue
		}
		if t.Field == skip && !t.Neg {
			continue
		}
		var ors []string
		for _, v := range t.Values {
			c, a := valueClause(t.Field, v)
			if c == "" {
				continue
			}
			ors = append(ors, c)
			args = append(args, a...)
		}
		if len(ors) == 0 {
			// A field with no usable value matches nothing, instead of
			// quietly matching everything.
			if !t.Neg {
				cl = append(cl, "0")
			}
			continue
		}
		c := "(" + strings.Join(ors, " OR ") + ")"
		if t.Neg {
			// NULL from json_extract on a missing key has to count as not
			// equal, or excluding a key would hide every line without it.
			c = "NOT COALESCE(" + c + ", 0)"
		}
		cl = append(cl, c)
	}
	return cl, args
}

func valueClause(field, v string) (string, []any) {
	col, isCol := columnFields[field]
	if !isCol {
		// A CASE, since SQLite may evaluate both sides of an AND and one
		// malformed bag would fail the whole scan.
		return "(CASE WHEN json_valid(attrs) THEN CAST(json_extract(attrs, ?) AS TEXT) END = ?)", []any{`$."` + field + `"`, v}
	}
	switch field {
	case "status":
		// 5xx or a bare 5 is a class, anything else a code.
		cls := strings.TrimRight(v, "x")
		if len(cls) == 1 && cls[0] >= '1' && cls[0] <= '5' {
			lo := int64(cls[0]-'0') * 100
			return "(status >= ? AND status < ?)", []any{lo, lo + 100}
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return "", nil
		}
		return "status = ?", []any{n}
	case "path", "msg":
		like := strings.ReplaceAll(escapeLike(v), "*", "%")
		return col + " LIKE ? ESCAPE '\\'", []any{like}
	}
	return col + " = ?", []any{v}
}

// highlightTerms are the plain words worth marking in a line.
func highlightTerms(ts []term) []string {
	var out []string
	for _, t := range ts {
		if t.Field == "" && !t.Neg && len(t.Text) > 1 {
			out = append(out, t.Text)
		}
	}
	return out
}

// legacyTerms folds the old form fields into the query, so a bookmark or the
// mcp tool still asking with source= and status= gets the same lines.
func legacyTerms(q map[string][]string) []term {
	first := func(k string) string {
		if v := q[k]; len(v) > 0 {
			return strings.TrimSpace(v[0])
		}
		return ""
	}
	var out []term
	if v := normalizeSource(first("source")); v != "" {
		out = append(out, term{Field: "source", Values: []string{v}})
	}
	switch v := strings.ToUpper(first("level")); v {
	case "DEBUG", "INFO", "WARN", "ERROR":
		out = append(out, term{Field: "level", Values: []string{v}})
	}
	if v := first("status"); len(v) == 1 && v[0] >= '1' && v[0] <= '5' {
		out = append(out, term{Field: "status", Values: []string{v + "xx"}})
	}
	if v := strings.ToLower(first("component")); v != "" {
		out = append(out, term{Field: "component", Values: []string{v}})
	}
	return out
}
