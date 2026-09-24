package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Images draws pictures. An interface for the same reason Memory is: the thing
// behind it is this site's own model client and database.
type Images interface {
	// Expect starts the record of a picture before the chat model has written
	// its prompt, so the page can show that stage too.
	Expect() ImageProgress
	// Draw carries on from what Expect started, or starts afresh from nil.
	Draw(ctx context.Context, prompt, shape string, from *ImageProgress, report func(ImageProgress)) (Drawn, error)
}

type Drawn struct {
	ID     string
	Model  string
	Width  int
	Height int
	// What each stage actually took, so the model and the page can say so.
	LoadMS int64
	DrawMS int64
}

// ImageProgress is one change in a picture being made. The whole state goes out
// each time rather than a delta, so a tab that attaches half way through only
// needs the last one it was sent.
type ImageProgress struct {
	ID     string `json:"id"`
	Stage  string `json:"stage"` // prompt, loading, drawing, done, failed
	Model  string `json:"model"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	// Unix milliseconds, so the page counts the seconds itself between events
	// rather than being sent a tick every second.
	StageAt  int64 `json:"stage_at"`
	PromptMS int64 `json:"prompt_ms,omitempty"`
	LoadMS   int64 `json:"load_ms,omitempty"`
	DrawMS   int64 `json:"draw_ms,omitempty"`
	// Guesses from how long the last few took on this card. The prompt one is
	// zero when the chat model decided on a picture by itself, since then there
	// was no stage before the call to show.
	PromptGuess int64  `json:"prompt_guess,omitempty"`
	LoadGuess   int64  `json:"load_guess"`
	DrawGuess   int64  `json:"draw_guess"`
	Err         string `json:"error,omitempty"`
}

var imageShapes = map[string]bool{"square": true, "portrait": true, "landscape": true}

var Image = Tool{
	Name: "image",
	Description: "Draw a picture with FLUX.2 klein, an image model that runs on the same card as you. " +
		"Call it whenever he asks for a picture, drawing, image, photo, illustration, logo, icon or wallpaper, " +
		"or asks for a change to the last one, in which case write the whole prompt again with the change in it. " +
		"The prompt is a plain description of what should be in the picture, the subject first, then the setting, " +
		"the style, the light and the framing, in one or two sentences. Put any words that should appear in the " +
		"picture in quotes. It takes about half a minute and the picture is shown to him on its own, " +
		"so do not describe it afterwards.",
	Schema: obj(map[string]any{
		"prompt": str("what to draw, written out in full"),
		"shape": map[string]any{
			"type":        "string",
			"enum":        []string{"square", "portrait", "landscape"},
			"description": "square unless the subject wants otherwise, portrait for a person or a phone wallpaper, landscape for a scene",
		},
	}, "prompt"),
	Run: func(ctx context.Context, d *Deps, args map[string]any) (any, error) {
		if d.Images == nil {
			return nil, errors.New("pictures are not set up on this server")
		}
		prompt := strings.TrimSpace(argStr(args, "prompt"))
		if prompt == "" {
			return nil, errors.New("say what to draw in prompt")
		}
		shape := argStr(args, "shape")
		if !imageShapes[shape] {
			shape = "square"
		}
		report := d.OnImage
		if report == nil {
			report = func(ImageProgress) {}
		}
		got, err := d.Images.Draw(ctx, prompt, shape, d.Picture, report)
		if err != nil {
			return nil, err
		}
		d.Widgets.Add(Widget{Kind: "image", Image: got.ID, Label: prompt, Model: got.Model,
			Width: got.Width, Height: got.Height})
		return map[string]any{
			"drawn":         true,
			"shown_to_user": true,
			"prompt":        prompt,
			"size":          fmt.Sprintf("%dx%d", got.Width, got.Height),
			"seconds_taken": (got.LoadMS + got.DrawMS + 500) / 1000,
		}, nil
	},
}
