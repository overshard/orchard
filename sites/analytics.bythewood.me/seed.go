package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"
)

// Dev tooling: fills a "Seed Test" property with plausible fake traffic, so the
// dashboard can be looked at without waiting for a real site to accumulate
// months of events.
//
// A flag on the site binary rather than a cmd/ of its own, because it needs the
// schema, the connection settings and the column list, and one copy of those is
// the point.

const seedPropertyName = "Seed Test"

type weightedURL struct {
	Path   string
	Title  string
	Weight int
}

var seedURLs = []weightedURL{
	{"/", "Home", 40},
	{"/about", "About", 10},
	{"/pricing", "Pricing", 8},
	{"/docs", "Documentation", 8},
	{"/blog", "Blog", 5},
	{"/blog/getting-started", "Getting Started", 5},
	{"/blog/whats-new-in-v2", "What's New in v2", 4},
	{"/blog/case-studies", "Case Studies", 3},
	{"/contact", "Contact", 4},
	{"/login", "Log In", 4},
	{"/signup", "Sign Up", 4},
	{"/dashboard", "Dashboard", 5},
}

type weightedString struct {
	Value  string
	Weight int
}

// The empty referrer is the heaviest entry, because most real traffic to a
// small site is direct or has its referrer stripped.
var seedReferrers = []weightedString{
	{"", 50}, {"google.com", 20}, {"twitter.com", 5}, {"news.ycombinator.com", 3},
	{"github.com", 3}, {"reddit.com", 4}, {"duckduckgo.com", 3}, {"bing.com", 3},
	{"linkedin.com", 2}, {"producthunt.com", 2}, {"dev.to", 2}, {"medium.com", 1},
}

type seedAgent struct {
	UA       string
	Platform string
	Browser  string
	Device   string
	IsBot    bool
	BotName  string
	Weight   int
}

var seedAgents = []seedAgent{
	{UA: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
		Platform: "Windows", Browser: "Chrome", Device: "Desktop", Weight: 25},
	{UA: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
		Platform: "Mac OS X", Browser: "Chrome", Device: "Desktop", Weight: 15},
	{UA: "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Mobile Safari/537.36",
		Platform: "Android", Browser: "Chrome Mobile", Device: "Mobile", Weight: 15},
	{UA: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15",
		Platform: "Mac OS X", Browser: "Safari", Device: "Desktop", Weight: 10},
	{UA: "Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1",
		Platform: "iOS", Browser: "Mobile Safari", Device: "Mobile", Weight: 15},
	{UA: "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:128.0) Gecko/20100101 Firefox/128.0",
		Platform: "Windows", Browser: "Firefox", Device: "Desktop", Weight: 5},
	{UA: "Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0",
		Platform: "Ubuntu", Browser: "Firefox", Device: "Desktop", Weight: 3},
	{UA: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0",
		Platform: "Windows", Browser: "Edge", Device: "Desktop", Weight: 8},
	{UA: "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
		IsBot: true, BotName: "Googlebot", Weight: 1},
	{UA: "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)",
		IsBot: true, BotName: "bingbot", Weight: 1},
	{UA: "facebookexternalhit/1.1 (+http://www.facebook.com/externalhit_uatext.php)",
		IsBot: true, BotName: "facebookexternalhit", Weight: 1},
}

type seedGeo struct {
	Country string
	Region  string
	City    string
	Lat     float64
	Lon     float64
	Weight  int
}

// Region names are the English forms the admin-1 topojson joins on, so the
// map's drill-down lights up rather than showing an empty country.
var seedGeos = []seedGeo{
	{"US", "New York", "New York", 40.7128, -74.0060, 15},
	{"US", "California", "Los Angeles", 34.0522, -118.2437, 10},
	{"US", "California", "San Francisco", 37.7749, -122.4194, 8},
	{"US", "Illinois", "Chicago", 41.8781, -87.6298, 5},
	{"US", "Texas", "Austin", 30.2672, -97.7431, 5},
	{"GB", "England", "London", 51.5074, -0.1278, 8},
	{"GB", "England", "Manchester", 53.4808, -2.2426, 2},
	{"DE", "Berlin", "Berlin", 52.5200, 13.4050, 5},
	{"DE", "Bavaria", "Munich", 48.1351, 11.5820, 3},
	{"FR", "Île-de-France", "Paris", 48.8566, 2.3522, 5},
	{"CA", "Ontario", "Toronto", 43.6532, -79.3832, 4},
	{"CA", "British Columbia", "Vancouver", 49.2827, -123.1207, 2},
	{"AU", "New South Wales", "Sydney", -33.8688, 151.2093, 3},
	{"AU", "Victoria", "Melbourne", -37.8136, 144.9631, 2},
	{"JP", "Tokyo", "Tokyo", 35.6762, 139.6503, 4},
	{"BR", "São Paulo", "São Paulo", -23.5505, -46.6333, 3},
	{"IN", "Maharashtra", "Mumbai", 19.0760, 72.8777, 3},
	{"IN", "Karnataka", "Bangalore", 12.9716, 77.5946, 3},
	{"NL", "North Holland", "Amsterdam", 52.3676, 4.9041, 3},
	{"ES", "Madrid", "Madrid", 40.4168, -3.7038, 2},
	{"IT", "Lazio", "Rome", 41.9028, 12.4964, 2},
	{"MX", "Mexico City", "Mexico City", 19.4326, -99.1332, 2},
	{"KR", "Seoul", "Seoul", 37.5665, 126.9780, 2},
	{"SE", "Stockholm", "Stockholm", 59.3293, 18.0686, 2},
	{"PL", "Mazovia", "Warsaw", 52.2297, 21.0122, 2},
	{"TR", "Istanbul", "Istanbul", 41.0082, 28.9784, 2},
	{"ZA", "Gauteng", "Johannesburg", -26.2041, 28.0473, 1},
}

var (
	seedDesktopScreens = [][2]int64{{1920, 1080}, {1366, 768}, {1440, 900}, {1536, 864}, {1680, 1050}, {2560, 1440}}
	seedMobileScreens  = [][2]int64{{390, 844}, {414, 896}, {375, 667}, {360, 800}, {412, 915}, {393, 851}}
	seedUTMSources     = []string{"google", "twitter", "hn", "newsletter", "github", "producthunt"}
	seedUTMMediums     = []string{"cpc", "social", "email", "referral", "organic"}
	seedUTMCampaigns   = []string{"launch-2026", "spring-promo", "blog-feature", "rebrand", "retarget"}
)

// Demo custom events, with a per-session probability each, so the custom-card
// picker has something in it.
var seedCustomEvents = []struct {
	Name        string
	Probability float64
}{
	{"signup", 0.05},
	{"checkout_success", 0.02},
	{"signup_cta_click", 0.08},
}

func weightedPick[T any](items []T, weight func(T) int) T {
	total := 0
	for _, it := range items {
		total += weight(it)
	}
	pick := rand.IntN(total)
	for _, it := range items {
		w := weight(it)
		if pick < w {
			return it
		}
		pick -= w
	}
	return items[len(items)-1]
}

// runSeed wipes and refills the Seed Test property. Re-runs reuse it rather
// than making a new one, so the dashboard URL stays stable across seeds.
func runSeed(ctx context.Context, db *sql.DB, sessions, days int) error {
	id, err := ensureSeedProperty(ctx, db)
	if err != nil {
		return err
	}

	for _, table := range []string{"events", "bot_events"} {
		if _, err := db.ExecContext(ctx,
			"DELETE FROM "+table+" WHERE property_id = ?", id[:]); err != nil {
			return fmt.Errorf("clear %s: %w", table, err)
		}
	}

	cards := make([]CustomCard, 0, len(seedCustomEvents))
	for _, ce := range seedCustomEvents {
		cards = append(cards, CustomCard{Event: ce.Name, Value: true})
	}
	encoded, _ := json.Marshal(cards)
	if _, err := db.ExecContext(ctx,
		"UPDATE properties SET custom_cards = ?, updated_at = ? WHERE id = ?",
		string(encoded), time.Now().UnixMilli(), id[:]); err != nil {
		return fmt.Errorf("set custom cards: %w", err)
	}

	total, err := generateSeed(ctx, db, id, sessions, days)
	if err != nil {
		return err
	}

	slog.Info(fmt.Sprintf("seeded %d sessions (%d events) into %q (%s)", sessions, total, seedPropertyName, id))
	slog.Info(fmt.Sprintf("dashboard: http://localhost:8000/%s", id))
	return nil
}

func ensureSeedProperty(ctx context.Context, db *sql.DB) (uuid.UUID, error) {
	var raw []byte
	err := db.QueryRowContext(ctx,
		"SELECT id FROM properties WHERE name = ?", seedPropertyName).Scan(&raw)
	if err == nil {
		return uuid.FromBytes(raw)
	}
	if err != sql.ErrNoRows {
		return uuid.Nil, err
	}

	id := uuid.New()
	now := time.Now().UnixMilli()
	_, err = db.ExecContext(ctx,
		`INSERT INTO properties (id, name, custom_cards, is_protected, is_public, created_at, updated_at)
		 VALUES (?, ?, '[]', 0, 0, ?, ?)`,
		id[:], seedPropertyName, now, now)
	return id, err
}

func generateSeed(ctx context.Context, db *sql.DB, id uuid.UUID, sessions, days int) (int64, error) {
	// One transaction for the whole run. Otherwise every insert is its own
	// commit, and a few hundred thousand fsyncs turn a two-second job into a
	// several-minute one.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	human, err := tx.PrepareContext(ctx, `INSERT INTO events (
	    property_id, event, created_at, user_id, url, title, referrer, user_agent,
	    platform, browser, device, screen_width, screen_height, country, region, city,
	    lat, lon, utm_source, utm_medium, utm_campaign, time_on_page_ms, extra
	  ) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'{}')`)
	if err != nil {
		return 0, err
	}
	defer human.Close()

	bot, err := tx.PrepareContext(ctx, `INSERT INTO bot_events (
	    property_id, event, created_at, bot_name, url, user_agent, country, extra
	  ) VALUES (?,?,?,?,?,?,?,'{}')`)
	if err != nil {
		return 0, err
	}
	defer bot.Close()

	now := time.Now().UnixMilli()
	windowMS := int64(days) * 24 * 60 * 60 * 1000
	var total int64

	for i := 0; i < sessions; i++ {
		agent := weightedPick(seedAgents, func(a seedAgent) int { return a.Weight })
		geo := weightedPick(seedGeos, func(g seedGeo) int { return g.Weight })
		referrer := weightedPick(seedReferrers, func(r weightedString) int { return r.Weight }).Value

		userID := fmt.Sprintf("%d", 100_000_000+rand.Int64N(899_999_999))

		// Bias toward recent days. A uniform spread makes the default 28-day
		// window compare a full period against a nearly empty one, showing
		// +1000% on every card.
		offset := int64(math.Pow(rand.Float64(), 1.15) * float64(windowMS))
		sessionStart := now - offset

		url := weightedPick(seedURLs, func(u weightedURL) int { return u.Weight })

		if agent.IsBot {
			if _, err := bot.ExecContext(ctx, id[:], "page_view", sessionStart,
				agent.BotName, url.Path, agent.UA, geo.Country); err != nil {
				return total, err
			}
			total++
			continue
		}

		screens := seedDesktopScreens
		if agent.Device == "Mobile" {
			screens = seedMobileScreens
		}
		screen := screens[rand.IntN(len(screens))]

		var utmSource, utmMedium, utmCampaign any
		if rand.Float64() < 0.3 {
			utmSource = seedUTMSources[rand.IntN(len(seedUTMSources))]
			utmMedium = seedUTMMediums[rand.IntN(len(seedUTMMediums))]
			utmCampaign = seedUTMCampaigns[rand.IntN(len(seedUTMCampaigns))]
		}

		insert := func(event string, at int64, url weightedURL, ref any, utmS, utmM, utmC any, timeOnPage any) error {
			_, err := human.ExecContext(ctx, id[:], event, at, userID, url.Path, url.Title,
				ref, agent.UA, agent.Platform, agent.Browser, agent.Device,
				screen[0], screen[1], geo.Country, geo.Region, geo.City, geo.Lat, geo.Lon,
				utmS, utmM, utmC, timeOnPage)
			return err
		}

		var sessionRef any
		if referrer != "" {
			sessionRef = referrer
		}

		if err := insert("session_start", sessionStart, url, sessionRef, utmSource, utmMedium, utmCampaign, nil); err != nil {
			return total, err
		}
		total++

		for _, ce := range seedCustomEvents {
			if rand.Float64() < ce.Probability {
				at := sessionStart + 1_000 + rand.Int64N(29_000)
				if err := insert(ce.Name, at, url, nil, nil, nil, nil, nil); err != nil {
					return total, err
				}
				total++
			}
		}

		t := sessionStart
		pageCount := 1 + rand.IntN(8)
		for page := 0; page < pageCount; page++ {
			timeOnPage := 2_000 + rand.Int64N(118_000)

			// Only the first page view of a session carries the referrer,
			// matching what a real collector sends.
			var pvRef any
			if page == 0 {
				pvRef = sessionRef
			}
			if err := insert("page_view", t, url, pvRef, utmSource, utmMedium, utmCampaign, nil); err != nil {
				return total, err
			}
			total++

			if rand.Float64() < 0.4 {
				if err := insert("click", t+500+rand.Int64N(timeOnPage), url, nil, nil, nil, nil, nil); err != nil {
					return total, err
				}
				total++
			}

			if err := insert("page_leave", t+timeOnPage, url, nil, nil, nil, nil, timeOnPage); err != nil {
				return total, err
			}
			total++

			t += timeOnPage + 500 + rand.Int64N(2_500)
			if page+1 < pageCount {
				url = weightedPick(seedURLs, func(u weightedURL) int { return u.Weight })
			}
		}
	}

	return total, tx.Commit()
}
