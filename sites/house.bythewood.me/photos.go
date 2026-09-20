package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Listing photos are served from disk, never hotlinked. A grid of big photos
// opened twice a day from two phones would hammer somebody else's CDN, and a CDN
// that sees every request knows which houses are being looked at and how often.
//
// The first request for a photo fetches it once, writes it under the data volume
// and serves the copy. Every request after that is a file read.
type Photos struct {
	db     *sql.DB
	dir    string
	client *http.Client

	// One fetch per photo, even when eight cards in a grid ask at once.
	mu       sync.Mutex
	fetching map[string]*sync.WaitGroup
}

func NewPhotos(db *sql.DB, dataDir string) *Photos {
	dir := filepath.Join(dataDir, "photos")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		// Not fatal. Without a cache directory every photo falls through to its
		// original URL, which is worse and still works.
		dir = ""
	}
	return &Photos{
		db:       db,
		dir:      dir,
		client:   &http.Client{Timeout: 20 * time.Second},
		fetching: map[string]*sync.WaitGroup{},
	}
}

func cacheName(url string) string {
	sum := sha256.Sum256([]byte(url))
	name := hex.EncodeToString(sum[:])
	ext := ".jpg"
	if i := strings.LastIndex(url, "."); i > 0 && len(url)-i <= 5 {
		candidate := strings.ToLower(url[i:])
		if strings.ContainsAny(candidate, "?&") {
			candidate = candidate[:strings.IndexAny(candidate, "?&")]
		}
		switch candidate {
		case ".jpg", ".jpeg", ".png", ".webp", ".avif":
			ext = candidate
		}
	}
	return name + ext
}

var photoTypes = map[string]string{
	".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png",
	".webp": "image/webp", ".avif": "image/avif",
}

// Serve answers /photo/{id}/{idx}. The indirection is what lets the page cache a
// photo for a year: the upstream URL carries a signature that expires and this
// one does not.
func (p *Photos) Serve(w http.ResponseWriter, r *http.Request, listingID int64, idx int) {
	var url string
	err := p.db.QueryRowContext(r.Context(),
		`SELECT url FROM photos WHERE listing_id = ? AND idx = ?`, listingID, idx).Scan(&url)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if p.dir == "" {
		http.Redirect(w, r, url, http.StatusFound)
		return
	}

	name := cacheName(url)
	path := filepath.Join(p.dir, name)

	if _, err := os.Stat(path); err != nil {
		if err := p.fetch(r.Context(), url, path); err != nil {
			// Fall back to the original rather than showing a hole in the grid.
			http.Redirect(w, r, url, http.StatusFound)
			return
		}
		if _, err := p.db.ExecContext(r.Context(),
			`UPDATE photos SET local = ? WHERE listing_id = ? AND idx = ?`, name, listingID, idx); err != nil {
			// The file is there and serving it is what matters.
			_ = err
		}
	}

	if ct, ok := photoTypes[strings.ToLower(filepath.Ext(path))]; ok {
		w.Header().Set("Content-Type", ct)
	}
	// A listing photo at a given index does not change, and this URL is behind
	// the session, so it is a private cache for a long time.
	w.Header().Set("Cache-Control", "private, max-age=604800")
	http.ServeFile(w, r, path)
}

func (p *Photos) fetch(ctx context.Context, url, path string) error {
	p.mu.Lock()
	if wg, busy := p.fetching[path]; busy {
		p.mu.Unlock()
		wg.Wait()
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		return fmt.Errorf("photo fetch already failed")
	}
	wg := &sync.WaitGroup{}
	wg.Add(1)
	p.fetching[path] = wg
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()
		delete(p.fetching, path)
		p.mu.Unlock()
		wg.Done()
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", browserUA)

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("photo HTTP %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "image/") {
		return fmt.Errorf("photo content type %q", ct)
	}

	// Written to a temp name and renamed, so an interrupted fetch never leaves a
	// half-downloaded photo that looks cached.
	tmp := path + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	// 12MB, past any listing photo and short of a runaway response.
	if _, err := io.Copy(f, io.LimitReader(resp.Body, 12<<20)); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// Forget removes the cached image files for one listing. The rows go with the
// listing on the cascade, so without this the files stay on disk forever with
// nothing pointing at them. A file shared with another listing is left alone,
// since the cache name is the URL and two listings can carry the same photo.
func (p *Photos) Forget(ctx context.Context, listingID int64) {
	if p.dir == "" {
		return
	}

	rows, err := p.db.QueryContext(ctx, `SELECT url FROM photos WHERE listing_id = ?`, listingID)
	if err != nil {
		return
	}
	defer rows.Close()

	var urls []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return
		}
		urls = append(urls, u)
	}

	for _, u := range urls {
		var others int
		if err := p.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM photos WHERE url = ? AND listing_id != ?`, u, listingID).
			Scan(&others); err != nil || others > 0 {
			continue
		}
		_ = os.Remove(filepath.Join(p.dir, cacheName(u)))
	}
}
