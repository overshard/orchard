// The pictures he attaches, kept with the conversation and asked for by id.
package main

import (
	"net/http"

	"chat.bythewood.me/internal/pictures"
)

// holdPictures puts the pictures he attached on the shelf, where the page can
// find them before the turn is stored.
func holdPictures(p *pictures.Store, parts []filePart) {
	for _, f := range parts {
		if f.Image != "" {
			p.Hold(f.Image, f.Picture, f.Width, f.Height)
		}
	}
}

func pictureIDs(files []Attachment) []string {
	var ids []string
	for _, f := range files {
		if f.Image != "" {
			ids = append(ids, f.Image)
		}
	}
	return ids
}

// image serves one picture, attached or drawn back when pictures were made
// here. Ids are never reused, so it can be cached for good.
func (s *site) image(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	png, ok := s.pictures.PNG(id)
	if !ok {
		http.Error(w, "no such picture", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("Content-Disposition", `inline; filename="`+id+`.png"`)
	_, _ = w.Write(png)
}
