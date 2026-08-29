package main

import (
	"bytes"
	"encoding/xml"
	"net/http"
	"time"
)

// The Atom feed.
//
// Atom rather than RSS 2.0. Both are universally supported, and Atom has the
// tighter specification: RFC3339 dates instead of RSS's underspecified RFC822
// variants, required explicit entry ids, and an unambiguous content type.
//
// Each entry carries full content rather than a summary. There is no ad model
// that needs the click, and a reader with the whole article does not have to be
// online to finish it.

const feedPath = "/feed.xml"

type atomFeed struct {
	XMLName  xml.Name    `xml:"feed"`
	NS       string      `xml:"xmlns,attr"`
	Title    string      `xml:"title"`
	Subtitle string      `xml:"subtitle,omitempty"`
	ID       string      `xml:"id"`
	Updated  string      `xml:"updated"`
	Links    []atomLink  `xml:"link"`
	Author   atomAuthor  `xml:"author"`
	Entries  []atomEntry `xml:"entry"`
}

type atomLink struct {
	Rel  string `xml:"rel,attr,omitempty"`
	Type string `xml:"type,attr,omitempty"`
	Href string `xml:"href,attr"`
}

type atomAuthor struct {
	Name string `xml:"name"`
	URI  string `xml:"uri,omitempty"`
}

type atomEntry struct {
	Title      string         `xml:"title"`
	ID         string         `xml:"id"`
	Link       atomLink       `xml:"link"`
	Published  string         `xml:"published"`
	Updated    string         `xml:"updated"`
	Summary    string         `xml:"summary,omitempty"`
	Categories []atomCategory `xml:"category,omitempty"`
	Content    atomContent    `xml:"content"`
}

type atomCategory struct {
	Term string `xml:"term,attr"`
}

// Content is type="html", so the body is escaped into a text node rather than
// inlined as XHTML, and a post containing raw HTML cannot produce a malformed
// feed.
type atomContent struct {
	Type string `xml:"type,attr"`
	Body string `xml:",chardata"`
}

// feedTime turns a YYYY-MM-DD front matter date into the RFC3339 stamp Atom
// requires. Posts carry a day rather than a time, so they are dated at midnight
// UTC. A date that will not parse falls back to the epoch rather than failing
// the whole feed over one bad file.
func feedTime(day string) string {
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		return time.Unix(0, 0).UTC().Format(time.RFC3339)
	}
	return t.UTC().Format(time.RFC3339)
}

func (s *site) feed(w http.ResponseWriter, r *http.Request) {
	published, _, _ := s.lib.Published()

	feed := atomFeed{
		NS:       "http://www.w3.org/2005/Atom",
		Title:    siteName,
		Subtitle: "Writing about webdev, infrastructure, security, and tooling.",
		ID:       baseURL + "/",
		Author:   atomAuthor{Name: authorName, URI: baseURL + "/"},
		Links: []atomLink{
			{Rel: "self", Type: "application/atom+xml", Href: baseURL + feedPath},
			{Rel: "alternate", Type: "text/html", Href: baseURL + "/"},
		},
	}

	// Atom's feed-level updated is the newest entry's, and Published() is
	// already newest first. An empty blog gets now, because the element is
	// required.
	if len(published) > 0 {
		feed.Updated = feedTime(published[0].Date)
	} else {
		feed.Updated = time.Now().UTC().Format(time.RFC3339)
	}

	for _, post := range published {
		entry := atomEntry{
			Title:     post.Title,
			ID:        baseURL + post.URL(),
			Link:      atomLink{Rel: "alternate", Type: "text/html", Href: baseURL + post.URL()},
			Published: feedTime(post.PublishDate),
			Updated:   feedTime(post.Date),
			Summary:   post.Description,
			Content:   atomContent{Type: "html", Body: string(post.BodyHTML)},
		}
		for _, tag := range post.Tags {
			entry.Categories = append(entry.Categories, atomCategory{Term: tag})
		}
		feed.Entries = append(feed.Entries, entry)
	}

	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	encoder := xml.NewEncoder(&buf)
	encoder.Indent("", "  ")
	if err := encoder.Encode(feed); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = buf.WriteTo(w)
}

// redirectFeed points the names a reader is likely to try at the real one.
// Permanent, because the canonical path is not going to move.
func redirectFeed(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, feedPath, http.StatusMovedPermanently)
}
