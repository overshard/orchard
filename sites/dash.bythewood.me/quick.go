package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"dash.bythewood.me/web"
)

// dash is public and its home page can come out of Cloudflare's cache, so the
// page is the same for everybody and only these endpoints know who is asking.
const chatSendURL = "http://orchard-chat:8000/api/send"

// chatClient has no overall timeout, since a turn streams for minutes. Chat
// answers the headers as soon as the turn is queued, so a wait for them past
// this is chat being down, and it lands inside Cloudflare's hundred seconds.
var chatClient = &http.Client{
	Transport: &http.Transport{ResponseHeaderTimeout: 90 * time.Second},
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

type quickReq struct {
	Message string `json:"message"`
	ConvID  string `json:"conversation_id"`
	RunID   string `json:"run_id"`
}

// quick tells the page whether to offer the box at all.
func (s *site) quick(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]bool{"signed_in": s.auth.Authenticated(r)})
}

// quickSend relays one turn to chat over the bridge and streams its events
// back, so the browser never calls chat cross origin and chat needs no CORS.
// Only the session cookie goes with it, since chat checks that against auth
// the same way this did.
func (s *site) quickSend(w http.ResponseWriter, r *http.Request) {
	// A page on another bythewood.me host is same-site and would carry the
	// cookie, so the fence is same origin rather than SameSite.
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
		quickError(w, http.StatusForbidden, "cross origin")
		return
	}
	// A form can post text/plain across origins without asking first, and
	// JSON cannot.
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		quickError(w, http.StatusUnsupportedMediaType, "json only")
		return
	}
	if !s.auth.Authenticated(r) {
		quickError(w, http.StatusUnauthorized, "not signed in")
		return
	}
	cookie, err := r.Cookie(web.SessionCookie)
	if err != nil {
		quickError(w, http.StatusUnauthorized, "not signed in")
		return
	}

	var in quickReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
		quickError(w, http.StatusBadRequest, "bad request")
		return
	}
	in.Message = strings.TrimSpace(in.Message)
	if in.Message == "" || (in.ConvID == "" && in.RunID == "") {
		quickError(w, http.StatusBadRequest, "empty message")
		return
	}
	body, _ := json.Marshal(in)

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.chat, bytes.NewReader(body))
	if err != nil {
		quickError(w, http.StatusInternalServerError, "bad request")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.AddCookie(&http.Cookie{Name: web.SessionCookie, Value: cookie.Value})

	resp, err := chatClient.Do(req)
	if err != nil {
		slog.Warn("quick chat relay failed", slog.String("component", "quick"), slog.Any("err", err))
		quickError(w, http.StatusBadGateway, "chat is not answering")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, io.LimitReader(resp.Body, 4<<10))
		return
	}

	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_ = rc.Flush()

	buf := make([]byte, 32<<10)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if rc.Flush() != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func quickError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
