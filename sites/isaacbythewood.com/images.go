package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"strconv"
	"strings"
)

// The image ladder, shared with the generator.
//
// images.json is the single source of truth and is read by two things that
// previously agreed only by hand: this file, which writes srcset attributes,
// and frontend/scripts/images.js, which produces the files. When those drifted
// the page referenced a width that was never generated.
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
		log.Fatalf("parse images.json: %v", err)
	}
	if s.Format == "" || len(s.CardWidths) == 0 {
		log.Fatal("images.json is missing format or cardWidths")
	}
	// Every width the templates can ask for must have a quality, because the
	// generator reads the same map and would otherwise skip it silently.
	for _, w := range append(append([]int{}, s.CardWidths...), s.LightboxWidth, s.HeroWidth) {
		if _, ok := s.Quality[strconv.Itoa(w)]; !ok {
			log.Fatalf("images.json: no quality for width %d", w)
		}
	}
	return s
}

// pourURL is the public path of one generated variant.
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
