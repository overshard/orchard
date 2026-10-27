package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIsPublicIP(t *testing.T) {
	for _, bad := range []string{
		"127.0.0.1", "::1", "10.0.0.5", "172.18.0.3", "192.168.1.10",
		"169.254.169.254", "100.64.0.1", "0.0.0.0", "fe80::1", "fd00::1", "::ffff:127.0.0.1",
	} {
		if isPublicIP(net.ParseIP(bad)) {
			t.Errorf("%s was treated as public", bad)
		}
	}
	for _, good := range []string{"1.1.1.1", "93.184.216.34", "2606:4700:4700::1111"} {
		if !isPublicIP(net.ParseIP(good)) {
			t.Errorf("%s was treated as internal", good)
		}
	}
}

// A name that resolves inward gets past any check on the string, so this points
// the real client at a real loopback server.
func TestTheArticleClientRefusesLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("this must never be read"))
	}))
	defer srv.Close()

	req, err := http.NewRequestWithContext(context.Background(), "GET", srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := publicClient(5 * time.Second).Do(req)
	if err == nil {
		resp.Body.Close()
		t.Fatal("a loopback address was fetched")
	}
	if !strings.Contains(err.Error(), "refusing") {
		t.Errorf("err = %v, want the fence to have refused it", err)
	}
}

func TestFetchArticleRefusesAPrivateAddress(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<article><p>" + strings.Repeat("an internal page that must stay internal ", 40) + "</p></article>"))
	}))
	defer srv.Close()

	g := NewGuard(t.TempDir())
	if got := fetchArticle(context.Background(), g, srv.URL+"/admin"); got != "" {
		t.Errorf("fetchArticle returned %q from a loopback server", got)
	}
	if hit {
		t.Error("the loopback server was reached")
	}
}
