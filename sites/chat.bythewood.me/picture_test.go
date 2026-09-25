package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"chat.bythewood.me/tools"
)

func TestWhatReadsAsAskingForAPicture(t *testing.T) {
	for _, c := range []struct {
		msg  string
		want bool
	}{
		{"draw a fox in the snow", true},
		{"Can you make me a picture of a red barn at dawn", true},
		{"generate an image of a lighthouse", true},
		{"please paint the blue ridge in autumn", true},
		{"make a logo for a coffee shop called Grounded", true},
		{"make a wallpaper for my phone, pine forest at night", true},
		{"what is a fox", false},
		{"make a list of what to pack for camping", false},
		{"what image format does the blog use?", false},
		{"remember that I like watercolour", false},
		{"how do I draw a fox?", false},
	} {
		if got := isPictureAsk(c.msg, nil); got != c.want {
			t.Errorf("isPictureAsk(%q) = %v, want %v", c.msg, got, c.want)
		}
	}
}

// A change to the last picture names no picture at all, so it only counts
// straight after one.
func TestAChangeCountsOnlyStraightAfterAPicture(t *testing.T) {
	after := []Message{
		{Role: RoleUser, Content: "draw a barn"},
		{Role: RoleAssistant, Content: drewPrefix + "a red barn"},
	}
	before := []Message{
		{Role: RoleUser, Content: "what is a barn"},
		{Role: RoleAssistant, Content: "A barn is a farm building."},
	}
	for _, msg := range []string{"make it night", "now in watercolour", "try again", "another one"} {
		if !isPictureAsk(msg, after) {
			t.Errorf("%q after a picture did not read as a change to it", msg)
		}
		if isPictureAsk(msg, before) {
			t.Errorf("%q with no picture before it read as asking for one", msg)
		}
	}
	if isPictureAsk("thanks, that's great", after) {
		t.Error("thanks read as a change to the picture")
	}
}

func TestAPictureNamesItsConversationFromThePrompt(t *testing.T) {
	got := pictureTitle(drewPrefix + "a red barn in fog, early morning, watercolour")
	if got != "A red barn in fog" {
		t.Errorf("title = %q", got)
	}
}

// onePixel is a real png, since the client refuses anything without the magic.
var onePixel, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==")

// fakeCard is the gateway with both models behind it. The chat model always
// asks for a picture, and the picture model takes a moment to load and then
// a moment to draw, which is what the stages are read off.
type fakeCard struct {
	mu        sync.Mutex
	offered   [][]string
	answers   int
	asked     time.Time
	loadFor   time.Duration
	drawFor   time.Duration
	failWith  string
	lastImage map[string]any
	hold      []filePart
	args      string
	history   []Message
}

func (f *fakeCard) server(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/running":
			f.mu.Lock()
			state := "starting"
			if !f.asked.IsZero() && time.Since(f.asked) > f.loadFor {
				state = "ready"
			}
			f.mu.Unlock()
			fmt.Fprintf(w, `{"loaded":true,"models":[{"model":"image","name":"klein","state":%q}]}`, state)
		case "/v1/images/generations", "/v1/images/edits":
			f.mu.Lock()
			f.asked = time.Now()
			if r.URL.Path == "/v1/images/edits" {
				_ = r.ParseMultipartForm(8 << 20)
				f.lastImage = map[string]any{"prompt": r.FormValue("prompt"), "size": r.FormValue("size"),
					"from": len(r.MultipartForm.File["image[]"])}
			} else {
				_ = json.NewDecoder(r.Body).Decode(&f.lastImage)
			}
			f.mu.Unlock()
			time.Sleep(f.loadFor + f.drawFor)
			if f.failWith != "" {
				w.WriteHeader(500)
				fmt.Fprintf(w, `{"error":{"message":%q}}`, f.failWith)
				return
			}
			fmt.Fprintf(w, `{"data":[{"b64_json":%q}]}`, base64.StdEncoding.EncodeToString(onePixel))
		case "/v1/chat/completions":
			var req struct {
				Tools []struct {
					Function struct {
						Name string `json:"name"`
					} `json:"function"`
				} `json:"tools"`
				MaxTokens int `json:"max_tokens"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(req.Tools) == 0 {
				// The warm up and the answer both arrive with no tools. Only
				// the answer asks for more than a token.
				if req.MaxTokens > 1 {
					f.answers++
				}
				fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"words"}}]}`)
				return
			}
			var names []string
			for _, tl := range req.Tools {
				names = append(names, tl.Function.Name)
			}
			f.offered = append(f.offered, names)
			// A real model takes a while to write the call, and the prompt
			// stage is timed in whole milliseconds.
			time.Sleep(5 * time.Millisecond)
			args := f.args
			if args == "" {
				args = `{"prompt":"a red barn in fog","shape":"landscape"}`
			}
			call, _ := json.Marshal(map[string]any{"role": "assistant", "content": "", "tool_calls": []any{
				map[string]any{"id": "c1", "type": "function", "function": map[string]any{"name": "image", "arguments": args}}}})
			fmt.Fprintf(w, `{"choices":[{"message":%s}]}`, call)
		default:
			http.NotFound(w, r)
		}
	}))
}

func (f *fakeCard) run(t *testing.T, store *Store) (Message, []tools.Widget, []tools.ImageProgress) {
	return f.runWith(t, store, context.Background(), "draw a red barn in the fog")
}

func (f *fakeCard) runWith(t *testing.T, store *Store, ctx context.Context, message string) (Message, []tools.Widget, []tools.ImageProgress) {
	t.Helper()
	srv := f.server(t)
	t.Cleanup(srv.Close)
	llm := NewLLM(srv.URL, "local", "k")
	e := NewEngine(llm, "test")
	e.Render = func(md string) string { return md }
	p := NewPainter(llm, "image", "klein", store)
	p.poll = 10 * time.Millisecond
	e.Deps().Images = p

	var mu sync.Mutex
	var stages []tools.ImageProgress
	p.Hold(f.hold)
	reply, _, _, widgets, _, err := e.Run(ctx, f.history, message,
		"", "", NewTrace(nil), func(ev Event) {
			if ev.Kind == "image" {
				mu.Lock()
				stages = append(stages, *ev.Image)
				mu.Unlock()
			}
		})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return reply, widgets, stages
}

func testStore(t *testing.T) *Store {
	t.Helper()
	st, err := OpenStore(filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// The whole point of ending the turn on the picture is that the chat model is
// not put back on the card to write a sentence about it.
func TestAPictureEndsTheTurnWithoutTheChatModelComingBack(t *testing.T) {
	f := &fakeCard{loadFor: 60 * time.Millisecond, drawFor: 60 * time.Millisecond}
	store := testStore(t)
	reply, widgets, stages := f.run(t, store)

	if f.answers != 0 {
		t.Errorf("the chat model was asked to write %d answers after the picture", f.answers)
	}
	if len(f.offered) == 0 || len(f.offered[0]) != 1 || f.offered[0][0] != "image" {
		t.Errorf("tools offered = %v, want image alone", f.offered)
	}
	if !strings.HasPrefix(reply.Content, drewPrefix) || !strings.Contains(reply.Content, "a red barn in fog") {
		t.Errorf("reply = %q", reply.Content)
	}
	if len(widgets) != 1 || widgets[0].Kind != "image" || widgets[0].Width != 1920 || widgets[0].Height != 1080 {
		t.Fatalf("widgets = %+v", widgets)
	}
	if f.lastImage["size"] != "1920x1080" || f.lastImage["model"] != "image" {
		t.Errorf("the gateway was asked for %v", f.lastImage)
	}

	var order []string
	for _, s := range stages {
		order = append(order, s.Stage)
	}
	if strings.Join(order, ",") != "prompt,loading,drawing,done" {
		t.Fatalf("stages = %v, want prompt, loading, drawing, done", order)
	}
	// One picture from the first event to the last, so the page keeps the one
	// panel it opened while the prompt was being written.
	for _, s := range stages {
		if s.ID != stages[0].ID {
			t.Fatalf("the picture changed id part way: %v", stages)
		}
	}
	done := stages[3]
	if done.LoadMS < 40 || done.DrawMS < 40 {
		t.Errorf("load %dms and draw %dms, both should be near 60", done.LoadMS, done.DrawMS)
	}
	if stages[0].LoadGuess != firstLoadGuess || stages[0].PromptGuess != firstPromptGuess {
		t.Errorf("first guesses = %+v, want the defaults before anything was timed", stages[0])
	}
	if done.PromptMS <= 0 {
		t.Errorf("the prompt stage was not timed: %+v", done)
	}
	// And the next one is guessed from this one.
	if g := store.ImageGuess("klein", 1920*1080); g.Load != done.LoadMS || g.Prompt != done.PromptMS {
		t.Errorf("next guess = %+v, want the %d and %d just measured", g, done.PromptMS, done.LoadMS)
	}
}

func TestAFailedPictureSaysWhichStageItDiedIn(t *testing.T) {
	f := &fakeCard{loadFor: 60 * time.Millisecond, drawFor: 30 * time.Millisecond,
		failWith: "CUDA error: out of memory"}
	reply, widgets, stages := f.run(t, testStore(t))

	if len(widgets) != 0 {
		t.Errorf("a failed picture left a widget: %+v", widgets)
	}
	last := stages[len(stages)-1]
	if last.Stage != "failed" || !strings.Contains(last.Err, "while drawing") || !strings.Contains(last.Err, "out of memory") {
		t.Errorf("last stage = %+v", last)
	}
	if !strings.Contains(reply.Content, "out of memory") {
		t.Errorf("reply = %q", reply.Content)
	}
	if f.answers != 0 {
		t.Error("the chat model came back to explain a failure the page already shows")
	}
}

// A picture sits in memory until its turn is stored, then lives with its
// conversation and goes when that is deleted.
func TestAPictureIsKeptWithItsConversationAndDeletedWithIt(t *testing.T) {
	store := testStore(t)
	p := NewPainter(nil, "image", "klein", store)
	p.shelf.Put("pic-1", onePixel, 1, 1)
	if _, ok := p.PNG("pic-1"); !ok {
		t.Fatal("a picture on the shelf could not be read back")
	}

	conv, err := store.NewConversation("")
	if err != nil {
		t.Fatal(err)
	}
	p.Keep(conv, []Widget{{Kind: "image", Image: "pic-1", Width: 1, Height: 1}}, nil)
	if _, ok := p.shelf.Get("pic-1"); ok {
		t.Error("a kept picture is still on the shelf")
	}
	// A failed write leaves it where the page can still find it.
	p.shelf.Put("pic-2", onePixel, 1, 1)
	p.Keep("no-such-conversation", []Widget{{Kind: "image", Image: "pic-2", Width: 1, Height: 1}}, nil)
	if _, ok := p.PNG("pic-2"); !ok {
		t.Error("a picture that could not be stored was lost")
	}
	if got, ok := p.PNG("pic-1"); !ok || string(got) != string(onePixel) {
		t.Fatal("a kept picture could not be read back from the database")
	}
	if err := store.Delete(conv); err != nil {
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
	store := testStore(t)
	for _, r := range [][3]int64{{9000, 5000, 10000}, {0, 7000, 12000}, {11000, 60000, 90000}} {
		store.SaveImageRun("klein", r[0], r[1], r[2], 1024*1024)
	}
	g := store.ImageGuess("klein", 1024*1024)
	if g.Load != 7000 || g.Draw != 12000 {
		t.Errorf("guess = %+v, want the medians 7000/12000 and not the outlier", g)
	}
	// A picture the model asked for by itself had no prompt stage to time.
	if g.Prompt != 10000 {
		t.Errorf("prompt guess = %d, want 10000 from the two that were timed", g.Prompt)
	}
	if half := store.ImageGuess("klein", 512*1024).Draw; half != 6000 {
		t.Errorf("half the pixels guessed %d, want 6000", half)
	}
	// A different model's times say nothing about this one.
	if g := store.ImageGuess("bigger", 1024*1024); g.Draw != firstDrawGuess || g.Load != firstLoadGuess {
		t.Errorf("another model guessed %+v, want the defaults", g)
	}
}

// A picture he attached is the only thing that can be done anything with, so
// it becomes a picture turn whatever he typed, and the picture goes to the image
// model as an edit in its own shape.
func TestAnAttachedPictureIsWhatTheImageModelStartsFrom(t *testing.T) {
	f := &fakeCard{}
	store := testStore(t)
	wide, _, _, err := asReference(solidPNG(t, 2000, 800))
	if err != nil {
		t.Fatal(err)
	}
	parts := []filePart{{Attachment: Attachment{Name: "sofa.png", Image: "att-1", Width: 1618, Height: 647}, Picture: wide}}
	ctx := withReferences(context.Background(), references{Attached: pictures(parts)})

	f.hold = parts
	ctx = withReferences(context.Background(), references{Attached: pictures(parts), Message: "put this sofa in a nice living room"})
	_, widgets, _ := f.runWith(t, store, ctx, composeTurn("put this sofa in a nice living room", parts))
	if len(f.offered) != 0 {
		t.Errorf("the chat model was asked to write the prompt: %v", f.offered)
	}
	if f.lastImage["from"] != 1 {
		t.Fatalf("the image model was not handed the picture: %v", f.lastImage)
	}
	if p, _ := f.lastImage["prompt"].(string); !strings.HasPrefix(p, "Keep the sofa from the picture exactly as it is") ||
		!strings.Contains(p, "Put this sofa in a nice living room.") {
		t.Errorf("prompt = %q", p)
	}
	if f.lastImage["size"] != "2288x912" {
		t.Errorf("size = %v, want the wide shape of the picture", f.lastImage["size"])
	}
	if len(widgets) != 1 || widgets[0].Kind != "image" {
		t.Errorf("widgets = %+v", widgets)
	}
}

func TestChangeLastStartsFromTheNewestPicture(t *testing.T) {
	stored := []Stored{
		{Role: RoleUser, Files: []Attachment{{Name: "a.png", Image: "old"}}},
		{Role: RoleAssistant, Widgets: []Widget{{Kind: "image", Image: "drawn"}}},
		{Role: RoleUser, Content: "make it night"},
	}
	if got := lastPicture(stored); got != "drawn" {
		t.Errorf("last picture = %q, want the drawn one", got)
	}
	if got := lastPicture(nil); got != "" {
		t.Errorf("last picture of nothing = %q", got)
	}
}

func TestAReferenceIsShrunkToWhatTheModelReads(t *testing.T) {
	pic, w, h, err := asReference(solidPNG(t, 3000, 2000))
	if err != nil {
		t.Fatal(err)
	}
	if w*h > referencePixels || w < 1200 || h < 800 {
		t.Errorf("scaled to %dx%d", w, h)
	}
	if !bytes.HasPrefix(pic, pngMagic) {
		t.Error("not a png")
	}
	if _, _, _, err := asReference(append([]byte("\x89PNG\r\n\x1a\n"), 0, 1)); err == nil {
		t.Error("a broken png was taken")
	}
}

func TestAnEditPromptKeepsWhatHeNamed(t *testing.T) {
	got := editPrompt("put this sofa in a nice upscale apartment overlooking a city --", "", true)
	if !strings.HasPrefix(got, "Keep the sofa from the picture exactly as it is") ||
		!strings.Contains(got, "Put this sofa in a nice upscale apartment overlooking a city.") {
		t.Errorf("got %q", got)
	}
	if got := editPrompt("change the background of this to a loft", "", true); !strings.HasPrefix(got, "Keep the subject") {
		t.Errorf("the background was kept as the thing: %q", got)
	}
	// A new scene later in the conversation keeps what the first message named.
	got = editPrompt("can we change the background, do a modern living room", "put this sofa in a nice room", true)
	if !strings.HasPrefix(got, "Keep the sofa from the picture") {
		t.Errorf("got %q", got)
	}
	if got := editPrompt("make it night", "", false); !strings.HasPrefix(got, "Keep everything in the picture") {
		t.Errorf("got %q", got)
	}
}

// A new background starts again from what he attached, since an edit of an
// edit coarsens the fabric, and anything smaller changes the last picture.
func TestANewSceneStartsFromTheAttachedPicture(t *testing.T) {
	stored := []Stored{
		{Role: RoleUser, Display: "put this sofa in a room", Files: []Attachment{{Name: "a.png", Image: "silo"}}},
		{Role: RoleAssistant, Widgets: []Widget{{Kind: "image", Image: "scene"}}},
	}
	if id, said := lastAttached(stored); id != "silo" || said != "put this sofa in a room" {
		t.Errorf("lastAttached = %q, %q", id, said)
	}
	if !sceneWord.MatchString("can we change the background of this it's weird") {
		t.Error("a new background did not read as a new scene")
	}
	if sceneWord.MatchString("make it night with the lamps on") {
		t.Error("a lighting change read as a new scene")
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

func TestAChangeStartsFromTheLastPictureAndAFreshGoDoesNot(t *testing.T) {
	after := []Message{
		{Role: RoleUser, Content: "draw a barn"},
		{Role: RoleAssistant, Content: drewPrefix + "a red barn"},
	}
	for msg, want := range map[string]bool{
		"make it night":          true,
		"now in watercolour":     true,
		"add a dog on the porch": true,
		"can we change the background of this it's weird do a modern living room": true,
		"why did it change the colour?":                                           false,
		"try again":                                                               false,
		"another one":                                                             false,
		"draw a lighthouse instead":                                               false,
	} {
		if got := isPictureChange(msg, after); got != want {
			t.Errorf("isPictureChange(%q) = %v, want %v", msg, got, want)
		}
	}
	if isPictureChange("make it night", nil) {
		t.Error("a change with no picture before it started from nothing")
	}
}

// A message after a picture that reads as nothing in particular, where the
// model still reaches for the image tool, is a change it did not recognise.
func TestAPictureCallStraightAfterOneChangesIt(t *testing.T) {
	f := &fakeCard{
		hold: []filePart{{Attachment: Attachment{Image: "scene"}, Picture: solidPNG(t, 64, 32)}},
		history: []Message{
			{Role: RoleUser, Content: "put this sofa in a room"},
			{Role: RoleAssistant, Content: drewPrefix + "a sofa in a room"},
		},
		args: `{"prompt":"a sofa the model imagined","shape":"square"}`,
	}
	msg := "the base of the sofa looks muddy near the floor"
	ctx := withReferences(context.Background(), references{Last: "scene", Message: msg})
	f.runWith(t, testStore(t), ctx, msg)
	if f.lastImage["from"] != 1 {
		t.Fatalf("drew from nothing: %v", f.lastImage)
	}
	if p, _ := f.lastImage["prompt"].(string); strings.Contains(p, "imagined") || !strings.Contains(p, "muddy") {
		t.Errorf("prompt = %q, want his words and not the model's", p)
	}
	if f.lastImage["size"] == "1024x1024" {
		t.Error("came back square")
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
