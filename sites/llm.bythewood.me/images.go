package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

type imageReq struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Size   string `json:"size"`
}

// images forwards one picture to llama-swap, which takes the chat model off the
// card to make room. The prompt is logged like a message would be, and the
// picture is not, since a couple of megabytes of base64 a row would bury the
// log it sits in.
func (s *site) images(w http.ResponseWriter, r *http.Request, k Key) {
	started := time.Now()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBytes))
	if err != nil {
		http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		return
	}
	var req imageReq
	_ = json.Unmarshal(body, &req)

	keep := r.Header.Get(incognitoHeader) != "1"
	call := Call{KeyID: k.ID, Caller: k.Name, Model: req.Model}
	if keep {
		call.Messages = string(mustJSON([]map[string]string{{"role": "user", "content": req.Prompt}}))
	}
	defer func() {
		if !keep {
			return
		}
		call.MS = time.Since(started).Milliseconds()
		s.store.LogCall(call)
	}()

	up, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.upstream+r.URL.Path, bytes.NewReader(body))
	if err != nil {
		call.Err = err.Error()
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	up.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(up)
	if err != nil {
		call.Err = err.Error()
		call.Status = http.StatusBadGateway
		slog.Error("upstream refused an image", "err", err, "caller", k.Name)
		http.Error(w, "the model server is not answering", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	call.Status = resp.StatusCode

	out, err := io.ReadAll(io.LimitReader(resp.Body, maxRequestBytes))
	if err != nil {
		call.Err = err.Error()
		http.Error(w, "the model server stopped part way through", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(out)

	if resp.StatusCode >= 400 {
		call.Err = upstreamReason(out)
		slog.Error("upstream refused an image", "status", resp.StatusCode,
			"caller", k.Name, "reason", call.Err)
		return
	}
	call.Completion = describeImages(out, req.Size)
}

// describeImages is what the log keeps in place of the picture.
func describeImages(out []byte, size string) string {
	var resp struct {
		Data []struct {
			B64 string `json:"b64_json"`
		} `json:"data"`
	}
	if json.Unmarshal(out, &resp) != nil || len(resp.Data) == 0 {
		return "[no image came back]"
	}
	if size == "" {
		size = "default size"
	}
	kb := len(resp.Data[0].B64) * 3 / 4 / 1024
	if len(resp.Data) == 1 {
		return fmt.Sprintf("[an image, %s, %d KB]", size, kb)
	}
	return fmt.Sprintf("[%d images, %s]", len(resp.Data), size)
}
