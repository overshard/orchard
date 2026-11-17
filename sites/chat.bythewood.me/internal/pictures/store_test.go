package pictures

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

var onePixel, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==")

func testStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "chat.db")+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE conversations (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	p, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	return p, db
}

func TestAPictureIsKeptWithItsConversationAndDeletedWithIt(t *testing.T) {
	p, db := testStore(t)
	p.Hold("pic-1", onePixel, 1, 1)
	if _, ok := p.PNG("pic-1"); !ok {
		t.Fatal("a picture on the shelf could not be read back")
	}
	if _, err := db.Exec(`INSERT INTO conversations(id) VALUES('conv-1')`); err != nil {
		t.Fatal(err)
	}
	p.Keep("conv-1", []string{"pic-1"})
	if _, ok := p.shelf.Get("pic-1"); ok {
		t.Error("a kept picture is still on the shelf")
	}
	// A failed write leaves it where the page can still find it.
	p.Hold("pic-2", onePixel, 1, 1)
	p.Keep("no-such-conversation", []string{"pic-2"})
	if _, ok := p.PNG("pic-2"); !ok {
		t.Error("a picture that could not be stored was lost")
	}
	if got, ok := p.PNG("pic-1"); !ok || string(got) != string(onePixel) {
		t.Fatal("a kept picture could not be read back from the database")
	}
	if _, err := db.Exec(`DELETE FROM conversations WHERE id='conv-1'`); err != nil {
		t.Fatal(err)
	}
	if _, ok := p.PNG("pic-1"); ok {
		t.Error("the picture outlived its conversation")
	}
}

func TestTheShelfIsBounded(t *testing.T) {
	s := newShelf()
	for i := range shelfSize + 5 {
		s.Put(fmt.Sprint(i), onePixel, 1, 1)
	}
	if len(s.items) != shelfSize {
		t.Errorf("%d on the shelf, want %d", len(s.items), shelfSize)
	}
	if _, ok := s.Get("0"); ok {
		t.Error("the oldest picture was kept over a newer one")
	}
}

func TestABigPictureIsShrunkBeforeItIsKept(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 3000, 2000))
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	pic, w, h, err := AsPNG(b.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if w*h > keptPixels || w < 1200 || h < 800 {
		t.Errorf("scaled to %dx%d", w, h)
	}
	if !bytes.HasPrefix(pic, []byte("\x89PNG\r\n\x1a\n")) {
		t.Error("not a png")
	}
	if _, _, _, err := AsPNG(append([]byte("\x89PNG\r\n\x1a\n"), 0, 1)); err == nil {
		t.Error("a broken png was taken")
	}
}
