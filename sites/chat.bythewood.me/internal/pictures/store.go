// Package pictures keeps the pictures attached to a conversation, and the ones
// older conversations drew, so the page can show them by id.
package pictures

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"log/slog"
	"math"
	"sync"
	"time"

	_ "golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const schema = `
-- Pictures, which go with their conversation when it is deleted. The message
-- only carries the id, so a thread with a dozen of them still loads quickly.
CREATE TABLE IF NOT EXISTS images (
  id      TEXT PRIMARY KEY,
  conv_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  png     BLOB NOT NULL,
  width   INTEGER NOT NULL,
  height  INTEGER NOT NULL,
  at      INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS images_conv ON images(conv_id);
`

type Store struct {
	db    *sql.DB
	shelf *shelf
}

// New makes sure the picture table is in the site's database, beside the
// conversations its rows belong to.
func New(db *sql.DB) (*Store, error) {
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	return &Store{db: db, shelf: newShelf()}, nil
}

// NewID is a version 4 uuid, which every picture is served and stored under.
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// The only way this fails is a broken kernel, and carrying on with a
		// predictable id would be worse than saying so.
		panic("no randomness for a picture id: " + err.Error())
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// A megapixel is plenty for a picture shown beside a message, and a phone
// photo kept whole would be most of the database.
const keptPixels = 1024 * 1024

// AsPNG decodes any picture a browser is likely to hand over and gives it back
// as a PNG no bigger than is worth keeping.
func AsPNG(raw []byte) ([]byte, int, int, error) {
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("could not read the picture: %w", err)
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 16 || h < 16 {
		return nil, 0, 0, fmt.Errorf("the picture is too small to show")
	}
	if w*h > keptPixels {
		s := math.Sqrt(float64(keptPixels) / float64(w*h))
		w, h = int(float64(w)*s), int(float64(h)*s)
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
		img = dst
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, 0, 0, err
	}
	return out.Bytes(), w, h, nil
}

// Hold puts a picture he attached on the shelf, where the page can find it
// before the turn is stored.
func (p *Store) Hold(id string, png []byte, w, h int) {
	p.shelf.Put(id, png, w, h)
}

// Keep moves a turn's pictures off the shelf and into the conversation, once
// the conversation has an id to hang them on.
func (p *Store) Keep(convID string, ids []string) {
	for _, id := range ids {
		// Off the shelf only once it is in the database, since the page asks
		// for it at the same moment the turn ends and this runs.
		pic, ok := p.shelf.Take(id)
		if !ok {
			continue
		}
		if _, err := p.db.Exec(`INSERT INTO images(id, conv_id, png, width, height, at) VALUES(?,?,?,?,?,?)`,
			id, convID, pic.png, pic.w, pic.h, time.Now().Unix()); err != nil {
			slog.Error("keeping a picture", "err", err, "id", id)
			continue
		}
		p.shelf.Drop(id)
	}
}

func (p *Store) PNG(id string) ([]byte, bool) {
	if png, ok := p.shelf.Get(id); ok {
		return png, true
	}
	var png []byte
	if err := p.db.QueryRow(`SELECT png FROM images WHERE id = ?`, id).Scan(&png); err != nil {
		return nil, false
	}
	return png, true
}

// shelf holds pictures that are not in the database: a turn's own until the
// turn is stored, and an incognito one's for as long as it lasts here. Bounded,
// since incognito never reaches the step that would take them off.
type shelf struct {
	mu    sync.Mutex
	items map[string]shelved
}

type shelved struct {
	png  []byte
	w, h int
	at   time.Time
}

const (
	shelfSize = 24
	shelfLife = 2 * time.Hour
)

func newShelf() *shelf { return &shelf{items: map[string]shelved{}} }

func (s *shelf) Put(id string, png []byte, w, h int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, v := range s.items {
		if now.Sub(v.at) > shelfLife {
			delete(s.items, k)
		}
	}
	for len(s.items) >= shelfSize {
		oldest := ""
		for k, v := range s.items {
			if oldest == "" || v.at.Before(s.items[oldest].at) {
				oldest = k
			}
		}
		delete(s.items, oldest)
	}
	s.items[id] = shelved{png: png, w: w, h: h, at: now}
}

func (s *shelf) Get(id string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.items[id]
	return v.png, ok
}

// Take is Get with the size, and leaves the picture where it is.
func (s *shelf) Take(id string) (shelved, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.items[id]
	return v, ok
}

func (s *shelf) Drop(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, id)
}
