package main

import "html/template"

var templateFuncs = template.FuncMap{
	// The sparkline paths are computed in spark.go and go straight into a d
	// attribute, so they need the type that says a template may write them
	// without escaping. Everything else on this page is text and stays escaped.
	"path": func(d string) template.HTMLAttr { return template.HTMLAttr(d) },

	// range gives a zero based index and the feed ranks read from one.
	"inc": func(i int) int { return i + 1 },
}
