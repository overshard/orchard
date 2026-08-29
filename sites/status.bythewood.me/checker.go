package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/http2"
)

// The HTTP probe.
//
// This deliberately does not use http.Client. A pooled client measures a warm
// connection; this measures what a first-time visitor pays: DNS, then TCP, then
// the TLS handshake, then the wait for the first response byte. Those four
// numbers are the dashboard's phase chart, and they exist only because each
// phase is performed by hand and timed.
//
// **Do not "fix" this by hoisting a shared client into the app state.** Pooling
// would drop mean response time by roughly 70% and the number would stop
// meaning what the dashboard says it means. If response-time variance looks
// ugly, look at its cause (handshake jitter, ALPN, DNS, server-side state)
// rather than removing the handshake from the measurement.

const (
	// A real Chrome UA. Sites behave differently for something that announces
	// itself as a monitor, and the number wanted here is what a visitor gets.
	probeUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/102.0.5005.115 Safari/537.36 Status/2.0.0"
	probeTimeout = 10 * time.Second
	maxRedirects = 5
)

// PhaseTimings is one probe's breakdown. A nil phase did not run, because the
// probe failed before reaching it. TotalMS is wall-clock for the first hop and
// is what goes into checks.response_ms.
type PhaseTimings struct {
	DNSMS   *int64
	TCPMS   *int64
	TLSMS   *int64
	TTFBMS  *int64
	TotalMS int64
}

type probeOutcome struct {
	statusCode  int64
	headersJSON string
	timings     PhaseTimings
}

// hopResult is one request/response, with enough detail to decide whether to
// follow a redirect.
type hopResult struct {
	statusCode  int64
	headers     map[string]string
	headersJSON string
	timings     PhaseTimings
}

// errPlainHTTP is returned for an http:// URL. The probe is HTTP/2 only, and h2
// over plain TCP with prior knowledge is rare enough that supporting it would
// mean measuring something nobody serves. Property creation rejects http:// for
// the same reason, so this is a second fence.
var errPlainHTTP = errors.New("plain HTTP not supported; use https:// (HTTP/2 only)")

// atLeast1ms rounds a duration to whole milliseconds, reporting a sub-ms phase
// as 1 rather than 0.
//
// The Linux kernel routes traffic destined for the host's own public IP over
// lo, so probing a site that shares this machine takes 200-500 microseconds for
// the TCP and handshake phases, which truncates to zero. A 0 in the chart reads
// as "this phase did not happen" rather than "this phase was instant".
func atLeast1ms(d time.Duration) int64 {
	ms := d.Milliseconds()
	if ms == 0 && d > 0 {
		return 1
	}
	return ms
}

func ptr(v int64) *int64 { return &v }

// parseHTTPURL parses a URL and insists it is absolute and http(s).
//
// url.Parse accepts almost anything, including "example.com", which it reads
// as a relative path with no host. A property created from that string would
// probe forever and fail forever with a confusing error.
func parseHTTPURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("URL scheme is %q, want http or https", u.Scheme)
	}
	if u.Host == "" {
		return nil, errors.New("URL has no host")
	}
	return u, nil
}

// looksLikeTLSError decides between the two failure codes: 526 for an invalid
// certificate, which the dashboard renders as a certificate warning, and 408
// for a generic timeout. Matching on error text is unpleasant, but the
// alternative is unwrapping a chain of driver-specific types to pick which of
// two icons to draw.
func looksLikeTLSError(err error) bool {
	var certErr *tls.CertificateVerificationError
	if errors.As(err, &certErr) {
		return true
	}
	var recordErr tls.RecordHeaderError
	if errors.As(err, &recordErr) {
		return true
	}
	s := strings.ToLower(err.Error())
	for _, needle := range []string{"certificate", "tls", "handshake", "x509"} {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

// runCheck performs one probe and writes the row. It returns the status code
// the alert state machine should act on.
func runCheck(ctx context.Context, db *sql.DB, p *Property) (int64, error) {
	started := time.Now()

	outcome, err := probeWithRedirects(ctx, p.URL)
	if err != nil {
		code := int64(408)
		if looksLikeTLSError(err) {
			code = 526
		}
		// Record how long the failure actually took. An NXDOMAIN or a refused
		// connection fails in milliseconds, and charting either as the full
		// 10-second timeout would poison the response-time average.
		outcome = &probeOutcome{
			statusCode:  code,
			headersJSON: "{}",
			timings:     PhaseTimings{TotalMS: time.Since(started).Milliseconds()},
		}
	}

	_, err = db.ExecContext(ctx,
		`INSERT INTO checks (property_id, status_code, response_ms, headers,
		                     dns_ms, tcp_ms, tls_ms, ttfb_ms, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID[:], outcome.statusCode, outcome.timings.TotalMS, outcome.headersJSON,
		outcome.timings.DNSMS, outcome.timings.TCPMS, outcome.timings.TLSMS,
		outcome.timings.TTFBMS, nowMS())
	if err != nil {
		return 0, fmt.Errorf("insert check: %w", err)
	}
	return outcome.statusCode, nil
}

// probeWithRedirects times the first hop in full, then follows up to
// maxRedirects 3xx hops to find the final status code.
//
// Following matters because the alert state machine keys on 200: a property
// using an http-to-https or apex-to-www redirect would otherwise sit
// permanently "down" at 301. Phase timings describe the first hop only, which
// is the latency a fresh visitor pays before being sent elsewhere, and the only
// number that means anything when later hops live on other servers.
func probeWithRedirects(ctx context.Context, rawURL string) (*probeOutcome, error) {
	current, err := parseHTTPURL(rawURL)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	first, err := phasedHop(ctx, current)
	if err != nil {
		return nil, err
	}

	status := first.statusCode
	headersJSON := first.headersJSON
	headers := first.headers

	for hops := 0; isRedirect(status) && hops < maxRedirects; hops++ {
		loc := headers["location"]
		if loc == "" {
			break
		}
		next, err := current.Parse(loc)
		if err != nil {
			break
		}
		current = next
		hop, err := phasedHop(ctx, current)
		if err != nil {
			// A redirect into something unreachable keeps the last status
			// actually observed, which is the 3xx: the site answered and
			// pointed somewhere broken. The crawler's redirect chain check is
			// where that gets reported.
			break
		}
		status = hop.statusCode
		headersJSON = hop.headersJSON
		headers = hop.headers
	}

	return &probeOutcome{
		statusCode:  status,
		headersJSON: headersJSON,
		timings:     first.timings,
	}, nil
}

func isRedirect(code int64) bool {
	switch code {
	case 301, 302, 303, 307, 308:
		return true
	}
	return false
}

// phasedHop performs one request, timing each phase separately.
func phasedHop(ctx context.Context, u *url.URL) (*hopResult, error) {
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}

	totalStart := time.Now()

	// --- DNS ---
	dnsStart := time.Now()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("dns lookup: %w", err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("no addresses for %s", host)
	}
	dnsMS := atLeast1ms(time.Since(dnsStart))

	// --- TCP ---
	tcpStart := time.Now()
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(addrs[0].IP.String(), port))
	if err != nil {
		return nil, fmt.Errorf("tcp connect: %w", err)
	}
	defer conn.Close()
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		// Nagle batches small writes, which would fold the request write into
		// the measurement of the wait for the response.
		_ = tcpConn.SetNoDelay(true)
	}
	tcpMS := atLeast1ms(time.Since(tcpStart))

	if u.Scheme != "https" {
		return nil, errPlainHTTP
	}

	// --- TLS ---
	//
	// ALPN is pinned to h2 alone, so a server that does not speak HTTP/2 fails
	// the handshake rather than being measured over HTTP/1.1. That failure
	// maps to 526.
	tlsStart := time.Now()
	tlsConn := tls.Client(conn, &tls.Config{
		ServerName: host,
		NextProtos: []string{"h2"},
		MinVersion: tls.VersionTLS12,
	})
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return nil, fmt.Errorf("tls handshake: %w", err)
	}
	if proto := tlsConn.ConnectionState().NegotiatedProtocol; proto != "h2" {
		return nil, fmt.Errorf("tls handshake: server did not negotiate h2 (got %q)", proto)
	}
	tlsMS := atLeast1ms(time.Since(tlsStart))

	// --- request, and the wait for the first byte ---
	status, headers, headersJSON, ttfbMS, err := h2Request(ctx, tlsConn, u)
	if err != nil {
		return nil, err
	}

	return &hopResult{
		statusCode:  status,
		headers:     headers,
		headersJSON: headersJSON,
		timings: PhaseTimings{
			DNSMS:   ptr(dnsMS),
			TCPMS:   ptr(tcpMS),
			TLSMS:   ptr(tlsMS),
			TTFBMS:  ptr(ttfbMS),
			TotalMS: time.Since(totalStart).Milliseconds(),
		},
	}, nil
}

// h2Request runs an HTTP/2 GET over an already-established TLS connection.
//
// http2.Transport.NewClientConn is what makes this possible: the stdlib's own
// HTTP/2 support is reachable only through http.Client, which would do its own
// dialing and handshaking and defeat the point of the file. This takes a
// connection that has already been handshaked.
//
// TTFB is measured from the start of the h2 SETTINGS exchange to the arrival of
// the response HEADERS frame, so it includes protocol setup. That is what the
// chart labels it: everything between "secure connection ready" and "first
// server byte", or curl's time_starttransfer minus time_appconnect.
func h2Request(ctx context.Context, conn *tls.Conn, u *url.URL) (
	status int64, headers map[string]string, headersJSON string, ttfbMS int64, err error,
) {
	ttfbStart := time.Now()

	tr := &http2.Transport{}
	cc, err := tr.NewClientConn(conn)
	if err != nil {
		return 0, nil, "", 0, fmt.Errorf("h2 handshake: %w", err)
	}
	defer cc.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, nil, "", 0, fmt.Errorf("h2 request build: %w", err)
	}
	req.Header.Set("user-agent", probeUserAgent)
	req.Header.Set("accept", "*/*")

	resp, err := cc.RoundTrip(req)
	if err != nil {
		return 0, nil, "", 0, fmt.Errorf("h2 response: %w", err)
	}
	ttfbMS = atLeast1ms(time.Since(ttfbStart))

	// The body is never read. This measures time to first byte and only needs
	// the status line and headers, so draining a megabyte of HTML would add
	// latency to a number meant to exclude it. Closing without reading sends
	// RST_STREAM, which is correct and cheap.
	defer resp.Body.Close()

	headers = make(map[string]string, len(resp.Header))
	for k, v := range resp.Header {
		if len(v) > 0 {
			headers[strings.ToLower(k)] = v[0]
		}
	}

	// encoding/json sorts map keys, so the stored blob is byte-stable for a
	// given response rather than reshuffled by Go's random map iteration on
	// every probe, which makes two rows diffable.
	encoded, err := json.Marshal(headers)
	if err != nil {
		encoded = []byte("{}")
	}

	return int64(resp.StatusCode), headers, string(encoded), ttfbMS, nil
}

// processCheck runs a probe, stores it, then advances the alert state machine.
func processCheck(ctx context.Context, db *sql.DB, notifier *Notifier, p *Property) error {
	status, err := runCheck(ctx, db, p)
	if err != nil {
		return err
	}
	return advanceAlertState(ctx, db, notifier, p, status)
}

// advanceAlertState is the debounce that decides when to wake somebody up.
//
//	up   -> down: two consecutive non-200 checks
//	down -> up:   immediately, on any 200
//
// The asymmetry is the point. A single failed check is usually a blip, so going
// down needs corroboration; coming back does not, because a false "recovered"
// costs nothing and a delayed one is the alert that matters.
//
// The state is committed in a transaction *before* the notification is sent, so
// a crash between the two loses an alert rather than repeating one forever.
func advanceAlertState(ctx context.Context, db *sql.DB, notifier *Notifier, p *Property, statusCode int64) error {
	isUp := statusCode == 200

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var currentState string
	err = tx.QueryRowContext(ctx,
		"SELECT alert_state FROM properties WHERE id = ?", p.ID[:]).Scan(&currentState)
	if err == sql.ErrNoRows {
		// Deleted between the probe being scheduled and finishing.
		return nil
	}
	if err != nil {
		return err
	}

	transition := ""
	switch {
	case isUp && currentState == "down":
		transition = "recovery"
	case !isUp && currentState == "up":
		// The check just inserted is one of the two; look for a second.
		rows, err := tx.QueryContext(ctx,
			"SELECT status_code FROM checks WHERE property_id = ? ORDER BY created_at DESC LIMIT 2",
			p.ID[:])
		if err != nil {
			return err
		}
		var recent []int64
		for rows.Next() {
			var code int64
			if err := rows.Scan(&code); err != nil {
				rows.Close()
				return err
			}
			recent = append(recent, code)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(recent) >= 2 && recent[0] != 200 && recent[1] != 200 {
			transition = "down"
		}
	}

	if transition == "" {
		return tx.Commit()
	}

	newState := "down"
	if transition == "recovery" {
		newState = "up"
	}
	now := nowMS()
	if _, err := tx.ExecContext(ctx,
		"UPDATE properties SET alert_state = ?, last_alert_sent = ?, updated_at = ? WHERE id = ?",
		newState, now, now, p.ID[:]); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	avg, err := recentAvgResponseMS(ctx, db, p.ID)
	if err != nil {
		slog.Info(fmt.Sprintf("alert: average response time for %s: %v", p.URL, err))
	}

	// Fire and forget: a slow or unreachable notifier must not hold up the
	// scheduler tick that produced this transition.
	go notifier.Fire(transition, AlertContext{
		ID:            p.ID.String(),
		Name:          p.Name(),
		URL:           p.URL,
		CurrentStatus: statusCode,
		AvgResponseMS: avg,
	})
	return nil
}

// recentAvgResponseMS averages the most recent 31 checks, mirroring the
// dashboard's rolling-average tile so the alert quotes the number the operator
// will see when they open the page it links to.
func recentAvgResponseMS(ctx context.Context, db *sql.DB, id [16]byte) (int64, error) {
	var avg sql.NullFloat64
	err := db.QueryRowContext(ctx,
		`SELECT AVG(response_ms) FROM (
		   SELECT response_ms FROM checks WHERE property_id = ?
		   ORDER BY created_at DESC LIMIT 31
		 )`, id[:]).Scan(&avg)
	if err != nil || !avg.Valid {
		return 0, err
	}
	return int64(avg.Float64), nil
}

// next3MinBoundary returns the next wall-clock time divisible by three minutes.
//
// Aligning to a boundary rather than adding three minutes to "now" means every
// property is probed on the same cadence regardless of when it was created, so
// the response-time charts of two properties line up on one x-axis instead of
// drifting apart.
func next3MinBoundary() int64 {
	now := time.Now().UTC()
	aligned := now.Truncate(checkInterval)
	return aligned.Add(checkInterval).UnixMilli()
}
