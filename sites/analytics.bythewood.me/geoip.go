package main

import (
	"compress/gzip"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/oschwald/maxminddb-golang/v2"
)

// GeoIP resolves a visitor address to a country, region and city against DB-IP
// City Lite. It is safe for concurrent use, and a missing database disables
// enrichment rather than failing the boot.
type GeoIP struct {
	path string

	mu     sync.RWMutex
	reader *maxminddb.Reader
}

// GeoLookup is the enrichment written onto a session_start event.
type GeoLookup struct {
	Country string
	Region  string
	City    string
	Lat     float64
	Lon     float64
	HasLoc  bool
}

// cityRecord is the part of the GeoIP2-City schema this app uses; v2 of the
// reader ships no record structs. DB-IP Lite follows MaxMind's schema.
type cityRecord struct {
	Country struct {
		ISOCode string `maxminddb:"iso_code"`
	} `maxminddb:"country"`
	Subdivisions []struct {
		ISOCode string            `maxminddb:"iso_code"`
		Names   map[string]string `maxminddb:"names"`
	} `maxminddb:"subdivisions"`
	City struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"city"`
	Location struct {
		Latitude  float64 `maxminddb:"latitude"`
		Longitude float64 `maxminddb:"longitude"`
	} `maxminddb:"location"`
}

// LoadGeoIP opens the database if it is there, and returns a working but inert
// GeoIP if it is not.
func LoadGeoIP(path string) *GeoIP {
	g := &GeoIP{path: path}
	if r, err := maxminddb.Open(path); err == nil {
		g.reader = r
		slog.Info(fmt.Sprintf("geoip loaded from %s", path))
	} else {
		slog.Info(fmt.Sprintf("geoip unavailable at %s (%v); country enrichment is off until a refresh lands", path, err))
	}
	return g
}

// Reload swaps in a freshly downloaded database. The reader mmaps the file, so
// closing it while a lookup is in flight would unmap memory being read.
func (g *GeoIP) Reload() bool {
	r, err := maxminddb.Open(g.path)
	if err != nil {
		slog.Info(fmt.Sprintf("geoip reload: %v", err))
		return false
	}
	g.mu.Lock()
	old := g.reader
	g.reader = r
	g.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	slog.Info(fmt.Sprintf("geoip reloaded from %s", g.path))
	return true
}

// Lookup resolves one address. The read lock has to span the decode, which is
// what touches the mapped bytes.
func (g *GeoIP) Lookup(ip netip.Addr) (GeoLookup, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.reader == nil {
		return GeoLookup{}, false
	}

	var rec cityRecord
	res := g.reader.Lookup(ip)
	if !res.Found() {
		return GeoLookup{}, false
	}
	if err := res.Decode(&rec); err != nil {
		return GeoLookup{}, false
	}

	out := GeoLookup{
		Country: rec.Country.ISOCode,
		City:    rec.City.Names["en"],
	}
	if len(rec.Subdivisions) > 0 {
		sub := rec.Subdivisions[0]
		// The admin-1 topojson matches on the English name; the ISO code is
		// a fallback that will not join to a shape.
		if n := sub.Names["en"]; n != "" {
			out.Region = n
		} else {
			out.Region = sub.ISOCode
		}
	}
	if rec.Location.Latitude != 0 || rec.Location.Longitude != 0 {
		out.Lat = rec.Location.Latitude
		out.Lon = rec.Location.Longitude
		out.HasLoc = true
	}
	return out, true
}

// geoipMaxAge follows DB-IP, which publishes monthly.
const geoipMaxAge = 30 * 24 * time.Hour

// EnsureGeoIPDB downloads a fresh database when the local one is missing or
// stale, and reports whether a new file was written. It walks back two months
// because DB-IP publishes each month's file some hours into the first.
func EnsureGeoIPDB(dest string) (bool, error) {
	if info, err := os.Stat(dest); err == nil {
		if time.Since(info.ModTime()) < geoipMaxAge {
			return false, nil
		}
	}

	now := time.Now().UTC()
	var lastErr error
	for offset := 0; offset < 3; offset++ {
		target := now.AddDate(0, -offset, 0)
		url := fmt.Sprintf("https://download.db-ip.com/free/dbip-city-lite-%d-%02d.mmdb.gz",
			target.Year(), int(target.Month()))
		if err := downloadGeoIP(url, dest); err != nil {
			slog.Info(fmt.Sprintf("geoip download %d-%02d: %v", target.Year(), int(target.Month()), err))
			lastErr = err
			continue
		}
		slog.Info(fmt.Sprintf("geoip downloaded from %s", url))
		return true, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no candidate months")
	}
	return false, lastErr
}

// downloadGeoIP fetches, decompresses, validates and atomically installs the
// database. Validation happens before the rename, because a truncated file at
// dest would carry a fresh mtime and defeat the staleness check for a month.
func downloadGeoIP(url, dest string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %s", resp.Status)
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, gz); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}

	r, err := maxminddb.Open(tmp)
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("downloaded mmdb failed validation: %w", err)
	}
	_ = r.Close()

	return os.Rename(tmp, dest)
}
