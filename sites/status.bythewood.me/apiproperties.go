// The read only JSON view of every property, for chat.bythewood.me's tools.
//
// It reuses buildStatusPayload rather than assembling its own shape, so what a
// tool reads and what /properties/{id}/status reports cannot drift apart. The
// only thing added is the identity of the property, since the per property
// endpoint already knows which one it is and a list does not.
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
