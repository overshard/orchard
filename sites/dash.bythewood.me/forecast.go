package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// The local NWS office's Area Forecast Discussion, the forecaster's own notes
// on what's coming and how sure they are, put in plain words for ATMOS.

const (
	// Blacksburg covers Yadkin County, from api.weather.gov/points.
	forecastOffice = "RNK"
	afdListURL     = "https://api.weather.gov/products/types/AFD/locations/" + forecastOffice
	afdPageURL     = "https://forecast.weather.gov/product.php?site=" + forecastOffice + "&issuedby=" + forecastOffice + "&product=AFD"

	// The sections worth reading are a few thousand characters, and the
	// aviation and fire weather ones after them aren't for this reader.
	afdRunes    = 5000
	forecastMax = 420
)

// afdKeep is the sections of a discussion about the weather people will have,
// by the start of their header. The rest are aviation, fire and the like.
var afdKeep = []string{"KEY MESSAGES", "SYNOPSIS", "DISCUSSION", "NEAR TERM", "SHORT TERM", "LONG TERM"}

type afdList struct {
	Graph []struct {
		ID           string `json:"id"`
		IssuanceTime string `json:"issuanceTime"`
	} `json:"@graph"`
}

type afdProduct struct {
	ID           string `json:"id"`
	IssuanceTime string `json:"issuanceTime"`
	ProductText  string `json:"productText"`
}

func fetchAFD(ctx context.Context, g *Guard) (afdProduct, error) {
	headers := map[string]string{"User-Agent": contactAgent, "Accept": "application/ld+json"}
	var list afdList
	if err := getJSONHeaders(ctx, g, "nws", afdListURL, headers, &list); err != nil {
		return afdProduct{}, err
	}
	if len(list.Graph) == 0 {
		return afdProduct{}, fmt.Errorf("no discussions")
	}
	var p afdProduct
	err := getJSONHeaders(ctx, g, "nws", "https://api.weather.gov/products/"+list.Graph[0].ID, headers, &p)
	return p, err
}

// afdText is the discussion's kept sections. Each starts with a line like
// ".NEAR TERM /THROUGH 6 AM FRIDAY/..." and ends at "&&".
func afdText(raw string) string {
	var b strings.Builder
	for _, sec := range strings.Split(raw, "&&") {
		sec = strings.TrimSpace(sec)
		i := strings.Index(sec, "\n.")
		if !strings.HasPrefix(sec, ".") && i >= 0 {
			sec = sec[i+1:]
		}
		head := strings.TrimPrefix(sec, ".")
		keep := false
		for _, k := range afdKeep {
			if strings.HasPrefix(head, k) {
				keep = true
				break
			}
		}
		if keep {
			b.WriteString(strings.Join(strings.Fields(sec), " ") + "\n\n")
		}
	}
	text := b.String()
	if r := []rune(text); len(r) > afdRunes {
		text = string(r[:afdRunes])
	}
	return strings.TrimSpace(text)
}

const forecastSystem = `You put a National Weather Service forecast discussion into plain words for one reader in the Yadkin Valley of North Carolina.
- "read" is two or three short sentences, at most 50 words in all. Say what the next few days look like, when anything changes, and how sure the forecasters are if they say.
- Plain words only, no jargon like ridge, trough, shortwave, MOS, CWA or cigs. Say warm, dry, rain, wind and the like.
- Use only the discussion given. Quote temperatures and dates only as it gives them.`

func (b *Briefer) compileForecast(ctx context.Context, slot briefSlot, at time.Time) (Brief, error) {
	afd, err := fetchAFD(ctx, b.guard)
	if err != nil {
		return Brief{}, err
	}
	issued := ""
	if t, err := time.Parse(time.RFC3339, afd.IssuanceTime); err == nil {
		issued = strings.ToUpper(t.In(easternTime()).Format("3:04pm Mon"))
	}
	brief := Brief{Kind: slot.kind, Title: slot.title(), Slot: at.Unix(), Basis: afd.ID, Stories: 1,
		Outlets: "NWS " + forecastOffice, Compiled: time.Now().In(easternTime()).Format("15:04 MST")}
	// The office writes one every six hours or so, and the same discussion
	// reads the same, so it's carried until a new one is out.
	if prev := b.store.Snapshot().Briefs.Forecast; prev.Basis == afd.ID && len(prev.Points) > 0 {
		brief.Points = prev.Points
		return brief, nil
	}
	text := afdText(afd.ProductText)
	if text == "" {
		return Brief{}, fmt.Errorf("discussion had no sections to read")
	}

	schema := map[string]any{
		"type":                 "object",
		"properties":           map[string]any{"read": map[string]any{"type": "string", "maxLength": forecastMax}},
		"required":             []string{"read"},
		"additionalProperties": false,
	}
	user := fmt.Sprintf("It is %s Eastern.\n\nForecast discussion:\n%s", at.In(easternTime()).Format("15:04 on Monday, January 2"), text)
	ask := user
	var read string
	for try := 0; try < 2 && read == ""; try++ {
		var out struct {
			Read string `json:"read"`
		}
		if err := b.model.Structured(ctx, forecastSystem, ask, schema, 250, &out); err != nil {
			return Brief{}, err
		}
		read = whole(out.Read, forecastMax)
		if miss := unsupported(read, text+"\nYadkin Valley, North Carolina"); len(miss) > 0 {
			slog.Info("brief forecast retried", slog.String("component", "brief"), slog.String("missing", strings.Join(miss, ", ")))
			ask = user + "\n\nYour last read had " + strings.Join(miss, ", ") + ", which the discussion doesn't say. Use only what it says."
			read = ""
		}
	}
	if read == "" {
		return Brief{}, fmt.Errorf("no forecast read")
	}
	label := "NWS " + forecastOffice
	if issued != "" {
		label += " " + issued
	}
	brief.Points = []Point{{Text: read, Links: []Link{{Source: label, URL: afdPageURL, Title: "Area Forecast Discussion"}}}}
	return brief, nil
}
