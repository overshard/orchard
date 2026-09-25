// Pictures. A second model draws them, and it takes turns with the chat model
// on the one card, so most of the wait is one model coming off and the other
// going on. The page is told which of those is happening and how long each
// took the last few times, because a panel that only spins reads as broken.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"

	"chat.bythewood.me/tools"
)

// Past this a picture is not coming, and saying so beats a panel that counts
// forever.
const drawLimit = 4 * time.Minute

// Close to a megapixel each, which is what the model was trained around, and
// every side a multiple of 16 since it will not take anything else.
var shapes = map[string][2]int{
	"square":    {1024, 1024},
	"portrait":  {1080, 1920},
	"landscape": {1920, 1080},
}

type Painter struct {
	llm   *LLM
	model string // what llama-swap swaps on
	name  string // what the page calls it
	store *Store
	shelf *shelf
	poll  time.Duration
}

func NewPainter(llm *LLM, model, name string, store *Store) *Painter {
	return &Painter{llm: llm, model: model, name: name, store: store,
		shelf: newShelf(), poll: 400 * time.Millisecond}
}

// Expect is the picture before its prompt exists. The size is not known until
// the chat model picks a shape, so the guesses are for a square.
func (p *Painter) Expect() tools.ImageProgress {
	w, h := shapes["square"][0], shapes["square"][1]
	g := p.store.ImageGuess(p.name, w*h)
	return tools.ImageProgress{ID: newID(), Stage: "prompt", Model: p.name, Width: w, Height: h,
		StageAt: time.Now().UnixMilli(), PromptGuess: g.Prompt, LoadGuess: g.Load, DrawGuess: g.Draw}
}

func (p *Painter) Draw(ctx context.Context, prompt, shape string, startFrom []string, from *tools.ImageProgress, report func(tools.ImageProgress)) (tools.Drawn, error) {
	refs, err := p.references(startFrom)
	if err != nil {
		if from != nil {
			f := *from
			f.Stage, f.Err, f.StageAt = "failed", err.Error(), time.Now().UnixMilli()
			report(f)
		}
		return tools.Drawn{}, err
	}
	size, ok := shapes[shape]
	if !ok {
		size = shapes["square"]
	}
	w, h := size[0], size[1]
	if len(refs) > 0 {
		if shape == "same" || shape == "square" {
			// Square is what a model asks for when it has no reason to, and
			// the picture it starts from is the reason.
			w, h = refs[0].w, refs[0].h
		}
		w, h = sizeLike(w, h, editPixels)
	}
	// A picture it starts from is read in beside the one being drawn, so it
	// costs about what the same number of pixels drawn would.
	pixels := w * h
	for _, r := range refs {
		pixels += r.w * r.h
	}
	g := p.store.ImageGuess(p.name, pixels)
	pr := tools.ImageProgress{ID: newID(), Model: p.name}
	if from != nil {
		pr = *from
		pr.PromptMS = time.Now().UnixMilli() - from.StageAt
	}
	pr.Stage, pr.Width, pr.Height, pr.StageAt = "loading", w, h, time.Now().UnixMilli()
	pr.LoadGuess, pr.DrawGuess = g.Load, g.Draw
	report(pr)

	ctx, cancel := context.WithTimeout(ctx, drawLimit)
	defer cancel()
	start := time.Now()
	type result struct {
		png []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		if len(refs) > 0 {
			pngs := make([][]byte, len(refs))
			for i, r := range refs {
				pngs[i] = r.png
			}
			png, err := p.llm.Edit(ctx, p.model, prompt, w, h, pngs)
			done <- result{png, err}
			return
		}
		png, err := p.llm.Image(ctx, p.model, prompt, w, h)
		done <- result{png, err}
	}()

	// The request is one call that blocks for the whole wait, so which half of
	// it is running comes from watching llama-swap's process table beside it.
	loadMS := int64(-1)
	tick := time.NewTicker(p.poll)
	defer tick.Stop()
	for {
		select {
		case r := <-done:
			total := time.Since(start).Milliseconds()
			if r.err != nil {
				pr.Stage, pr.Err, pr.StageAt = "failed", p.explain(ctx, r.err, loadMS >= 0), time.Now().UnixMilli()
				report(pr)
				return tools.Drawn{}, errors.New(pr.Err)
			}
			id := pr.ID
			r.png = cropTo(r.png, w, h)
			p.shelf.Put(id, r.png, w, h)
			if loadMS < 0 {
				// Never saw it ready, so the split is unknown and a guess
				// built from it would be wrong in both halves.
				loadMS = 0
			} else if !IsIncognito(ctx) {
				p.store.SaveImageRun(p.name, pr.PromptMS, loadMS, total-loadMS, pixels)
			}
			pr.Stage, pr.LoadMS, pr.DrawMS, pr.StageAt = "done", loadMS, total-loadMS, time.Now().UnixMilli()
			report(pr)
			return tools.Drawn{ID: id, Model: p.name, Width: w, Height: h, LoadMS: loadMS, DrawMS: total - loadMS}, nil
		case <-tick.C:
			if loadMS >= 0 || !p.ready(ctx) {
				continue
			}
			loadMS = time.Since(start).Milliseconds()
			pr.Stage, pr.LoadMS, pr.StageAt = "drawing", loadMS, time.Now().UnixMilli()
			report(pr)
		}
	}
}

type reference struct {
	png  []byte
	w, h int
}

// At most this many, since each one read in makes the drawing slower and the
// model was trained on a handful.
const maxReferences = 4

func (p *Painter) references(ids []string) ([]reference, error) {
	if len(ids) > maxReferences {
		ids = ids[:maxReferences]
	}
	out := make([]reference, 0, len(ids))
	for _, id := range ids {
		png, ok := p.PNG(id)
		if !ok {
			return nil, fmt.Errorf("the picture to start from is gone")
		}
		cfg, err := pngSize(png)
		if err != nil {
			return nil, fmt.Errorf("the picture to start from is unreadable: %w", err)
		}
		out = append(out, reference{png: png, w: cfg.Width, h: cfg.Height})
	}
	return out, nil
}

func (p *Painter) ready(ctx context.Context) bool {
	models, _ := p.llm.Running(ctx)
	for _, m := range models {
		if m.Model == p.model && m.State == "ready" {
			return true
		}
	}
	return false
}

// explain turns a failure into something a reader can act on. Which stage it
// died in is most of that, since a model that never loaded and one that fell
// over drawing are different problems.
func (p *Painter) explain(ctx context.Context, err error, loaded bool) string {
	stage := "while loading the image model"
	if loaded {
		stage = "while drawing"
	}
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Sprintf("it gave up %s, after %d minutes with nothing back", stage, int(drawLimit.Minutes()))
	case errors.Is(ctx.Err(), context.Canceled):
		return "stopped " + stage
	}
	slog.Error("a picture failed", "err", err, "loaded", loaded)
	return fmt.Sprintf("it failed %s: %v", stage, err)
}

// Hold puts pictures he attached on the shelf, where the image tool and the
// page can both find them before the turn is stored.
func (p *Painter) Hold(parts []filePart) {
	for _, f := range parts {
		if f.Image != "" {
			p.shelf.Put(f.Image, f.Picture, f.Width, f.Height)
		}
	}
}

// Keep moves a turn's pictures off the shelf and into the conversation, once
// the conversation has an id to hang them on.
func (p *Painter) Keep(convID string, widgets []Widget, files []Attachment) {
	ids := pictureIDs(widgets, files)
	for _, id := range ids {
		// Off the shelf only once it is in the database, since the page asks
		// for it at the same moment the turn ends and this runs.
		pic, ok := p.shelf.Take(id)
		if !ok {
			continue
		}
		if err := p.store.SaveImage(id, convID, pic.png, pic.w, pic.h); err != nil {
			slog.Error("keeping a picture", "err", err, "id", id)
			continue
		}
		p.shelf.Drop(id)
	}
}

func pictureIDs(widgets []Widget, files []Attachment) []string {
	var ids []string
	for _, f := range files {
		if f.Image != "" {
			ids = append(ids, f.Image)
		}
	}
	for _, w := range widgets {
		if w.Kind == "image" && w.Image != "" {
			ids = append(ids, w.Image)
		}
	}
	return ids
}

func (p *Painter) PNG(id string) ([]byte, bool) {
	if png, ok := p.shelf.Get(id); ok {
		return png, true
	}
	return p.store.ImagePNG(id)
}

// image serves one picture. Ids are never reused, so it can be cached for good.
func (s *site) image(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	png, ok := s.painter.PNG(id)
	if !ok {
		http.Error(w, "no such picture", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("Content-Disposition", `inline; filename="`+id+`.png"`)
	_, _ = w.Write(png)
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

// What a first picture is told to expect before this card has timed any, off
// FLUX.2 klein 9B on a 3070 straight after the chat model.
const (
	firstPromptGuess = 10000
	firstLoadGuess   = 2000
	firstDrawGuess   = 26000
)

type guess struct{ Prompt, Load, Draw int64 }

// ImageGuess is how long the next picture will probably take, from the median
// of the last few by this model, with the drawing scaled to the size asked for.
func (s *Store) ImageGuess(model string, pixels int) guess {
	g := guess{firstPromptGuess, firstLoadGuess, firstDrawGuess * int64(pixels) / (1024 * 1024)}
	rows, err := s.db.Query(`SELECT prompt_ms, load_ms, draw_ms, pixels FROM image_runs WHERE model = ? ORDER BY at DESC LIMIT 9`, model)
	if err != nil {
		return g
	}
	defer rows.Close()
	var prompts, loads, perMP []float64
	for rows.Next() {
		var pr, l, d int64
		var px int
		if rows.Scan(&pr, &l, &d, &px) != nil || px <= 0 {
			continue
		}
		if pr > 0 {
			prompts = append(prompts, float64(pr))
		}
		loads = append(loads, float64(l))
		perMP = append(perMP, float64(d)*1e6/float64(px))
	}
	if len(prompts) > 0 {
		g.Prompt = int64(median(prompts))
	}
	if len(loads) > 0 {
		g.Load, g.Draw = int64(median(loads)), int64(median(perMP)*float64(pixels)/1e6)
	}
	return g
}

func median(v []float64) float64 {
	sort.Float64s(v)
	if len(v)%2 == 1 {
		return v[len(v)/2]
	}
	return (v[len(v)/2-1] + v[len(v)/2]) / 2
}

func (s *Store) SaveImageRun(model string, promptMS, loadMS, drawMS int64, pixels int) {
	if _, err := s.db.Exec(`INSERT INTO image_runs(at, prompt_ms, load_ms, draw_ms, pixels, model) VALUES(?,?,?,?,?,?)`,
		time.Now().UnixMilli(), promptMS, loadMS, drawMS, pixels, model); err != nil {
		slog.Warn("recording how long a picture took", "err", err)
		return
	}
	_, _ = s.db.Exec(`DELETE FROM image_runs WHERE at < (SELECT at FROM image_runs ORDER BY at DESC LIMIT 1 OFFSET 49)`)
}

func (s *Store) SaveImage(id, convID string, png []byte, w, h int) error {
	_, err := s.db.Exec(`INSERT INTO images(id, conv_id, png, width, height, at) VALUES(?,?,?,?,?,?)`,
		id, convID, png, w, h, time.Now().Unix())
	return err
}

func (s *Store) ImagePNG(id string) ([]byte, bool) {
	var png []byte
	if err := s.db.QueryRow(`SELECT png FROM images WHERE id = ?`, id).Scan(&png); err != nil {
		return nil, false
	}
	return png, true
}
