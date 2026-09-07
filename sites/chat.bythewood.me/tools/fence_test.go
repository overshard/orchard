package tools

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPublicURLRefusesEverythingButHttp(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"file:///home/ubuntu/Downloads/070726 WellsFargo.pdf", "already in this conversation"},
		{"file:///etc/passwd", "no filesystem"},
		{"ftp://example.com/x", "only http and https"},
		{"gopher://example.com", "only http and https"},
		{"example.com/page", "needs to start with https"},
		{"http://orchard-auth:8000/verify", "not a public address"},
		{"http://localhost:8000/", "not a public address"},
		{"http://orchard-llm:8000/v1/models", "orchard tools"},
	} {
		got, err := publicURL(c.in)
		if err == nil {
			t.Errorf("%q was allowed as %q", c.in, got)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q gave %q, want it to mention %q", c.in, err, c.want)
		}
	}
}

func TestPublicURLAllowsOrdinaryPages(t *testing.T) {
	for _, ok := range []string{
		"https://en.wikipedia.org/wiki/Asmongold",
		"http://example.com/a/b?c=d",
		"https://www.espn.com/soccer/team/fixtures/_/id/364/liverpool",
	} {
		if _, err := publicURL(ok); err != nil {
			t.Errorf("%q was refused: %v", ok, err)
		}
	}
}

// The address ranges that reach this machine or its network. IsPrivate does not
// cover carrier grade NAT or the metadata address, and both matter.
func TestIsPublicIP(t *testing.T) {
	for _, bad := range []string{
		"127.0.0.1", "::1", "10.0.0.5", "172.18.0.3", "192.168.1.10",
		"169.254.169.254", "100.64.0.1", "0.0.0.0", "fe80::1", "fd00::1",
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

// The string check can be got past by a name that resolves inward, so the real
// fence is on the dial. This proves it by pointing a real client at a real
// loopback server, which is exactly the shape of the attack.
func TestTheClientRefusesToDialLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("this must never be read"))
	}))
	defer srv.Close()

	c := publicClient(5 * time.Second)
	req, err := http.NewRequestWithContext(context.Background(), "GET", srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Do(req)
	if err == nil {
		resp.Body.Close()
		t.Fatal("a loopback address was fetched")
	}
	if !strings.Contains(err.Error(), "refusing") {
		t.Errorf("err = %v, want the fence to have refused it", err)
	}
}

// get() must not put a host in the penalty box for being internal, or one bad
// suggestion from the model locks out a host that never even answered.
func TestTheFenceDoesNotTripTheBreaker(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	d := &Deps{
		HTTP:   srv.Client(),
		Public: publicClient(5 * time.Second),
		Now:    time.Now,
		Guard:  NewGuard(time.Minute),
	}
	if _, err := get(context.Background(), d, srv.URL, ""); err == nil {
		t.Fatal("loopback was fetched through get")
	}
	host := hostOf(srv.URL)
	if blocked, _ := d.Guard.Blocked(host); blocked {
		t.Error("the breaker tripped on a refusal that was ours, not the host's")
	}
}
