package tools

// The estate's own sites, read only. Each tool forwards the session of whoever is
// chatting and the site does its own check against auth.bythewood.me, so this
// chat can't read anything the person couldn't already open in a browser.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// SessionCookie is the name web/session.go uses, repeated here instead of
// imported because tools is a leaf package.
const SessionCookie = "bw_session"

// Site names resolve on the bridge so these never pass through Cloudflare, and
// they're vars so a test can stand a real server up in front of one.
var (
	loggingBase   = "http://orchard-logging:8000"
	statusBase    = "http://orchard-status:8000"
	analyticsBase = "http://orchard-analytics:8000"
	reposBase     = "http://orchard-repos:8000"
	dashBase      = "http://orchard-dash:8000"
)

// errEstateMissing is a 404 from one of the sites, which for a tool walking a
// repository is a path that isn't there, not a failure.
var errEstateMissing = fmt.Errorf("no such path")

// estateGet skips get() since the Guard is for third party endpoints that rate
// limit this address, and boxing a container on the bridge would take a whole
// site out over a blip.
func estateGet(ctx context.Context, d *Deps, rawURL string, into any) error {
	if d.Session == "" {
		return fmt.Errorf("this needs you to be signed in, and the turn carried no session")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: d.Session})
	req.Header.Set("Accept", "application/json")

	resp, err := d.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%s is not answering: %w", hostOf(rawURL), err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%s refused the session, so it may have been signed out", hostOf(rawURL))
	case resp.StatusCode == http.StatusSeeOther, resp.StatusCode == http.StatusFound:
		// A redirect here is the login page, which means the same thing as a
		// 401 and would otherwise be decoded as malformed JSON.
		return fmt.Errorf("%s wants a sign in", hostOf(rawURL))
	case resp.StatusCode == http.StatusNotFound:
		return errEstateMissing
	case resp.StatusCode >= 400:
		return fmt.Errorf("%s answered %d", hostOf(rawURL), resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(into)
}
