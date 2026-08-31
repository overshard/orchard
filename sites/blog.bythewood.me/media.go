package main

import (
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
)

// The blog ran on Wagtail until 2026, and its rendition URLs are still being
// requested by feed readers and search indexes years later. A rendition is
// /media/images/<stem>.<hash>.<filterspec>.<format>.webp, and the same picture
// now lives at /content/images/<stem>.webp under a tidier name, so the old URL
// can be resolved rather than 404'd.

// renditionToken matches the first component Wagtail appends: an eight
// character content hash, or a filter spec when the rendition was not hashed.
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
// caddyserver.com intact by stopping at the first component that is a Wagtail
// token rather than at the first dot.
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
// 404s rather than sending a reader to the wrong picture.
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

// target maps a Wagtail media URL onto the path that serves it now. The avatar
// had a UUID in its name and only ever had one subject, so the whole directory
// resolves to it. og_images were hashed with nothing recoverable in the name,
// so those have no answer and 404.
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
