package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"analytics.bythewood.me/web"
	"github.com/google/uuid"
)

// The collector is the one cross-origin endpoint and the only one an anonymous
// stranger can write to.
const (
	// The only bound on a table that grows forever.
	maxCollectBody = 16 * 1024
	// Each distinct name becomes a dashboard card, so an oversized one is
	// rejected rather than truncated into a collision with a real event.
	maxEventNameLen = 200
	// The clamp on every stored string field; these columns are indexed and
	// rendered into breakdown tables.
	maxFieldLen = 2048
)

// collectRequest is the wire format the embed script sends.
type collectRequest struct {
	CollectorID string          `json:"collectorId"`
	Event       string          `json:"event"`
	Data        json.RawMessage `json:"data"`
}

// collect records one event, answering 204 with no body. It never says whether
// the event was filed as a bot, which would give a bot something to tune against.
func (s *site) collect(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxCollectBody))
	if err != nil || len(body) == 0 {
		corsStatus(w, r, http.StatusBadRequest)
		return
	}

	var req collectRequest
	if err := json.Unmarshal(body, &req); err != nil {
		corsStatus(w, r, http.StatusBadRequest)
		return
	}
	if req.CollectorID == "" || req.Event == "" {
		corsStatus(w, r, http.StatusBadRequest)
		return
	}
	if len([]rune(req.Event)) > maxEventNameLen {
		corsStatus(w, r, http.StatusBadRequest)
		return
	}
	propertyID, err := uuid.Parse(req.CollectorID)
	if err != nil {
		corsStatus(w, r, http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// An unknown id answers 404, the only signal that a snippet was pasted with
	// the wrong one.
	var found []byte
	switch err := s.db.QueryRowContext(ctx,
		"SELECT id FROM properties WHERE id = ?", propertyID[:]).Scan(&found); {
	case err == sql.ErrNoRows:
		corsStatus(w, r, http.StatusNotFound)
		return
	case err != nil:
		slog.Info(fmt.Sprintf("collect: property lookup: %v", err))
		corsStatus(w, r, http.StatusInternalServerError)
		return
	}

	data := map[string]any{}
	if len(req.Data) > 0 {
		// A non-object data field is ignored; the event is still worth recording.
		_ = json.Unmarshal(req.Data, &data)
	}

	if ref, ok := data["referrer"].(string); ok {
		data["referrer"] = normalizeReferrer(ref)
	}

	// Every later event in a session comes from the same address, so geo is
	// looked up once.
	if req.Event == "session_start" {
		s.enrichGeo(r, data)
	}

	uaString, _ := data["user_agent"].(string)
	if uaString == "" {
		uaString = r.Header.Get("User-Agent")
	}

	if uaString != "" {
		parsed := s.ua.Parse(uaString)
		putIfNotEmpty(data, "platform", parsed.Platform)
		putIfNotEmpty(data, "browser", parsed.Browser)
		putIfNotEmpty(data, "device", parsed.Device)

		if parsed.IsBot {
			data["is_bot"] = true
			putIfNotEmpty(data, "bot_name", parsed.BotName)
			s.insertBotEvent(ctx, propertyID, req.Event, uaString, parsed.BotName, data)
			corsStatus(w, r, http.StatusNoContent)
			return
		}
	}

	s.insertEvent(ctx, propertyID, req.Event, uaString, data)
	corsStatus(w, r, http.StatusNoContent)
}

// enrichGeo writes country, region, city and coordinates onto a session_start.
// web.ClientIP prefers CF-Connecting-IP; the last X-Forwarded-For entry behind
// the tunnel is cloudflared's own bridge address, and resolves without erroring.
func (s *site) enrichGeo(r *http.Request, data map[string]any) {
	addr, err := netip.ParseAddr(web.ClientIP(r))
	if err != nil || addr.IsLoopback() {
		return
	}
	geo, ok := s.geoip.Lookup(addr)
	if !ok {
		return
	}
	putIfNotEmpty(data, "country", geo.Country)
	putIfNotEmpty(data, "region", geo.Region)
	putIfNotEmpty(data, "city", geo.City)
	if geo.HasLoc {
		data["loc"] = []any{geo.Lat, geo.Lon}
	}
}

func putIfNotEmpty(m map[string]any, key, value string) {
	if value != "" {
		m[key] = value
	}
}

// normalizeReferrer reduces a referrer to a bare hostname without "www.", so
// the breakdown is one row per site rather than per URL.
func normalizeReferrer(ref string) string {
	host := ref
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+3:]
	}
	if i := strings.Index(host, "/"); i >= 0 {
		host = host[:i]
	}
	return strings.TrimPrefix(strings.ToLower(host), "www.")
}

// insertEvent writes a human event, lifting the hot fields out of the payload
// into typed columns and leaving whatever the site sent of its own in extra.
func (s *site) insertEvent(ctx context.Context, propertyID uuid.UUID, event, userAgent string, data map[string]any) {
	// Each take removes the key, so what is left in data is the caller's own
	// fields and goes to extra.
	userID := takeString(data, "user_id")
	url := takeString(data, "url")
	title := takeString(data, "title")
	referrer := takeString(data, "referrer")
	delete(data, "user_agent")
	platform := takeString(data, "platform")
	browser := takeString(data, "browser")
	device := takeString(data, "device")
	screenWidth := takeInt(data, "screen_width")
	screenHeight := takeInt(data, "screen_height")
	country := takeString(data, "country")
	region := takeString(data, "region")
	city := takeString(data, "city")
	lat, lon := takeLoc(data)
	utmSource := takeString(data, "utm_source")
	utmMedium := takeString(data, "utm_medium")
	utmCampaign := takeString(data, "utm_campaign")
	utmTerm := takeString(data, "utm_term")
	utmContent := takeString(data, "utm_content")
	// The wire field is time_on_page, the column time_on_page_ms. Renaming
	// either end breaks the other.
	timeOnPage := takeInt(data, "time_on_page")

	_, err := s.db.ExecContext(ctx, `INSERT INTO events (
	    property_id, event, created_at, user_id, url, title, referrer, user_agent,
	    platform, browser, device, screen_width, screen_height, country, region, city,
	    lat, lon, utm_source, utm_medium, utm_campaign, utm_term, utm_content,
	    time_on_page_ms, extra
	  ) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		propertyID[:], event, time.Now().UnixMilli(), userID, url, title, referrer,
		nullString(userAgent), platform, browser, device, screenWidth, screenHeight,
		country, region, city, lat, lon, utmSource, utmMedium, utmCampaign, utmTerm,
		utmContent, timeOnPage, encodeExtra(data))
	if err != nil {
		slog.Info(fmt.Sprintf("collect: insert event: %v", err))
	}
}

// insertBotEvent writes to the separate bot table, so no human aggregation has
// to remember an is_bot filter.
func (s *site) insertBotEvent(ctx context.Context, propertyID uuid.UUID, event, userAgent, botName string, data map[string]any) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO bot_events (
	    property_id, event, created_at, bot_name, url, user_agent, country, extra
	  ) VALUES (?,?,?,?,?,?,?,?)`,
		propertyID[:], event, time.Now().UnixMilli(), nullString(botName),
		nullString(stringField(data, "url")), nullString(userAgent),
		nullString(stringField(data, "country")), encodeExtra(data))
	if err != nil {
		slog.Info(fmt.Sprintf("collect: insert bot event: %v", err))
	}
}

// encodeExtra serialises whatever is left of the payload. SetEscapeHTML(false)
// because this is stored, not rendered; the default writes \u003c for a "<".
func encodeExtra(data map[string]any) string {
	if len(data) == 0 {
		return "{}"
	}
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(data); err != nil {
		return "{}"
	}
	return strings.TrimRight(sb.String(), "\n")
}

// takeString removes a key and returns it as a NULL-able clamped string. A
// non-string value is stringified rather than dropped.
func takeString(data map[string]any, key string) any {
	v, ok := data[key]
	if !ok {
		return nil
	}
	delete(data, key)

	var s string
	switch t := v.(type) {
	case string:
		s = t
	case nil:
		return nil
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return nil
		}
		s = string(b)
	}
	if s == "" {
		return nil
	}
	return clampRunes(s, maxFieldLen)
}

func takeInt(data map[string]any, key string) any {
	v, ok := data[key]
	if !ok {
		return nil
	}
	delete(data, key)
	// Every number out of encoding/json is a float64, integers included.
	if f, ok := v.(float64); ok {
		return int64(f)
	}
	return nil
}

// takeLoc splits the [lat, lon] pair the geo enrichment writes.
func takeLoc(data map[string]any) (any, any) {
	v, ok := data["loc"]
	if !ok {
		return nil, nil
	}
	delete(data, "loc")
	arr, ok := v.([]any)
	if !ok || len(arr) < 2 {
		return nil, nil
	}
	lat, latOK := arr[0].(float64)
	lon, lonOK := arr[1].(float64)
	if !latOK || !lonOK {
		return nil, nil
	}
	return lat, lon
}

func stringField(data map[string]any, key string) string {
	s, _ := data[key].(string)
	return s
}

// nullString stores an empty value as NULL, since every breakdown query filters
// on IS NOT NULL and an empty string would be a row labelled with nothing.
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// clampRunes truncates on character boundaries, not bytes, so a clamped string
// is never invalid UTF-8.
func clampRunes(s string, max int) string {
	if len([]rune(s)) <= max {
		return s
	}
	return string([]rune(s)[:max])
}

// collectOptions answers the CORS preflight.
func (s *site) collectOptions(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Allow", "OPTIONS, POST")
	h.Set("Access-Control-Allow-Methods", "OPTIONS, POST")

	reqHeaders := r.Header.Get("Access-Control-Request-Headers")
	if reqHeaders == "" {
		reqHeaders = "Content-Type"
	}
	h.Set("Access-Control-Allow-Headers", reqHeaders)
	h.Set("Access-Control-Allow-Origin", originOrWildcard(r))
	w.WriteHeader(http.StatusNoContent)
}

// corsStatus answers with the permissive origin header the collector needs. Any
// site may be tracked, so there is no allowlist; nothing is read back here and
// no credentials are accepted.
func corsStatus(w http.ResponseWriter, r *http.Request, status int) {
	w.Header().Set("Access-Control-Allow-Origin", originOrWildcard(r))
	w.WriteHeader(status)
}

func originOrWildcard(r *http.Request) string {
	if o := r.Header.Get("Origin"); o != "" {
		return o
	}
	return "*"
}

// collectorScript serves the embed script at a stable URL, resolving Vite's
// content hash per request because pasted snippets hardcode this path. The
// short cache is because the name is stable and the bytes are not.
func (s *site) collectorScript(w http.ResponseWriter, r *http.Request) {
	name := s.assets.Script("static_src/collector/index.js")
	if name == "" {
		slog.Info("collector entry missing from vite manifest")
		http.Error(w, "collector unavailable", http.StatusServiceUnavailable)
		return
	}

	f, err := s.dist.Open(strings.TrimPrefix(name, "/static/"))
	if err != nil {
		slog.Info(fmt.Sprintf("collector open %s: %v", name, err))
		http.Error(w, "collector unavailable", http.StatusServiceUnavailable)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300, must-revalidate")

	body, err := io.ReadAll(f)
	if err != nil {
		slog.Info(fmt.Sprintf("collector read %s: %v", name, err))
		http.Error(w, "collector unavailable", http.StatusServiceUnavailable)
		return
	}
	_, _ = w.Write(body)
}
