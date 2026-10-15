// The read only JSON view of every property, for chat.bythewood.me's tools. It
// reuses buildStatusPayload so it can't drift from /properties/{id}/status, and
// adds the property's identity since a list doesn't know which one it is.
package main

import (
	"net/http"
)

type apiProperty struct {
	ID        string        `json:"id"`
	URL       string        `json:"url"`
	Public    bool          `json:"public"`
	Status    statusPayload `json:"status"`
	Protected bool          `json:"protected"`
}

func (s *site) apiProperties(w http.ResponseWriter, r *http.Request) {
	props, err := listProperties(r.Context(), s.db, "")
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "unavailable"})
		return
	}
	out := make([]apiProperty, 0, len(props))
	for _, p := range props {
		out = append(out, apiProperty{
			ID:        p.ID.String(),
			URL:       p.URL,
			Public:    p.IsPublic,
			Protected: p.IsProtected,
			Status:    buildStatusPayload(p),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"properties": out})
}
