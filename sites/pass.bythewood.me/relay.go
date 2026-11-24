package main

import (
	"io"
	"net/http"
	"time"
)

var relayClient = &http.Client{Timeout: 3 * time.Second}

// relay hands dash Vaultwarden's own answer to a path that's already public,
// so dash doesn't have to sit on Vaultwarden's network to ask.
func relay(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, vaultURL+path, nil)
		if err != nil {
			http.Error(w, "bad gateway", http.StatusBadGateway)
			return
		}
		resp, err := relayClient.Do(req)
		if err != nil {
			http.Error(w, "vaultwarden didn't answer", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, io.LimitReader(resp.Body, 4096))
	}
}
