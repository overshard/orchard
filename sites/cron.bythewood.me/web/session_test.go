package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeAuth answers /verify for two cookies, a browser's and a connector's.
func fakeAuth(t *testing.T) *Authenticator {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := r.Cookie(SessionCookie)
		switch {
		case c != nil && c.Value == "browser":
			_, _ = w.Write([]byte(`{"ok":true,"username":"isaac","sudo":false,"method":"code"}`))
		case c != nil && c.Value == "agent":
			_, _ = w.Write([]byte(`{"ok":true,"username":"isaac","sudo":false,"method":"connector"}`))
		default:
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"ok":false}`))
		}
	}))
	t.Cleanup(srv.Close)
	return NewAuthenticatorAt(srv.URL)
}

func withSession(method, value string) *http.Request {
	r := httptest.NewRequest(method, "/x", nil)
	r.AddCookie(&http.Cookie{Name: SessionCookie, Value: value})
	return r
}

func TestConnectorSessionOnlyReads(t *testing.T) {
	a := fakeAuth(t)
	cases := []struct {
		method, session string
		want            bool
	}{
		{"GET", "browser", true},
		{"POST", "browser", true},
		{"GET", "agent", true},
		{"HEAD", "agent", true},
		{"POST", "agent", false},
		{"DELETE", "agent", false},
		{"GET", "nobody", false},
	}
	for _, c := range cases {
		if got := a.Authenticated(withSession(c.method, c.session)); got != c.want {
			t.Errorf("%s as %s: got %v, want %v", c.method, c.session, got, c.want)
		}
	}
}

func TestRequireAuthJSONConnector(t *testing.T) {
	a := fakeAuth(t)
	ok := func(w http.ResponseWriter, r *http.Request) {}
	for _, c := range []struct {
		name    string
		h       http.HandlerFunc
		session string
		want    int
	}{
		{"plain refuses a connector", a.RequireAuthJSON(ok), "agent", http.StatusUnauthorized},
		{"plain takes a browser", a.RequireAuthJSON(ok), "browser", http.StatusOK},
		{"opted in takes a connector", a.RequireAuthJSONConnector(ok), "agent", http.StatusOK},
		{"opted in takes a browser", a.RequireAuthJSONConnector(ok), "browser", http.StatusOK},
		{"opted in refuses nobody", a.RequireAuthJSONConnector(ok), "nobody", http.StatusUnauthorized},
	} {
		rec := httptest.NewRecorder()
		c.h(rec, withSession("POST", c.session))
		if rec.Code != c.want {
			t.Errorf("%s: got %d, want %d", c.name, rec.Code, c.want)
		}
	}

	rec := httptest.NewRecorder()
	a.RequireAuth(ok)(rec, withSession("POST", "agent"))
	if rec.Code != http.StatusSeeOther {
		t.Errorf("RequireAuth on a connector POST: got %d, want a redirect to sign in", rec.Code)
	}
}
