package ntfy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type seen struct {
	path   string
	header http.Header
	body   string
}

func server(t *testing.T, status int) (*httptest.Server, *[]seen) {
	t.Helper()
	var got []seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = append(got, seen{r.Method + " " + r.URL.Path, r.Header.Clone(), string(b)})
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestPublishSendsTheMessageAsBodyAndHeaders(t *testing.T) {
	srv, got := server(t, http.StatusOK)
	c := New("alerts", "tk_test")
	c.Base = srv.URL + "/"

	err := c.Publish(t.Context(), Message{
		Title: "down", Body: "line one\nline two", Priority: "high", Tags: "warning",
		Link: "https://example.com/x", Label: "Open",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(*got) != 1 {
		t.Fatalf("%d requests, want 1", len(*got))
	}
	r := (*got)[0]
	if r.path != "POST /alerts" || r.body != "line one\nline two" {
		t.Errorf("sent %s with %q", r.path, r.body)
	}
	for k, want := range map[string]string{
		"Authorization": "Bearer tk_test", "Title": "down", "Priority": "high",
		"Tags": "warning", "Actions": "view, Open, https://example.com/x",
	} {
		if v := r.header.Get(k); v != want {
			t.Errorf("%s is %q, want %q", k, v, want)
		}
	}
}

// No token still publishes, so a missing credential shows up as ntfy refusing
// instead of as nothing happening, and no link means no button.
func TestPublishLeavesOutWhatIsNotThere(t *testing.T) {
	srv, got := server(t, http.StatusOK)
	c := New("alerts", "")
	c.Base = srv.URL
	if c.Configured() {
		t.Error("a client with no token reads as configured")
	}
	if err := c.Publish(t.Context(), Message{Title: "code", Body: "b", Priority: "low", Tags: "key"}); err != nil {
		t.Fatal(err)
	}
	h := (*got)[0].header
	if _, ok := h["Authorization"]; ok {
		t.Error("sent an Authorization header with no token")
	}
	if _, ok := h["Actions"]; ok {
		t.Error("sent an Actions header with no link")
	}
}

func TestPublishReportsARefusal(t *testing.T) {
	srv, _ := server(t, http.StatusForbidden)
	c := New("alerts", "tk_test")
	c.Base = srv.URL
	err := c.Publish(t.Context(), Message{Title: "t", Body: "b"})
	if err == nil || err.Error() != "ntfy returned 403" {
		t.Errorf("got %v, want ntfy returned 403", err)
	}
}

func TestNewPublishesOverTheBridge(t *testing.T) {
	c := New("alerts", "tk_test")
	if c.Base != "http://orchard-ntfy:8000" || c.endpoint() != "http://orchard-ntfy:8000/alerts" {
		t.Errorf("publishes to %s", c.endpoint())
	}
	if c.HTTP.Timeout != Timeout {
		t.Errorf("client timeout is %s", c.HTTP.Timeout)
	}
}

func TestPreviewMatchesWhatIsSent(t *testing.T) {
	var buf bytes.Buffer
	Preview(&buf, "alerts", Message{
		Title: "down", Body: "body", Priority: "high", Tags: "warning",
		Link: "https://example.com/x", Label: "Open",
	})
	want := "POST http://orchard-ntfy:8000/alerts\n" +
		"Title:    down\nPriority: high\nTags:     warning\n" +
		"Actions:  view, Open, https://example.com/x\n\nbody\n"
	if buf.String() != want {
		t.Errorf("preview is\n%s\nwant\n%s", buf.String(), want)
	}
	buf.Reset()
	Preview(&buf, "alerts", Message{Title: "t", Body: "b"})
	if strings.Contains(buf.String(), "Actions") {
		t.Error("previewed a button with no link")
	}
}
