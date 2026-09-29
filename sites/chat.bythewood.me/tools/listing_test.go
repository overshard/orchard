package tools

import (
	"strings"
	"testing"
)

func TestAnIndexPageIsReadAsRowsWithSizes(t *testing.T) {
	page := `<html><head><title>Index of /other/kiwix/zim/wikipedia/</title></head><body><pre>
<a href="../">../</a>
<a href="wikipedia_en_all_maxi_2026-08.zim">wikipedia_en_all_maxi_2026-08.zim</a>                  08-Sep-2026 18:38        127418087648
<a href="wikipedia_en_all_mini_2026-06.zim">wikipedia_en_all_mini_2026-06.zim</a>                  18-Jun-2026 13:38         12531679311
<a href="sub/">sub/</a>                                                                   01-Jan-2026 00:00                   -
</pre></body></html>`
	txt, ok := dirListing(page)
	if !ok {
		t.Fatal("not read as a listing")
	}
	for _, want := range []string{
		"wikipedia_en_all_maxi_2026-08.zim | 08-Sep-2026 18:38 | 127.4 GB (127418087648 bytes)",
		"wikipedia_en_all_mini_2026-06.zim | 18-Jun-2026 13:38 | 12.5 GB",
		"sub/ | 01-Jan-2026 00:00 | -",
	} {
		if !strings.Contains(txt, want) {
			t.Errorf("missing %q in\n%s", want, txt)
		}
	}
	if _, ok := dirListing("<title>News</title><p>hi</p>"); ok {
		t.Error("an ordinary page read as a listing")
	}
}
