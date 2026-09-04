package main

import (
	neturl "net/url"
	"testing"
)

func neturlParse(s string) (*neturl.URL, error) { return neturl.Parse(s) }

func TestCalculator(t *testing.T) {
	ok := []struct {
		in   string
		want string
	}{
		{"30*27", "810"},
		{"what is 30 * 27", "810"},
		{"30 times 27?", "810"},
		{"1,234 + 766", "2,000"},
		{"2^10", "1,024"},
		{"(3+4)*5", "35"},
		{"10/4", "2.5"},
		{"what's 15% of 200", "30"},
		{"100 - 250", "-150"},
		{"7 % 3", "1"},
		{"2^3^2", "512"}, // right associative
		{"-5 + 3", "-2"},
		{"1000000*1000", "1,000,000,000"},
	}
	for _, c := range ok {
		got, hit := TryCalculate(c.in)
		if !hit {
			t.Errorf("%q: not recognised as arithmetic", c.in)
			continue
		}
		if got.Pretty != c.want {
			t.Errorf("%q = %s, want %s", c.in, got.Pretty, c.want)
		}
	}

	// The important half: things that must go to the web instead. A calculator
	// that guesses at these is worse than no calculator.
	no := []string{
		"what is the deepest river in the US",
		"how do i make a breakfast burrito",
		"5 star hotels in paris",
		"42",              // a bare number is not a question
		"go 1.24 release", // version numbers are not arithmetic
		"3 +",
		"(3+4",
		"10/0",
		"what is sqlite wal mode",
		"top 10 go projects 2026",
	}
	for _, q := range no {
		if got, hit := TryCalculate(q); hit {
			t.Errorf("%q was answered as arithmetic: %s", q, got.Pretty)
		}
	}
}

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
