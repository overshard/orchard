package tools

import (
	"sync"

	"chat.bythewood.me/property"
)

// A widget is the structured half of an answer. The model still writes prose
// about a ticker or a forecast, but the numbers read better as a chart and a
// model is not good at reciting fourteen figures without dropping one.
//
// What is recorded is the subject and not the data. The frontend fetches the
// readings from /api/widget/..., so a chart can change range without another
// turn and a week old conversation draws today's price.
type Widget struct {
	Kind string `json:"kind"`

	// ticker
	Symbol string `json:"symbol,omitempty"`

	// weather. Zip is what pollen.com is keyed on and Country decides whether
	// there is any pollen to ask for at all.
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

	// prompts. The one kind that carries its content rather than its subject,
	// because there is nothing to fetch: these are questions, they do not go
	// stale, and a conversation reopened next week should still offer them.
	Asks []property.Prompt `json:"asks,omitempty"`
}

// Key is what makes two widgets the same one. It lives here because the sink and
// the emitter both dedupe on it, and they had drifted into two spellings of it,
// which is how a second chart quietly stops being drawn.
func (w Widget) Key() string {
	return w.Kind + "\x00" + w.Symbol + "\x00" + w.Place + "\x00" + w.Label + "\x00" + w.Image
}

// Sink collects the widgets one turn produced. A turn runs its tools on
// goroutines, so this locks, and it is created per turn and hung on the Deps
// copy rather than on the shared one, for the same reason the session is.
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
