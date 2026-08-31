package main

import (
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
)

// The site ran on Next.js until 2026 and its image optimiser is still being
// asked for pictures that are still here, under a different extension and with
// the width baked into the name instead of the query string. The old URL was
// /_next/image?url=/static/images/art/acrylic-pours/000.webp&w=960, and that
// picture is now /static/images/art/acrylic-pours/000-960.avif.

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

// resolve maps an old optimiser request onto a file that exists now. It takes
// the smallest width at least as large as the one asked for, so the picture is
// never upscaled, and the largest available when the request wants more than
// anything here has.
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
