package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
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
	if _, err := web.NewRenderer(templates, templateFuncs, layoutTemplates, pageTemplates); err != nil {
		t.Fatalf("the template set does not parse: %v", err)
	}
}

// The CSP allows the early script by hash, so the bytes the template writes
// have to be exactly the ones hashed.
func TestEarlyScriptMatchesItsHash(t *testing.T) {
	templates, err := fs.Sub(templateFS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	r, err := web.NewRenderer(templates, templateFuncs, layoutTemplates, pageTemplates)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	r.Render(rec, http.StatusOK, pageTemplates[0], PageData{Script: "/static/x.js"})
	body := rec.Body.String()
	if !strings.Contains(body, "<script>"+earlyScript+"</script>") {
		t.Error("the early script was not written byte for byte")
	}
	if !strings.Contains(csp(), earlyScriptHash()) {
		t.Error("the CSP is missing the early script's hash")
	}
}
