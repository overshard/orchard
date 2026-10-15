package main

import (
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
)

// Feed readers and search indexes still request Wagtail rendition URLs like
// /media/images/<stem>.<hash>.<filterspec>.<format>.webp, and the same picture
// lives at /content/images/<stem>.webp so the old URL can redirect to it.

// renditionToken matches the first component Wagtail appends: an eight
// character content hash, or a filter spec when the rendition wasn't hashed.
var renditionToken = regexp.MustCompile(`^([0-9a-f]{8}|(?:max|min|fill|width|height|scale|original|format)-.*)$`)

// wagtailAliases covers what no rule can derive. Wagtail truncated the stem to
// twenty characters, which prefix matching handles, but it truncated a
// misspelled original here and no amount of matching recovers the missing "l".
var wagtailAliases = map[string]string{
	"postgrseql-row-cou": "postgresql-row-count-output.webp",
}

// mediaIndex maps a normalized stem to the file that serves it now.
type mediaIndex struct {
	byStem map[string]string
	stems  []string
}

func newMediaIndex(images fs.FS) *mediaIndex {
	idx := &mediaIndex{byStem: make(map[string]string)}
	entries, err := fs.ReadDir(images, ".")
	if err != nil {
		return idx
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		stem := normalizeStem(strings.TrimSuffix(name, path.Ext(name)))
		idx.byStem[stem] = name
		idx.stems = append(idx.stems, stem)
	}
	return idx
}

// normalizeStem folds the two things Wagtail and the current tree disagree
// about, case and the word separator.
func normalizeStem(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "_", "-"))
}

// originalStem strips the rendition suffix, keeping dotted names like
// caddyserver.com intact by stopping at the first component that's a Wagtail
// token instead of at the first dot.
func originalStem(filename string) string {
	parts := strings.Split(filename, ".")
	for i, p := range parts {
		if i > 0 && renditionToken.MatchString(strings.ToLower(p)) {
			return strings.Join(parts[:i], ".")
		}
	}
	if len(parts) > 1 {
		return strings.Join(parts[:len(parts)-1], ".")
	}
	return filename
}

// resolve finds the current file for an old rendition name. A truncated stem
// resolves only when exactly one file starts with it, so an ambiguous prefix
// 404s instead of sending a reader to the wrong picture.
func (m *mediaIndex) resolve(filename string) (string, bool) {
	stem := normalizeStem(originalStem(filename))
	if stem == "" {
		return "", false
	}
	if name, ok := m.byStem[stem]; ok {
		return name, true
	}
	for prefix, name := range wagtailAliases {
		if strings.HasPrefix(stem, prefix) {
			return name, true
		}
	}
	var match string
	for _, s := range m.stems {
		if strings.HasPrefix(s, stem) {
			if match != "" {
				return "", false
			}
			match = m.byStem[s]
		}
	}
	return match, match != ""
}

// The avatar had a UUID in its name and only ever one subject, so the whole
// directory resolves to it. og_images were hashed with nothing recoverable in
// the name, so those 404.
func (m *mediaIndex) target(urlPath string) (string, bool) {
	dir, file := path.Split(strings.TrimPrefix(urlPath, "/media/"))

	switch strings.Trim(dir, "/") {
	case "avatar_images":
		return "/content/images/avatar.webp", true
	case "images":
		name, ok := m.resolve(file)
		if !ok {
			return "", false
		}
		return "/content/images/" + name, true
	}
	return "", false
}

func (s *site) media(w http.ResponseWriter, r *http.Request) {
	target, ok := s.mediaIdx.target(r.URL.Path)
	if !ok {
		s.notFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.Redirect(w, r, target, http.StatusMovedPermanently)
}

// ogPublished serves a card only for the site or a post that's out, since the
// cards are built for every post and a scheduled one's title isn't public yet.
func (s *site) ogPublished(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slug, ok := strings.CutSuffix(r.URL.Path, ".png")
		if !ok {
			slug, ok = strings.CutSuffix(r.URL.Path, ".svg")
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		if _, published := s.lib.Lookup(slug); !published && slug != ogSiteCard {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ogLegacy redirects the old SVG card URLs search indexes still request. It
// sits inside StripPrefix, so the path is the bare file name.
func ogLegacy(cards fs.FS, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if slug, ok := strings.CutSuffix(r.URL.Path, ".svg"); ok {
			if _, err := fs.Stat(cards, slug+".png"); err == nil {
				http.Redirect(w, r, "/og/"+slug+".png", http.StatusMovedPermanently)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
