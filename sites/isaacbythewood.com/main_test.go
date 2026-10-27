package main

import (
	"io/fs"
	"testing"

	"isaacbythewood.com/web"
)

// NewRenderer resolves the list at boot and not at build, so a page left listed
// after its file was deleted compiles, ships, and then crash-loops on startup.
func TestEveryListedTemplateParses(t *testing.T) {
	templates, err := fs.Sub(templateFS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := web.NewRenderer(templates, nil, layoutTemplates, pageTemplates); err != nil {
		t.Fatalf("the template set does not parse: %v", err)
	}
}
