package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

// images.json is read here and by frontend/scripts/images.js, which generates
// the files, so the page cannot reference a width that was never built.
//
//go:embed images.json
var imagesJSON []byte

type imageSpec struct {
	Format        string         `json:"format"`
	Hero          string         `json:"hero"`
	CardWidths    []int          `json:"cardWidths"`
	LightboxWidth int            `json:"lightboxWidth"`
	HeroWidth     int            `json:"heroWidth"`
	Quality       map[string]int `json:"quality"`
	Avatar        struct {
		Width   int `json:"width"`
		Quality int `json:"quality"`
	} `json:"avatar"`
}

var images = loadImageSpec()

func loadImageSpec() imageSpec {
	var s imageSpec
	if err := json.Unmarshal(imagesJSON, &s); err != nil {
		slog.Error(fmt.Sprintf("parse images.json: %v", err))
		os.Exit(1)
	}
	if s.Format == "" || len(s.CardWidths) == 0 {
		slog.Error("images.json is missing format or cardWidths")
		os.Exit(1)
	}
	// Every width the templates can ask for needs a quality, since the
	// generator reads the same map and would skip it silently.
	for _, w := range append(append([]int{}, s.CardWidths...), s.LightboxWidth, s.HeroWidth) {
		if _, ok := s.Quality[strconv.Itoa(w)]; !ok {
			slog.Error(fmt.Sprintf("images.json: no quality for width %d", w))
			os.Exit(1)
		}
	}
	return s
}

func pourURL(number string, width int) string {
	return fmt.Sprintf("/static/images/art/acrylic-pours/%s-%d.%s", number, width, images.Format)
}

// pourSrcset builds the candidate list. template.Srcset keeps html/template
// from mangling the comma-separated descriptors.
func pourSrcset(number string, widths ...int) template.Srcset {
	parts := make([]string, 0, len(widths))
	for _, w := range widths {
		parts = append(parts, fmt.Sprintf("%s %dw", pourURL(number, w), w))
	}
	return template.Srcset(strings.Join(parts, ", "))
}

func avatarURL() string {
	return fmt.Sprintf("/static/images/avatar.%s", images.Format)
}
