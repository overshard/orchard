package tools

import (
	"sync"

	"chat.bythewood.me/property"
)

// Numbers read better as a chart than recited by a model. A widget records the
// subject and the frontend fetches the readings from /api/widget/..., so a chart
// changes range without another turn and an old conversation draws today's price.
type Widget struct {
	Kind string `json:"kind"`

	// ticker
	Symbol string `json:"symbol,omitempty"`

	// weather. pollen.com is keyed on Zip and Country decides whether there is
	// any pollen to ask for at all.
	Place   string  `json:"place,omitempty"`
	Lat     float64 `json:"lat,omitempty"`
	Lon     float64 `json:"lon,omitempty"`
	Zip     string  `json:"zip,omitempty"`
	Country string  `json:"country,omitempty"`

	Label string `json:"label,omitempty"`

	// image. The id of a stored picture and its size, so the page can hold the
	// space open before the bytes arrive.
	Image  string `json:"image,omitempty"`
	Model  string `json:"model,omitempty"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`

	// prompts. The one kind that carries its content instead of its subject,
	// since these are questions that don't go stale and a conversation reopened
	// next week should still offer them.
	Asks []property.Prompt `json:"asks,omitempty"`
}

// Key lives here because the sink and the emitter both dedupe on it, and two
// spellings of it quietly stop a second chart being drawn.
func (w Widget) Key() string {
	return w.Kind + "\x00" + w.Symbol + "\x00" + w.Place + "\x00" + w.Label + "\x00" + w.Image
}

// Sink collects the widgets one turn produced. A turn runs its tools on
// goroutines, so this locks, and each Turn has its own.
type Sink struct {
	mu   sync.Mutex
	list []Widget
	seen map[string]bool
}

func NewSink() *Sink { return &Sink{seen: map[string]bool{}} }

// Add ignores a repeat. A model that asks for VTI twice in one turn should not
// get two identical charts stacked up.
func (s *Sink) Add(w Widget) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen[w.Key()] {
		return
	}
	s.seen[w.Key()] = true
	s.list = append(s.list, w)
}

func (s *Sink) List() []Widget {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Widget(nil), s.list...)
}
