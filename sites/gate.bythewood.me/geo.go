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

// Geo answers the country and the network an address belongs to, from DB-IP's
// free lite databases. Either one missing just leaves its field empty.
type Geo struct {
	dir string

	mu      sync.RWMutex
	country *maxminddb.Reader
	asn     *maxminddb.Reader
	built   string
}

type GeoAnswer struct {
	Country string
	ASN     int
	Org     string
}

var geoFiles = map[string]string{
	"country": "dbip-country-lite",
	"asn":     "dbip-asn-lite",
}

func OpenGeo(dir string) *Geo {
	g := &Geo{dir: dir}
	g.reload()
	return g
}

func (g *Geo) path(kind string) string { return filepath.Join(g.dir, geoFiles[kind]+".mmdb") }

// reload swaps readers under the lock, since a lookup in flight reads the
// mapped file and closing it early would pull that out from under it.
func (g *Geo) reload() {
	c, cerr := maxminddb.Open(g.path("country"))
	a, aerr := maxminddb.Open(g.path("asn"))
	if cerr != nil || aerr != nil {
		slog.Info("geo databases not all there yet", slog.Any("country", cerr), slog.Any("asn", aerr))
	}
	g.mu.Lock()
	oldC, oldA := g.country, g.asn
	if cerr == nil {
		g.country = c
		g.built = time.Unix(int64(c.Metadata.BuildEpoch), 0).UTC().Format("2006-01")
	}
	if aerr == nil {
		g.asn = a
	}
	g.mu.Unlock()
	if cerr == nil && oldC != nil {
		_ = oldC.Close()
	}
	if aerr == nil && oldA != nil {
		_ = oldA.Close()
	}
}

func (g *Geo) Built() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.built
}

func (g *Geo) Lookup(ip netip.Addr) GeoAnswer {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var out GeoAnswer
	if g.country != nil {
		var rec struct {
			Country struct {
				ISOCode string `maxminddb:"iso_code"`
			} `maxminddb:"country"`
		}
		if res := g.country.Lookup(ip); res.Found() && res.Decode(&rec) == nil {
			out.Country = rec.Country.ISOCode
		}
	}
	if g.asn != nil {
		var rec struct {
			Number uint   `maxminddb:"autonomous_system_number"`
			Org    string `maxminddb:"autonomous_system_organization"`
		}
		if res := g.asn.Lookup(ip); res.Found() && res.Decode(&rec) == nil {
			out.ASN, out.Org = int(rec.Number), rec.Org
		}
	}
	return out
}

// refresh downloads either database when it's missing or a month old. DB-IP
// publishes each month's file some hours into the first, so it walks back.
func (g *Geo) refresh() {
	changed := false
	for kind, name := range geoFiles {
		dest := g.path(kind)
		if info, err := os.Stat(dest); err == nil && time.Since(info.ModTime()) < 30*24*time.Hour {
			continue
		}
		now := time.Now().UTC()
		for back := range 3 {
			m := now.AddDate(0, -back, 0)
			url := fmt.Sprintf("https://download.db-ip.com/free/%s-%d-%02d.mmdb.gz", name, m.Year(), int(m.Month()))
			if err := download(url, dest); err != nil {
				slog.Info("geo download", slog.String("db", kind), slog.String("month", m.Format("2006-01")), slog.Any("err", err))
				continue
			}
			slog.Info("geo downloaded", slog.String("db", kind), slog.String("month", m.Format("2006-01")))
			changed = true
			break
		}
	}
	if changed {
		g.reload()
	}
}

// download checks the file opens before renaming it in, since a truncated one
// with a fresh mtime would be trusted for a month.
func download(url, dest string) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %s", resp.Status)
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return err
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
	if _, err := io.Copy(f, io.LimitReader(gz, 512<<20)); err != nil {
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
		return fmt.Errorf("the file didn't open: %w", err)
	}
	_ = r.Close()
	return os.Rename(tmp, dest)
}
