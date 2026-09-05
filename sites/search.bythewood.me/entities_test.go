package main

import (
	neturl "net/url"
	"testing"
)

func neturlParse(s string) (*neturl.URL, error) { return neturl.Parse(s) }

func TestRankCandidates(t *testing.T) {
	links := []Link{
		{URL: "https://ollama.com/", Text: "Ollama"},
		{URL: "https://example.com/blog/best-tools", Text: "read more"},
		{URL: "https://github.com/ollama/ollama", Text: "the repo"},
		{URL: "https://facebook.com/share", Text: "Ollama"},
	}
	got := rankCandidates("Ollama", links)
	if len(got) == 0 {
		t.Fatal("no candidates for a name that is right there")
	}
	if got[0].URL != "https://ollama.com/" {
		t.Errorf("the name as the domain should win, got %s", got[0].URL)
	}
	for _, c := range got {
		if c.URL == "https://example.com/blog/best-tools" {
			t.Error("an unrelated link scored")
		}
	}
	if len(rankCandidates("Deepest River", links)) != 0 {
		t.Error("a name nothing links to should produce no candidates")
	}
}

func TestKeepLinkDropsJunk(t *testing.T) {
	base, _ := neturlParse("https://example.com/post")
	cases := []struct {
		href string
		text string
		keep bool
	}{
		{"https://ollama.com", "Ollama", true},
		{"https://example.com/other", "same site", false},
		{"https://facebook.com/sharer", "Share", false},
		{"https://ollama.com", "x", false},
		{"mailto:a@b.c", "mail", false},
	}
	for _, c := range cases {
		u, err := neturlParse(c.href)
		if err != nil {
			t.Fatal(err)
		}
		if got := keepLink(u, base, c.text); got != c.keep {
			t.Errorf("keepLink(%q, %q) = %v, want %v", c.href, c.text, got, c.keep)
		}
	}
}
