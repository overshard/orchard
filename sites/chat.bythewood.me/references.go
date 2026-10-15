// Pictures a turn starts from: one he attached, or the last one in the
// conversation. The chat model cannot see any of them, so it only ever writes
// what should change, and the image model is handed the picture itself.
package main

import "context"

// Starting points for this turn, carried on the context like incognito is,
// since the engine and the tools both need them and neither owns the turn.
type references struct {
	Attached []string
	Last     string
	// What he typed, without the note about the file wrapped round it.
	Message string
	// The newest picture he attached in this conversation, and what he said
	// with it, which is where a new scene starts from.
	Origin     string
	OriginSaid string
}

type referencesKey struct{}

func withReferences(ctx context.Context, r references) context.Context {
	return context.WithValue(ctx, referencesKey{}, r)
}

func referencesOf(ctx context.Context) references {
	r, _ := ctx.Value(referencesKey{}).(references)
	return r
}

// lastPicture is the newest picture in a stored conversation, drawn or
// attached, and "make it night" means that one.
func lastPicture(stored []Stored) string {
	for i := len(stored) - 1; i >= 0; i-- {
		m := stored[i]
		for j := len(m.Widgets) - 1; j >= 0; j-- {
			if m.Widgets[j].Kind == "image" && m.Widgets[j].Image != "" {
				return m.Widgets[j].Image
			}
		}
		for j := len(m.Files) - 1; j >= 0; j-- {
			if m.Files[j].Image != "" {
				return m.Files[j].Image
			}
		}
	}
	return ""
}

func lastAttached(stored []Stored) (string, string) {
	for i := len(stored) - 1; i >= 0; i-- {
		m := stored[i]
		for j := len(m.Files) - 1; j >= 0; j-- {
			if m.Files[j].Image != "" {
				return m.Files[j].Image, m.Display
			}
		}
	}
	return "", ""
}
