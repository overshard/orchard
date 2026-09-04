package main

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"
)

// Looking like a browser is not about the User-Agent alone. A request carrying
// one browser header and nothing else is a more obvious scraper than one
// carrying none, because no real browser has ever sent that combination. So
// this sends the whole set Chrome sends, in Chrome's order, keeps cookies
// across requests the way a browser does, and asks for compression it can
// actually decode.
//
// Keep the version current. An outdated Chrome is itself a fingerprint, and
// this was found advertising Chrome 131 in September 2026, nearly a year stale.
const (
	chromeMajor = "152"
	browserUA   = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) " +
		"Chrome/" + chromeMajor + ".0.0.0 Safari/537.36"
	acceptHTML = "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8"
	clientHint = `"Chromium";v="` + chromeMajor + `", "Google Chrome";v="` + chromeMajor + `", "Not?A_Brand";v="24"`
)

// newHTTPClient carries a cookie jar. DuckDuckGo sets preference cookies on a
// first visit and a client that never returns them looks like a fresh stranger
// on every query, which is exactly the pattern rate limiting looks for.
func newHTTPClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{
		Timeout: 25 * time.Second,
		Jar:     jar,
	}
}

// browserHeaders sets what Chrome sends on a top-level navigation.
func browserHeaders(req *http.Request, referer string) {
	h := req.Header
	h.Set("sec-ch-ua", clientHint)
	h.Set("sec-ch-ua-mobile", "?0")
	h.Set("sec-ch-ua-platform", `"Windows"`)
	h.Set("Upgrade-Insecure-Requests", "1")
	h.Set("User-Agent", browserUA)
	h.Set("Accept", acceptHTML)
	h.Set("Sec-Fetch-Site", "none")
	h.Set("Sec-Fetch-Mode", "navigate")
	h.Set("Sec-Fetch-User", "?1")
	h.Set("Sec-Fetch-Dest", "document")
	// Only gzip and deflate are advertised because those are what this can
	// decode. Claiming br and zstd and then failing to read them is worse than
	// not claiming them.
	h.Set("Accept-Encoding", "gzip, deflate")
	h.Set("Accept-Language", "en-US,en;q=0.9")
	if referer != "" {
		h.Set("Referer", referer)
		h.Set("Sec-Fetch-Site", "same-origin")
	}
}

// readBody transparently decompresses. Setting Accept-Encoding by hand turns
// off Go's automatic gzip handling, so this has to do it.
func readBody(resp *http.Response, limit int64) ([]byte, error) {
	var r io.Reader = resp.Body
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Encoding")), "gzip") {
		zr, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		r = zr
	}
	return io.ReadAll(io.LimitReader(r, limit))
}
