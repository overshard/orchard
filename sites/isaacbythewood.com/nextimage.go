package main

import (
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
)

// Search indexes still ask the old Next.js image optimiser for pictures that now
// have another extension and the width in the name, so a request for
// /_next/image?url=/static/images/x/000.webp&w=960 goes to /static/images/x/000-960.avif.

// nextImageIndex holds the widths available for each image base path.
type nextImageIndex struct {
	widths map[string][]int
	plain  map[string]string
}

func newNextImageIndex(dist fs.FS) *nextImageIndex {
	idx := &nextImageIndex{widths: map[string][]int{}, plain: map[string]string{}}

	_ = fs.WalkDir(dist, "images", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		ext := path.Ext(p)
		stem := strings.TrimSuffix(p, ext)
		if base, w, ok := splitWidth(stem); ok {
			idx.widths[base] = append(idx.widths[base], w)
			return nil
		}
		idx.plain[stem] = p
		return nil
	})
	for k := range idx.widths {
		sort.Ints(idx.widths[k])
	}
	return idx
}

// splitWidth pulls the trailing -<width> off a built name. A stem that ends in
// a number for its own reasons keeps it, since the part before the dash has to
// survive as a name.
func splitWidth(stem string) (base string, width int, ok bool) {
	i := strings.LastIndex(stem, "-")
	if i <= 0 {
		return "", 0, false
	}
	w, err := strconv.Atoi(stem[i+1:])
	if err != nil || w <= 0 {
		return "", 0, false
	}
	return stem[:i], w, true
}

// resolve maps an old optimiser request onto a file that exists. It takes the
// smallest width at least as large as the one asked for so nothing is upscaled,
// or the largest there is when the request wants more than that.
func (n *nextImageIndex) resolve(rawURL string, want int) (string, bool) {
	rest, ok := strings.CutPrefix(path.Clean(rawURL), "/static/")
	if !ok {
		return "", false
	}
	stem := strings.TrimSuffix(rest, path.Ext(rest))

	if widths := n.widths[stem]; len(widths) > 0 {
		pick := widths[len(widths)-1]
		for _, w := range widths {
			if w >= want {
				pick = w
				break
			}
		}
		return "/static/" + stem + "-" + strconv.Itoa(pick) + ".avif", true
	}
	if p, ok := n.plain[stem]; ok {
		return "/static/" + p, true
	}
	return "", false
}

func (s *site) nextImage(w http.ResponseWriter, r *http.Request) {
	want, _ := strconv.Atoi(r.URL.Query().Get("w"))
	target, ok := s.nextImages.resolve(r.URL.Query().Get("url"), want)
	if !ok {
		s.notFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.Redirect(w, r, target, http.StatusMovedPermanently)
}
