package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// benchCheck is Caddy asking about one request, over and over, through the
// same handler production runs.
func benchCheck(b *testing.B, host, uri string) {
	s := testSite(b)
	h := s.handler()
	r := httptest.NewRequest(http.MethodGet, "http://orchard-gate:8000/check", nil)
	r.Host = host
	r.Header.Set("X-Forwarded-Method", "GET")
	r.Header.Set("X-Forwarded-Uri", uri)
	r.Header.Set("X-Gate-Ip", stranger)
	r.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) Firefox/140.0")
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		h.ServeHTTP(httptest.NewRecorder(), r)
	}
}

func BenchmarkCheckPass(b *testing.B)  { benchCheck(b, "blog.bythewood.me", "/posts/some-post/?ref=x") }
func BenchmarkCheckBlock(b *testing.B) { benchCheck(b, "blog.bythewood.me", "/wp-login.php") }
