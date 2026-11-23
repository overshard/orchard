package main

import "testing"

func TestGroupFixesRanksByPages(t *testing.T) {
	var in []Insight
	for _, u := range []string{"/a", "/b", "/c"} {
		in = append(in, finding("https://x"+u, "1 form input(s) without associated label", typeA11y, sevWarn, "/search"))
	}
	in = append(in,
		finding("https://x/a", "Title length is 12 chars (recommended 30-60)", typeSEO, sevWarn, ""),
		finding("https://x/b", "Title length is 80 chars (recommended 30-60)", typeSEO, sevWarn, ""),
		finding("https://x/a", "Page has no h1", typeSEO, sevError, ""),
		finding("https://x/a", "Broken external link (404)", typeLinks, sevWarn, "https://gone"),
	)
	fixes := groupFixes(in)
	if len(fixes) != 4 {
		t.Fatalf("got %d kinds, want 4", len(fixes))
	}
	if fixes[0].Label != "Pages with no main heading" {
		t.Errorf("an error should rank first, got %q", fixes[0].Label)
	}
	if fixes[1].Label != "Form fields with no label" || fixes[1].Pages != 3 {
		t.Errorf("the widest warning should come next, got %q on %d pages", fixes[1].Label, fixes[1].Pages)
	}
	if fixes[2].Label != "Title outside 30 to 60 characters" || len(fixes[2].Items) != 2 {
		t.Errorf("two title lengths should be one kind, got %q with %d", fixes[2].Label, len(fixes[2].Items))
	}
}
