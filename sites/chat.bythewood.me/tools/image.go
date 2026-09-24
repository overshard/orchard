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
	// Starting from pictures makes it a change to them rather than a new one.
	Draw(ctx context.Context, prompt, shape string, start []string, from *ImageProgress, report func(ImageProgress)) (Drawn, error)
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

var imageShapes = map[string]bool{"square": true, "portrait": true, "landscape": true, "same": true}

// KeepPrefix goes in front of every change to a picture, since the chat model
// cannot see it and a small model fills that gap with a description.
const KeepPrefix = "Keep everything from the reference picture that is not asked to change exactly as it is, " +
	"the same shape, colours, materials and details. "

var Image = Tool{
	Name: "image",
	Description: "Draw a picture with FLUX.2 klein, an image model that runs on the same card as you. " +
		"Call it whenever he asks for a picture, drawing, image, photo, illustration, logo, icon or wallpaper, " +
		"or attaches a picture, or asks for a change to the last one. " +
		"The prompt is a plain description of what should be in the picture, the subject first, then the setting, " +
		"the style, the light and the framing, in one or two sentences. Put any words that should appear in the " +
		"picture in quotes. " +
		"When he attached a picture it is always the starting point, and when he asks for a change to the last picture " +
		"set change_last. Either way the image model sees that picture and you do not, so the prompt says what to do " +
		"to it and what has to stay, like \"keep the sofa from the picture exactly as it is\". Never describe what is " +
		"in that picture, its colour, material, style or condition, unless he said it, because you would be guessing " +
		"and the image model draws your guess over the real thing. " +
		"It takes about half a minute and the picture is shown to him on its own, so do not describe it afterwards.",
	Schema: obj(map[string]any{
		"prompt": str("what to draw, or what to do to the picture it starts from, written out in full"),
		"shape": map[string]any{
			"type": "string",
			"enum": []string{"square", "portrait", "landscape", "same"},
			"description": "square unless the subject wants otherwise, portrait for a person or a phone wallpaper, " +
				"landscape for a scene, same to keep the shape of the picture it starts from",
		},
		"change_last": map[string]any{
			"type":        "boolean",
			"description": "true to change the last picture in this conversation rather than draw a new one",
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
		start := d.Start
		if len(start) == 0 && argBool(args, "change_last") && d.LastPicture != "" {
			start = []string{d.LastPicture}
		}
		shape := argStr(args, "shape")
		if !imageShapes[shape] || shape == "same" && len(start) == 0 {
			shape = "square"
			if len(start) > 0 {
				shape = "same"
			}
		}
		report := d.OnImage
		if report == nil {
			report = func(ImageProgress) {}
		}
		sent := prompt
		if len(start) > 0 {
			sent = KeepPrefix + prompt
		}
		got, err := d.Images.Draw(ctx, sent, shape, start, d.Picture, report)
		if err != nil {
			return nil, err
		}
		d.Widgets.Add(Widget{Kind: "image", Image: got.ID, Label: prompt, Model: got.Model,
			Width: got.Width, Height: got.Height})
		out := map[string]any{
			"drawn":         true,
			"shown_to_user": true,
			"prompt":        prompt,
			"size":          fmt.Sprintf("%dx%d", got.Width, got.Height),
			"seconds_taken": (got.LoadMS + got.DrawMS + 500) / 1000,
		}
		if len(start) > 0 {
			out["started_from"] = len(start)
		}
		return out, nil
	},
}
