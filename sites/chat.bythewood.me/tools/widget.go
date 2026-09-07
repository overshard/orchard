package tools

import "sync"

// A widget is the structured half of an answer. The model still writes prose
// about a ticker or a forecast, but the numbers behind it read far better as a
// chart than as a sentence, and the model is not good at either drawing one or
// at reciting fourteen figures without dropping one.
//
// What is recorded here is the subject and not the data. The frontend fetches
// the readings itself from /api/widget/..., so switching a chart from a day to
// a year does not need another turn, and reopening a week old conversation
// draws today's price rather than the one that was on screen when it was asked.
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
	key := w.Kind + "\x00" + w.Symbol + "\x00" + w.Place
	if s.seen[key] {
		return
	}
	s.seen[key] = true
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
