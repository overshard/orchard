package pictures

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"chat.bythewood.me/tools"
	_ "modernc.org/sqlite"
)

// onePixel is a real png, since the client refuses anything without the magic.
var onePixel, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==")

// convTable stands in for the conversations a picture is kept with, so a
// conversation going takes its pictures with it.
type convTable struct {
	db *sql.DB
	n  int
}

func (c *convTable) add() (string, error) {
	c.n++
	id := fmt.Sprint("conv-", c.n)
	_, err := c.db.Exec(`INSERT INTO conversations(id) VALUES(?)`, id)
	return id, err
}

func (c *convTable) remove(id string) error {
	_, err := c.db.Exec(`DELETE FROM conversations WHERE id=?`, id)
	return err
}

func testPainter(t *testing.T) (*Painter, *convTable) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "chat.db")+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE conversations (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	p, err := New(nil, "image", "klein", db)
	if err != nil {
		t.Fatal(err)
	}
	return p, &convTable{db: db}
}

func solidPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// slowCard takes a moment to load the model and a moment to draw, and says
// it is ready once the load is over, which is how the stages are told apart.
type slowCard struct {
	mu      sync.Mutex
	asked   time.Time
	loadFor time.Duration
	drawFor time.Duration
}

func (c *slowCard) Image(_ context.Context, _, _ string, w, h int) ([]byte, error) {
	c.mu.Lock()
	c.asked = time.Now()
	c.mu.Unlock()
	time.Sleep(c.loadFor + c.drawFor)
	return onePixel, nil
}

func (c *slowCard) Edit(ctx context.Context, model, prompt string, w, h int, _ [][]byte) ([]byte, error) {
	return c.Image(ctx, model, prompt, w, h)
}

func (c *slowCard) Ready(context.Context, string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.asked.IsZero() && time.Since(c.asked) > c.loadFor
}

// The first picture is told the defaults, each stage is timed, and the next
// picture is guessed from what this one took.
func TestAPictureIsTimedAndTheNextIsGuessedFromIt(t *testing.T) {
	p, _ := testPainter(t)
	p.llm = &slowCard{loadFor: 60 * time.Millisecond, drawFor: 60 * time.Millisecond}
	p.Poll = 10 * time.Millisecond

	first := p.Expect()
	if first.LoadGuess != firstLoadGuess || first.PromptGuess != firstPromptGuess {
		t.Errorf("first guesses = %+v, want the defaults before anything was timed", first)
	}
	time.Sleep(5 * time.Millisecond)
	var stages []tools.ImageProgress
	if _, err := p.Draw(context.Background(), "a red barn in fog", "landscape", nil, &first,
		func(pr tools.ImageProgress) { stages = append(stages, pr) }); err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, s := range stages {
		order = append(order, s.Stage)
	}
	if fmt.Sprint(order) != "[loading drawing done]" {
		t.Fatalf("stages = %v, want loading, drawing, done", order)
	}
	done := stages[len(stages)-1]
	if done.LoadMS < 40 || done.DrawMS < 40 || done.PromptMS <= 0 {
		t.Errorf("prompt %dms, load %dms and draw %dms", done.PromptMS, done.LoadMS, done.DrawMS)
	}
	if g := p.imageGuess("klein", 1920*1080); g.Load != done.LoadMS || g.Prompt != done.PromptMS {
		t.Errorf("next guess = %+v, want the %d and %d just measured", g, done.PromptMS, done.LoadMS)
	}
}

// A picture sits in memory until its turn is stored, then lives with its
// conversation and goes when that is deleted.
func TestAPictureIsKeptWithItsConversationAndDeletedWithIt(t *testing.T) {
	p, conversations := testPainter(t)
	p.shelf.Put("pic-1", onePixel, 1, 1)
	if _, ok := p.PNG("pic-1"); !ok {
		t.Fatal("a picture on the shelf could not be read back")
	}

	conv, err := conversations.add()
	if err != nil {
		t.Fatal(err)
	}
	p.Keep(conv, []string{"pic-1"})
	if _, ok := p.shelf.Get("pic-1"); ok {
		t.Error("a kept picture is still on the shelf")
	}
	// A failed write leaves it where the page can still find it.
	p.shelf.Put("pic-2", onePixel, 1, 1)
	p.Keep("no-such-conversation", []string{"pic-2"})
	if _, ok := p.PNG("pic-2"); !ok {
		t.Error("a picture that could not be stored was lost")
	}
	if got, ok := p.PNG("pic-1"); !ok || string(got) != string(onePixel) {
		t.Fatal("a kept picture could not be read back from the database")
	}
	if err := conversations.remove(conv); err != nil {
		t.Fatal(err)
	}
	if _, ok := p.PNG("pic-1"); ok {
		t.Error("the picture outlived its conversation")
	}
}

func TestTheShelfIsBounded(t *testing.T) {
	s := newShelf()
	for i := 0; i < shelfSize+5; i++ {
		s.Put(fmt.Sprint(i), onePixel, 1, 1)
	}
	if len(s.items) != shelfSize {
		t.Errorf("%d on the shelf, want %d", len(s.items), shelfSize)
	}
	if _, ok := s.Get("0"); ok {
		t.Error("the oldest picture was kept over a newer one")
	}
}

// Drawing scales with the size and loading does not.
func TestTheGuessIsTheMedianScaledToTheSize(t *testing.T) {
	p, _ := testPainter(t)
	for _, r := range [][3]int64{{9000, 5000, 10000}, {0, 7000, 12000}, {11000, 60000, 90000}} {
		p.saveImageRun("klein", r[0], r[1], r[2], 1024*1024)
	}
	g := p.imageGuess("klein", 1024*1024)
	if g.Load != 7000 || g.Draw != 12000 {
		t.Errorf("guess = %+v, want the medians 7000/12000 and not the outlier", g)
	}
	// A picture the model asked for by itself had no prompt stage to time.
	if g.Prompt != 10000 {
		t.Errorf("prompt guess = %d, want 10000 from the two that were timed", g.Prompt)
	}
	if half := p.imageGuess("klein", 512*1024).Draw; half != 6000 {
		t.Errorf("half the pixels guessed %d, want 6000", half)
	}
	// A different model's times say nothing about this one.
	if g := p.imageGuess("bigger", 1024*1024); g.Draw != firstDrawGuess || g.Load != firstLoadGuess {
		t.Errorf("another model guessed %+v, want the defaults", g)
	}
}

func TestAReferenceIsShrunkToWhatTheModelReads(t *testing.T) {
	pic, w, h, err := AsReference(solidPNG(t, 3000, 2000))
	if err != nil {
		t.Fatal(err)
	}
	if w*h > referencePixels || w < 1200 || h < 800 {
		t.Errorf("scaled to %dx%d", w, h)
	}
	if !bytes.HasPrefix(pic, []byte("\x89PNG\r\n\x1a\n")) {
		t.Error("not a png")
	}
	if _, _, _, err := AsReference(append([]byte("\x89PNG\r\n\x1a\n"), 0, 1)); err == nil {
		t.Error("a broken png was taken")
	}
}

func TestSizeLikeKeepsTheShape(t *testing.T) {
	for _, c := range []struct{ w, h, px, ww, wh int }{
		{1024, 1024, referencePixels, 1024, 1024},
		{1920, 767, referencePixels, 1616, 640},
		{1431, 732, editPixels, 2032, 1040},
		{800, 1200, editPixels, 1184, 1776},
		{5000, 100, referencePixels, 1776, 592},
	} {
		w, h := sizeLike(c.w, c.h, c.px)
		if w != c.ww || h != c.wh || w%16 != 0 || h%16 != 0 {
			t.Errorf("sizeLike(%d, %d) = %dx%d, want %dx%d", c.w, c.h, w, h, c.ww, c.wh)
		}
	}
}

// sd-server rounds 1080 up to 1088 on either side, and the picture kept is the one asked for.
func TestAPictureDrawnLargerIsCroppedToTheSizeAsked(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1920, 1088))
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	cfg, err := pngSize(cropTo(b.Bytes(), 1920, 1080))
	if err != nil || cfg.Width != 1920 || cfg.Height != 1080 {
		t.Errorf("cropped to %dx%d (%v), want 1920x1080", cfg.Width, cfg.Height, err)
	}
	tall := image.NewRGBA(image.Rect(0, 0, 1088, 1920))
	b.Reset()
	if err := png.Encode(&b, tall); err != nil {
		t.Fatal(err)
	}
	if cfg, _ := pngSize(cropTo(b.Bytes(), 1080, 1920)); cfg.Width != 1080 || cfg.Height != 1920 {
		t.Errorf("portrait cropped to %dx%d, want 1080x1920", cfg.Width, cfg.Height)
	}
	if small := cropTo(onePixel, 1920, 1080); !bytes.Equal(small, onePixel) {
		t.Error("a picture smaller than asked was changed")
	}
}
