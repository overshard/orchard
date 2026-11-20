package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fixed map[string]string

func (f fixed) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	body, ok := f[r.URL.Host+r.URL.Path]
	if !ok {
		rec.WriteHeader(404)
	}
	_, _ = rec.WriteString(body)
	return rec.Result(), nil
}

func TestGroupInsightsFoldsRepeatedRules(t *testing.T) {
	in := "# blog\n\n## SEO insights\n\n" +
		"- [warning] [seo] Title length is 22 chars (recommended 30-60): `https://a/1`\n" +
		"- [warning] [seo] Title length is 4 chars (recommended 30-60): `https://a/2`\n" +
		"- [error] [links] Broken internal link (status 404): `https://a/3`\n" +
		"- [warning] [content] Thin content (271 words): `https://a/`\n\n## After\n\nkept\n"
	out := groupInsights(in)
	for _, want := range []string{
		"- [error] [links] Broken internal link (status N), on 1 page, like `https://a/3`",
		"- [warning] [seo] Title length is N chars (recommended N-N), on 2 pages, like `https://a/1`, `https://a/2`",
		"## After\n\nkept",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Index(out, "[error]") > strings.Index(out, "[warning]") {
		t.Errorf("errors should come first:\n%s", out)
	}
}

func TestDashBriefReadsTheState(t *testing.T) {
	rd := reader{session: "s", client: &http.Client{Transport: fixed{"orchard-dash:8000/api/state": `{
		"updated": "2026-11-19T23:00:00Z",
		"briefs": {"news": {"title": "CLOSE", "compiled": "19:01 EST", "stories": 28,
			"points": [{"text": "A thing happened.", "gist": "More on it."}],
			"read": {"label": "NORMAL DAY", "spread": "TOP STORY ON 7 OF 11", "text": "Quiet."}}},
		"advisories": {"rows": [{"id": "CVE-1", "product": "LINUX KERNEL", "title": "Bad", "tag": "EXPLOITED", "source": "KEV", "age": "22D AGO", "status": "CHECK"}], "checked": "17:09", "next_patch": "TUE DEC 8"},
		"systems": {"rows": [{"label": "Pond", "host": "pond.bythewood.me", "state": "down", "errors": 3, "response": "-"}, {"label": "Blog", "host": "blog", "state": "up"}], "up": 11, "total": 12},
		"feeds": [{"name": "YAHOO", "state": "ok"}, {"name": "HN", "state": "stale", "age": "2h"}]
	}`}}}
	out, err := dashBrief(context.Background(), rd, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## news brief, close at 19:01 EST", "- A thing happened. More on it.", "CHECK LINUX KERNEL CVE-1, EXPLOITED from KEV", "Pond (pond.bythewood.me): down", "HN stale 2h ago"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "Blog (blog)") {
		t.Errorf("a system that's up was listed:\n%s", out)
	}
	only, _ := dashBrief(context.Background(), rd, "advisories")
	if strings.Contains(only, "news brief") || !strings.Contains(only, "CVE-1") {
		t.Errorf("section advisories:\n%s", only)
	}
}
