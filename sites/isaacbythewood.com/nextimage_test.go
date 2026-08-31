package main

import (
	"testing"
	"testing/fstest"
)

func testDist() fstest.MapFS {
	f := &fstest.MapFile{Data: []byte("x")}
	return fstest.MapFS{
		"images/art/acrylic-pours/000-480.avif":  f,
		"images/art/acrylic-pours/000-960.avif":  f,
		"images/art/acrylic-pours/000-1600.avif": f,
		"images/art/acrylic-pours/006-480.avif":  f,
		"images/avatar.avif":                     f,
		"images/favicon.png":                     f,
	}
}

// The old optimiser asked for a .webp and a width in the query string. Both
// moved into the filename, so the mapping has to put them back together.
func TestNextImageResolves(t *testing.T) {
	idx := newNextImageIndex(testDist())

	for _, tc := range []struct {
		url  string
		want int
		out  string
	}{
		// Smallest width at or above the request, so nothing is upscaled.
		{"/static/images/art/acrylic-pours/000.webp", 960, "/static/images/art/acrylic-pours/000-960.avif"},
		{"/static/images/art/acrylic-pours/000.webp", 500, "/static/images/art/acrylic-pours/000-960.avif"},
		{"/static/images/art/acrylic-pours/000.webp", 480, "/static/images/art/acrylic-pours/000-480.avif"},
		// More than anything here has, so the largest available.
		{"/static/images/art/acrylic-pours/000.webp", 3840, "/static/images/art/acrylic-pours/000-1600.avif"},
		// No width given at all still has to answer with something.
		{"/static/images/art/acrylic-pours/000.webp", 0, "/static/images/art/acrylic-pours/000-480.avif"},
		{"/static/images/art/acrylic-pours/006.webp", 1600, "/static/images/art/acrylic-pours/006-480.avif"},
		// Never had size variants.
		{"/static/images/avatar.webp", 640, "/static/images/avatar.avif"},
		{"/static/images/favicon.png", 64, "/static/images/favicon.png"},
	} {
		got, ok := idx.resolve(tc.url, tc.want)
		if !ok || got != tc.out {
			t.Errorf("resolve(%q, %d) = %q,%v want %q", tc.url, tc.want, got, ok, tc.out)
		}
	}
}

// An open redirect here would turn a dead endpoint into a way to bounce someone
// off this domain, and the url parameter is entirely attacker controlled.
func TestNextImageRefusesAnythingOffSite(t *testing.T) {
	idx := newNextImageIndex(testDist())

	for _, url := range []string{
		"https://evil.example/x.webp",
		"//evil.example/x.webp",
		"/etc/passwd",
		"/static/../../etc/passwd",
		"/images/art/acrylic-pours/000.webp",
		"/static/images/does-not-exist.webp",
		"",
	} {
		if got, ok := idx.resolve(url, 640); ok {
			t.Errorf("resolve(%q) = %q, want refused", url, got)
		}
	}
}
