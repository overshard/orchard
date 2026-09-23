package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

// Every /media/ URL the logs have actually seen, so a rename in content/images
// that breaks one of these fails here rather than quietly going back to 404.
func TestWagtailMediaURLsResolve(t *testing.T) {
	images, err := fs.Sub(contentFS, "content/images")
	if err != nil {
		t.Fatal(err)
	}
	idx := newMediaIndex(images)

	for _, tc := range []struct{ in, want string }{
		{"alpine-linux.2e16d0ba.fill-1920x1080.format-webp.webp", "alpine-linux.webp"},
		{"alpine-linux.2e16d0ba.fill-640x480.format-webp.webp", "alpine-linux.webp"},
		{"black-logo.2e16d0ba.fill-640x480.format-webp.webp", "black-logo.webp"},
		{"borg-logo.2e16d0ba.fill-1920x1080.format-webp.webp", "borg-logo.webp"},
		{"caddyserver.com.2e16d0ba.fill-1920x1080.format-webp.webp", "caddyserver.com.webp"},
		{"editorconfig.org.2e16d0ba.fill-1920x1080.format-webp.webp", "editorconfig.org.webp"},
		{"codemirror_website.2e16d0ba.fill-640x480.format-webp.webp", "codemirror-website.webp"},
		{"dark-mode-light-mode.2e16d0ba.fill-640x480.format-webp.webp", "dark-mode-light-mode.webp"},
		{"openssh_logo.2e16d0ba.fill-640x480.format-webp.webp", "openssh-logo.webp"},
		{"scrapy-logo-banner.2e16d0ba.fill-640x480.format-webp.webp", "scrapy-logo-banner.webp"},
		{"timelite-pwa-icon.max-1920x1080.format-webp.webp", "timelite-pwa-icon.webp"},
		{"timelite-pwa-screenshot.max-1920x1080.format-webp.webp", "timelite-pwa-screenshot.webp"},
		{"new-tab-extension.max-1920x1080.format-webp.webp", "new-tab-extension.webp"},
		{"new-tab-extension-complete.max-1920x1080.format-webp.webp", "new-tab-extension-complete.webp"},
		// Wagtail truncated the stem to twenty characters.
		{"django_dockerfile_edi.2e16d0ba.fill-640x480.format-webp.webp", "django-dockerfile-edited.webp"},
		{"lighthouse-pwa-chec.2e16d0ba.fill-1920x1080.format-webp.webp", "lighthouse-pwa-check.webp"},
		{"lighthouse-pwa-check.2e16d0ba.fill-640x480.format-webp.webp", "lighthouse-pwa-check.webp"},
		// Truncated a misspelling, so only the alias table gets these.
		{"postgrseql_row_coun.2e16d0ba.fill-1920x1080.format-webp.webp", "postgresql-row-count-output.webp"},
		{"postgrseql_row_count_.2e16d0ba.fill-640x480.format-webp.webp", "postgresql-row-count-output.webp"},
	} {
		got, ok := idx.resolve(tc.in)
		if !ok || got != tc.want {
			t.Errorf("resolve(%q) = %q,%v want %q", tc.in, got, ok, tc.want)
		}
	}
}

// An unresolvable name must 404 rather than redirect to whatever sorted first.
func TestUnknownMediaIsNotGuessed(t *testing.T) {
	images, err := fs.Sub(contentFS, "content/images")
	if err != nil {
		t.Fatal(err)
	}
	idx := newMediaIndex(images)
	for _, in := range []string{
		"nothing-like-this.2e16d0ba.fill-640x480.format-webp.webp",
		"a.2e16d0ba.fill-640x480.format-webp.webp",
		"",
	} {
		if got, ok := idx.resolve(in); ok {
			t.Errorf("resolve(%q) = %q, want no match", in, got)
		}
	}
}

func TestMediaTargets(t *testing.T) {
	images, err := fs.Sub(contentFS, "content/images")
	if err != nil {
		t.Fatal(err)
	}
	idx := newMediaIndex(images)

	for _, tc := range []struct {
		path string
		want string
	}{
		{"/media/images/openssh_logo.2e16d0ba.fill-640x480.format-webp.webp", "/content/images/openssh-logo.webp"},
		{"/media/avatar_images/avatar_0da51063-cf05-4913-9b84-b1ce72bb2dfc_Avatar.webp", "/content/images/avatar.webp"},
		// Hashed with nothing recoverable in the name.
		{"/media/og_images/abc179d186894844a64d14ce317cc9ae.png", ""},
		{"/media/images/does-not-exist.2e16d0ba.fill-640x480.format-webp.webp", ""},
		{"/media/", ""},
	} {
		got, ok := idx.target(tc.path)
		if tc.want == "" {
			if ok {
				t.Errorf("target(%q) = %q, want no match", tc.path, got)
			}
			continue
		}
		if !ok || got != tc.want {
			t.Errorf("target(%q) = %q,%v want %q", tc.path, got, ok, tc.want)
		}
	}
}

func TestOGLegacySVGRedirects(t *testing.T) {
	cards := fstest.MapFS{"counting-table-row-counts-in-postgresql.png": {Data: []byte("png")}}
	h := http.StripPrefix("/og/", ogLegacy(cards, http.FileServer(http.FS(cards))))

	for _, tc := range []struct {
		path     string
		status   int
		location string
	}{
		{"/og/counting-table-row-counts-in-postgresql.svg", 301, "/og/counting-table-row-counts-in-postgresql.png"},
		{"/og/no-such-post.svg", 404, ""},
		{"/og/counting-table-row-counts-in-postgresql.png", 200, ""},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", tc.path, nil))
		if rec.Code != tc.status || rec.Header().Get("Location") != tc.location {
			t.Errorf("%s = %d %q, want %d %q", tc.path, rec.Code, rec.Header().Get("Location"), tc.status, tc.location)
		}
	}
}
