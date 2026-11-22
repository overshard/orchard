package main

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

const realUsersFixture = `{"property":"example.com","dashboard":"https://analytics.bythewood.me/x","days":7,"samples":12,
	"metrics":[{"name":"LCP","p75":1400,"value":"1.4 s","class":"ok","n":12},{"name":"Load","p75":2100,"value":"2.1 s","class":"","n":12}],
	"errors_24h":2,"errors_7d":3,"top_errors":[{"kind":"js","message":"TypeError: x is undefined","source":"/app.js:1:2","count":3}]}`

func TestRealUsersReaderCachesPerHost(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Query().Get("host") != "example.com" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(realUsersFixture))
	}))
	defer srv.Close()

	rr := newRealUsersReader()
	rr.url = srv.URL
	ru := rr.For(t.Context(), "https://example.com/")
	if ru == nil || ru.Samples != 12 || ru.Metrics[0].Value != "1.4 s" || ru.TopErrors[0].Count != 3 {
		t.Fatalf("read %+v", ru)
	}
	rr.For(t.Context(), "https://example.com/pricing")
	if hits.Load() != 1 {
		t.Errorf("asked analytics %d times for one host", hits.Load())
	}
	if rr.For(t.Context(), "https://other.example/") != nil {
		t.Error("an untracked host came back with numbers")
	}
}
