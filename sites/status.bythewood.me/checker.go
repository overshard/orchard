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
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/http2"
)

// This probe measures what a first-time visitor pays: DNS, TCP, the TLS
// handshake and the wait for the first byte, each timed by hand. Do not swap it
// for a pooled http.Client; the phase chart exists because nothing is reused.

const (
	// A real Chrome UA: sites behave differently for something announcing
	// itself as a monitor.
	probeUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/102.0.5005.115 Safari/537.36 Status/2.0.0"
	probeTimeout = 10 * time.Second
	maxRedirects = 5
)

// PhaseTimings is one probe's breakdown. A nil phase never ran. TotalMS is wall
// clock for the first hop and is what goes into checks.response_ms.
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

	// A 200 from an edge cache says nothing about the origin.
	cacheStatus string
	age         *int64

	// originUnreachable is set when a cache answered and a direct probe of the
	// live origin could have left it; see classifyCache.
	originUnreachable bool
}

// hopResult is one request and response, with enough detail to follow a redirect.
type hopResult struct {
	statusCode  int64
	headers     map[string]string
	headersJSON string
	timings     PhaseTimings
}

// errPlainHTTP is returned for an http:// URL: the probe is HTTP/2 over TLS
// only. Property creation rejects http:// too, so this is the second fence.
var errPlainHTTP = errors.New("plain HTTP not supported; use https:// (HTTP/2 only)")

// atLeast1ms reports a sub-millisecond phase as 1 rather than 0. Probing a site
// that shares this machine goes over lo and truncates to zero, which reads on
// the chart as a phase that never happened.
func atLeast1ms(d time.Duration) int64 {
	ms := d.Milliseconds()
	if ms == 0 && d > 0 {
		return 1
	}
	return ms
}

func ptr(v int64) *int64 { return &v }

// parseHTTPURL parses a URL and insists it is absolute and http(s). url.Parse
// reads "example.com" as a relative path with no host and returns no error.
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

// looksLikeTLSError picks between 526 for a certificate problem and 408 for a
// generic timeout. Matching on error text, to avoid unwrapping driver types.
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
		// Record how long the failure took. An NXDOMAIN fails in milliseconds
		// and charting it as the full timeout would poison the average.
		outcome = &probeOutcome{
			statusCode:  code,
			headersJSON: "{}",
			timings:     PhaseTimings{TotalMS: time.Since(started).Milliseconds()},
		}
	}

	// The stored code is what the monitor concluded, not what the edge sent.
	// advanceAlertState looks for a second failure by re-reading status_code out
	// of checks, so a code that is only returned can never trigger a down.
	effective := outcome.statusCode
	if outcome.originUnreachable {
		effective = statusOriginStale
		slog.Info("origin unreachable behind cache",
			slog.String("component", "checker"),
			slog.String("url", p.URL),
			slog.String("cf_cache_status", outcome.cacheStatus),
			slog.Int64("age", derefAge(outcome.age)),
			slog.Int64("edge_status", outcome.statusCode),
		)
	}

	_, err = db.ExecContext(ctx,
		`INSERT INTO checks (property_id, status_code, response_ms, headers,
		                     dns_ms, tcp_ms, tls_ms, ttfb_ms,
		                     cf_cache_status, age, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID[:], effective, outcome.timings.TotalMS, outcome.headersJSON,
		outcome.timings.DNSMS, outcome.timings.TCPMS, outcome.timings.TLSMS,
		outcome.timings.TTFBMS, nullableString(outcome.cacheStatus), outcome.age,
		nowMS())
	if err != nil {
		return 0, fmt.Errorf("insert check: %w", err)
	}
	return effective, nil
}

// statusOriginStale is reported when a cache answered for an origin that has
// stopped. It is not a real HTTP code; nothing sent one.
const statusOriginStale = 523

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func derefAge(age *int64) int64 {
	if age == nil {
		return -1
	}
	return *age
}

// probeWithRedirects times the first hop in full, then follows up to
// maxRedirects 3xx hops for the final status code, since the alert machine keys
// on 200 and an apex-to-www property would otherwise sit at 301 forever.
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
			// Keep the 3xx actually observed: the site answered and pointed
			// somewhere broken.
			break
		}
		status = hop.statusCode
		headersJSON = hop.headersJSON
		headers = hop.headers
	}

	// Only pay for the extra request when the edge answered by itself, which is
	// the one case where this response says nothing about the origin.
	cacheStatus, age, cached := classifyCache(headers)
	unreachable := false
	if cached {
		alive, known := originAnswered(ctx, current)
		unreachable = known && !alive
	}
	return &probeOutcome{
		statusCode:        status,
		headersJSON:       headersJSON,
		timings:           first.timings,
		cacheStatus:       cacheStatus,
		age:               age,
		originUnreachable: unreachable,
	}, nil
}

// classifyCache reports whether the edge answered out of its own copy. A hit is
// not evidence either way about the origin, and Age cannot stand in for one: the
// Edge TTL comes from a Cache Rule the origin never sees, so Age climbs past the
// origin's own max-age on a site that is perfectly healthy.
func classifyCache(headers map[string]string) (status string, age *int64, cached bool) {
	status = headers["cf-cache-status"]

	if raw, ok := headers["age"]; ok {
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			age = &n
		}
	}

	switch strings.ToUpper(status) {
	case "HIT", "UPDATING", "STALE":
		return status, age, true
	}
	return status, age, false
}

// originAnswered asks the origin directly, with a query string nothing has
// cached and against the one path every site here serves no-store. It reports
// whether the origin answered and whether the question could be asked at all,
// so a property with no health endpoint gets no opinion rather than a permanent
// alarm.
func originAnswered(ctx context.Context, u *url.URL) (alive, known bool) {
	probe := *u
	probe.Path = "/healthz"
	probe.RawQuery = "cb=" + strconv.FormatInt(time.Now().UnixNano(), 36)

	hop, err := phasedHop(ctx, &probe)
	if err != nil {
		return false, true
	}
	switch hop.statusCode {
	case http.StatusOK:
		return true, true
	case http.StatusNotFound:
		return false, false
	}
	return false, true
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

	dnsStart := time.Now()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("dns lookup: %w", err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("no addresses for %s", host)
	}
	dnsMS := atLeast1ms(time.Since(dnsStart))

	tcpStart := time.Now()
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(addrs[0].IP.String(), port))
	if err != nil {
		return nil, fmt.Errorf("tcp connect: %w", err)
	}
	defer conn.Close()
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		// Nagle would fold the request write into the wait being measured.
		_ = tcpConn.SetNoDelay(true)
	}
	tcpMS := atLeast1ms(time.Since(tcpStart))

	if u.Scheme != "https" {
		return nil, errPlainHTTP
	}

	// ALPN is pinned to h2 alone, so a server that does not speak HTTP/2 fails
	// the handshake rather than being measured over HTTP/1.1.
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

// h2Request runs an HTTP/2 GET over an already-handshaked TLS connection, which
// http.Client cannot do without dialing itself. TTFB runs from the h2 SETTINGS
// exchange to the response HEADERS frame, so it includes protocol setup.
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

	// The body is never read: this measures time to first byte. Closing without
	// reading sends RST_STREAM, which is correct and cheap.
	defer resp.Body.Close()

	headers = make(map[string]string, len(resp.Header))
	for k, v := range resp.Header {
		if len(v) > 0 {
			headers[strings.ToLower(k)] = v[0]
		}
	}

	// encoding/json sorts map keys, so the stored blob is byte-stable across
	// probes rather than reshuffled by map iteration order.
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
// The state commits before the notification is sent, so a crash between the two
// loses an alert rather than repeating one forever.
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

	// Fire and forget: a slow notifier must not hold up the scheduler tick.
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
// dashboard tile so the alert quotes the number the operator will see.
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

// next3MinBoundary returns the next wall-clock time divisible by three minutes,
// so every property shares one cadence and two charts line up on one x-axis.
func next3MinBoundary() int64 {
	now := time.Now().UTC()
	aligned := now.Truncate(checkInterval)
	return aligned.Add(checkInterval).UnixMilli()
}
