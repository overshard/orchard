package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// searchSystem keeps the model to reporting what the results say, since the
// caller wants the results and does its own answering.
const searchSystem = "You run exactly one web search for the query you are given, with the query as written. " +
	"Then, for each result that bears on it, quote the passage that does, citing it. " +
	"Do not answer the question yourself and add nothing the results do not say."

type searchReq struct {
	Query   string   `json:"query"`
	N       int      `json:"n"`
	Domains []string `json:"domains"`
}

type searchHit struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
	PageAge string `json:"page_age,omitempty"`
}

// search runs Anthropic's web search for a caller that wants hits and not an
// answer. The results come back encrypted, so the snippets are the passages
// the model quoted from them, and a result it didn't quote keeps its title only.
func (s *site) search(w http.ResponseWriter, r *http.Request, k Key) {
	if s.claude == nil {
		writeOAError(w, http.StatusNotImplemented, "unsupported", "search needs the gateway on Claude")
		return
	}
	if !s.claudeAllowed(w, k) {
		return
	}
	started := time.Now()
	var req searchReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil || strings.TrimSpace(req.Query) == "" {
		writeOAError(w, http.StatusBadRequest, "invalid_request_error", "a query is required")
		return
	}
	if req.N < 1 || req.N > 12 {
		req.N = 6
	}

	keep := r.Header.Get(incognitoHeader) != "1"
	call := Call{KeyID: k.ID, Caller: k.Name, Model: s.claude.model, Tools: "web search", started: started}
	if keep {
		call.Messages = string(mustJSON([]map[string]string{{"role": "user", "content": "search: " + req.Query}}))
	}
	defer func() { s.store.record(call, keep) }()

	p := msgParams{
		Model:     s.claude.model,
		MaxTokens: 2048,
		Thinking:  map[string]string{"type": "disabled"},
		System:    []textBlock{newText(searchSystem)},
		Messages:  []message{{Role: "user", Content: []any{newText(req.Query)}}},
		Tools:     []any{newWebSearch(1, req.Domains)},
	}
	var raw []json.RawMessage
	for range maxPauses + 1 {
		resp, err := s.claude.newMessage(r.Context(), p)
		if err != nil {
			s.claudeFailed(w, err, &call)
			return
		}
		raw = append(raw, resp.Content...)
		call.addUsage(resp.Usage)
		if resp.StopReason != "pause_turn" {
			break
		}
		p.Messages = append(p.Messages, resp.toParam())
	}

	hits, failure := searchHits(readBlocks(raw))
	if len(hits) == 0 {
		msg := "the search found nothing"
		if failure != "" {
			msg = "the search failed: " + failure
		}
		call.Status, call.Err = http.StatusBadGateway, msg
		writeOAError(w, http.StatusBadGateway, "search_failed", msg)
		return
	}
	if len(hits) > req.N {
		hits = hits[:req.N]
	}
	call.Status = http.StatusOK
	call.Completion = fmt.Sprintf("%d results", len(hits))
	writeJSON(w, map[string]any{"query": req.Query, "results": hits})
}

// searchHits reads the results in the order the engine ranked them and puts
// each quoted passage under the result it came from.
func searchHits(blocks []respBlock) ([]searchHit, string) {
	var hits []searchHit
	at := map[string]int{}
	failure := ""
	for _, b := range blocks {
		if b.Type != "web_search_tool_result" {
			continue
		}
		// A failed search's content is an error object instead of the list.
		var results []struct {
			URL     string `json:"url"`
			Title   string `json:"title"`
			PageAge string `json:"page_age"`
		}
		if json.Unmarshal(b.Content, &results) != nil {
			var e struct {
				ErrorCode string `json:"error_code"`
			}
			_ = json.Unmarshal(b.Content, &e)
			failure = e.ErrorCode
			continue
		}
		for _, h := range results {
			if _, seen := at[h.URL]; seen {
				continue
			}
			at[h.URL] = len(hits)
			hits = append(hits, searchHit{Title: h.Title, URL: h.URL, PageAge: h.PageAge})
		}
	}
	for _, b := range blocks {
		if b.Type != "text" {
			continue
		}
		for _, c := range b.Citations {
			i, ok := at[c.URL]
			if !ok || c.CitedText == "" || strings.Contains(hits[i].Snippet, c.CitedText) {
				continue
			}
			hits[i].Snippet = strings.TrimSpace(hits[i].Snippet + " " + c.CitedText)
		}
	}
	return hits, failure
}
