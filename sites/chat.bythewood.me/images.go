// The pictures a turn hands the painter and the page asks for by id.
package main

import (
	"net/http"

	"chat.bythewood.me/internal/pictures"
)

// holdPictures puts the pictures he attached on the painter's shelf, where the
// image tool and the page can both find them before the turn is stored.
func holdPictures(p *pictures.Painter, parts []filePart) {
	for _, f := range parts {
		if f.Image != "" {
			p.Hold(f.Image, f.Picture, f.Width, f.Height)
		}
	}
}

func pictureIDs(widgets []Widget, files []Attachment) []string {
	var ids []string
	for _, f := range files {
		if f.Image != "" {
			ids = append(ids, f.Image)
		}
	}
	for _, w := range widgets {
		if w.Kind == "image" && w.Image != "" {
			ids = append(ids, w.Image)
		}
	}
	return ids
}

// image serves one picture. Ids are never reused, so it can be cached for good.
func (s *site) image(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	png, ok := s.painter.PNG(id)
	if !ok {
		http.Error(w, "no such picture", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("Content-Disposition", `inline; filename="`+id+`.png"`)
	_, _ = w.Write(png)
}
