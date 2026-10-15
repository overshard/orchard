package tools

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"unicode"
)

type musicLookupArgs struct {
	Artist string
	Track  string
}

func (a *musicLookupArgs) params() []param {
	return []param{
		strArg(&a.Artist, "artist", "the artist").required(),
		strArg(&a.Track, "track", "the song title, optional"),
	}
}

var MusicLookup = Define(Tool{
	Name: "music_lookup",
	Description: "Check that a song or artist actually exists and get its album and year, from the " +
		"iTunes catalogue. Use it before listing songs you are not certain about, since an invented " +
		"track is the one mistake in a playlist a reader cannot see. " +
		"Only for a question that is already about music. It searches a music catalogue and cannot " +
		"say what an unfamiliar name refers to, so never reach for it to identify a person, a " +
		"channel or a company because the name sounds like a band.",
}, func(ctx context.Context, d *Turn, a musicLookupArgs) (any, error) {
	term := strings.TrimSpace(a.Artist + " " + a.Track)
	if term == "" {
		return nil, fmt.Errorf("artist is required")
	}
	track := a.Track
	if out, ok := artistSongs(ctx, d.Deps, a.Artist, track); ok {
		return map[string]any{"query": term, "matches": out}, nil
	}
	var res itunesResults
	u := "https://itunes.apple.com/search?media=music&entity=song&limit=5&term=" + url.QueryEscape(term)
	if err := getJSON(ctx, d.Deps, u, &res); err != nil {
		return nil, err
	}
	out := make([]songMatch, 0, len(res.Results))
	for _, r := range res.Results {
		out = append(out, r.match())
	}
	if len(out) == 0 {
		return map[string]any{"query": term, "matches": out,
			"note": "nothing in the catalogue matches, so treat this as not existing"}, nil
	}
	return map[string]any{"query": term, "matches": out}, nil
})

type itunesResults struct {
	Results []itunesItem `json:"results"`
}

type itunesItem struct {
	WrapperType    string `json:"wrapperType"`
	ArtistID       int64  `json:"artistId"`
	ArtistName     string `json:"artistName"`
	TrackName      string `json:"trackName"`
	CollectionName string `json:"collectionName"`
	ReleaseDate    string `json:"releaseDate"`
}

type songMatch struct {
	Artist string `json:"artist"`
	Track  string `json:"track"`
	Album  string `json:"album"`
	Year   string `json:"year"`
}

func (r itunesItem) match() songMatch {
	y := r.ReleaseDate
	if len(y) >= 4 {
		y = y[:4]
	}
	return songMatch{r.ArtistName, r.TrackName, r.CollectionName, y}
}

// artistSongs reads the artist's own catalogue instead of searching every song,
// where an original is buried under its covers and tribute albums. The earliest
// release comes first, since that's usually the original.
func artistSongs(ctx context.Context, d *Deps, artist, track string) ([]songMatch, bool) {
	if artist == "" || track == "" {
		return nil, false
	}
	var found itunesResults
	u := "https://itunes.apple.com/search?media=music&entity=musicArtist&limit=1&term=" + url.QueryEscape(artist)
	if getJSON(ctx, d, u, &found) != nil || len(found.Results) == 0 || found.Results[0].ArtistID == 0 {
		return nil, false
	}
	var songs itunesResults
	u = fmt.Sprintf("https://itunes.apple.com/lookup?entity=song&limit=200&id=%d", found.Results[0].ArtistID)
	if getJSON(ctx, d, u, &songs) != nil {
		return nil, false
	}
	want := foldTitle(track)
	var out []songMatch
	for _, r := range songs.Results {
		if r.WrapperType == "track" && strings.Contains(foldTitle(r.TrackName), want) {
			out = append(out, r.match())
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Year < out[j].Year })
	return out[:min(len(out), 6)], true
}

func foldTitle(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
