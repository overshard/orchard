// A message as the site keeps it. The conversation store writes the parts beside
// the text as JSON without looking inside, and this is the side that knows them.
package main

import (
	"encoding/json"
	"time"

	"chat.bythewood.me/internal/history"
)

type Stored struct {
	Role    Role          `json:"role"`
	Content string        `json:"content"`
	Display string        `json:"display,omitempty"`
	Files   []Attachment  `json:"files,omitempty"`
	Tools   []ToolSummary `json:"tools,omitempty"`
	Sources []Source      `json:"sources,omitempty"`
	Widgets []Widget      `json:"widgets,omitempty"`
	Steps   []Step        `json:"steps,omitempty"`
	At      time.Time     `json:"at"`
}

// Shown is what the user typed, which is the whole message unless files were
// attached and their text was folded into it.
func (m Stored) Shown() string {
	if m.Display != "" {
		return m.Display
	}
	return m.Content
}

// ToolSummary is what the UI shows under a message: which tools ran and how
// they went, not their whole payload.
type ToolSummary struct {
	Name string `json:"name"`
	Args string `json:"args,omitempty"`
	MS   int64  `json:"ms"`
	OK   bool   `json:"ok"`
	Err  string `json:"err,omitempty"`
	// How old the data behind this call is, for a tool that reads a snapshot
	// and not the live thing. Shown on the chip, because an answer off a local
	// corpus looks exactly like one off the web otherwise.
	Age string `json:"age,omitempty"`
}

func appendStored(st *history.Store, convID string, m Stored) error {
	b, _ := json.Marshal(m.Tools)
	f, _ := json.Marshal(m.Files)
	src, _ := json.Marshal(m.Sources)
	wid, _ := json.Marshal(m.Widgets)
	stp, _ := json.Marshal(m.Steps)
	return st.Append(convID, history.Message{Role: string(m.Role), Content: m.Content, Display: m.Display,
		Files: f, Tools: b, Sources: src, Widgets: wid, Steps: stp})
}

func storedMessages(st *history.Store, convID string) ([]Stored, error) {
	rows, err := st.Messages(convID)
	if err != nil {
		return nil, err
	}
	var out []Stored
	for _, r := range rows {
		m := Stored{Role: Role(r.Role), Content: r.Content, Display: r.Display, At: r.At}
		_ = json.Unmarshal(r.Tools, &m.Tools)
		_ = json.Unmarshal(r.Files, &m.Files)
		_ = json.Unmarshal(r.Sources, &m.Sources)
		_ = json.Unmarshal(r.Widgets, &m.Widgets)
		_ = json.Unmarshal(r.Steps, &m.Steps)
		out = append(out, m)
	}
	return out, nil
}
