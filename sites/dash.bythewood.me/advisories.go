package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// Security advisories for what the VPS and Isaac's machines run. Only the big
// ones: anything CISA lists as exploited, and high or critical advisories on the
// projects that publish their own. Everything here is public and keyless.
const (
	advisoriesEvery = 3 * time.Hour

	// The loop wakes this often and only runs once advisoriesEvery has passed,
	// or sooner when the last run was partial.
	advisoriesPoll = 30 * time.Minute
	advisoryWindow = 30 * 24 * time.Hour

	// Long enough to see over a weekend, short enough that a banner doesn't
	// become furniture.
	bannerFor = 72 * time.Hour

	kevURL      = "https://www.cisa.gov/sites/default/files/feeds/known_exploited_vulnerabilities.json"
	trackerURL  = "https://security-tracker.debian.org/tracker/"
	osvURL      = "https://api.osv.dev/v1/query"
	ghRepoURL   = "https://api.github.com/repos/%s/security-advisories?per_page=10&sort=published&direction=desc"
	mfsaListURL = "https://api.github.com/repos/mozilla/foundation-security-advisories/contents/announce/%d"
	mfsaRawURL  = "https://raw.githubusercontent.com/mozilla/foundation-security-advisories/master/announce/%d/%s"
)

// Every source here is a public service run by somebody else, so it gets told
// who is asking instead of a browser's string.
var politeAgent = map[string]string{"User-Agent": feedAgent}

// The Debian release the host runs, as the tracker names it.
const debianRelease = "trixie"

// kevWatch is a KEV vendor and product, matched as lowercase substrings. An
// empty product is any product, and Debian names the source package the
// tracker knows it by, when the host gets it from Debian.
type kevWatch struct {
	label, vendor, product, debian, where string
}

var kevWatchlist = []kevWatch{
	{"LINUX KERNEL", "linux", "kernel", "linux", "VPS"},
	{"OPENSSH", "openbsd", "openssh", "openssh", "VPS"},
	{"OPENSSL", "openssl", "", "openssl", "VPS"},
	{"GLIBC", "gnu", "glibc", "glibc", "VPS"},
	{"SUDO", "sudo", "", "sudo", "VPS"},
	{"XZ", "tukaani", "xz", "xz-utils", "VPS"},
	{"SQLITE", "sqlite", "", "", "VPS"},
	{"DOCKER", "docker", "", "", "VPS"},
	{"RUNC", "", "runc", "", "VPS"},
	{"CONTAINERD", "", "containerd", "", "VPS"},
	{"CADDY", "caddy", "", "", "VPS"},
	{"CLOUDFLARE", "cloudflare", "", "", "VPS"},
	{"WINDOWS", "microsoft", "", "", "WINDOWS"},
	{"FIREFOX", "mozilla", "firefox", "", "EVERYWHERE"},
	{"THUNDERBIRD", "mozilla", "thunderbird", "", "WINDOWS"},
	{"STEAM", "valve", "", "", "WINDOWS"},
	{"ANDROID", "google", "android", "", "PHONE"},
	{"PIXEL", "google", "pixel", "", "PHONE"},
	{"BITWARDEN", "bitwarden", "", "", "EVERYWHERE"},
	{"TAILSCALE", "tailscale", "", "", "EVERYWHERE"},
	{"BUN", "oven", "bun", "", "VPS"},
	{"CLAUDE CODE", "anthropic", "", "", "VPS"},
	{"GO", "golang", "", "", "VPS"},
}

// Microsoft's KEV entries are mostly server products nobody here runs, so
// anything naming one of these is left out and the rest counts as Windows.
var microsoftSkip = []string{"office", "sharepoint", "exchange", "sql server", "outlook", "word", "excel", "skype", "dynamics", "azure", "edge", "internet explorer", "visual studio", ".net", "teams"}

func (w kevWatch) matches(vendor, product string) bool {
	v, p := strings.ToLower(vendor), strings.ToLower(product)
	if w.vendor != "" && !strings.Contains(v, w.vendor) {
		return false
	}
	if w.product != "" && !strings.Contains(p, w.product) {
		return false
	}
	if w.vendor == "microsoft" {
		for _, s := range microsoftSkip {
			if strings.Contains(p, s) {
				return false
			}
		}
	}
	return true
}

func kevMatch(vendor, product string) (kevWatch, bool) {
	for _, w := range kevWatchlist {
		if w.matches(vendor, product) {
			return w, true
		}
	}
	return kevWatch{}, false
}

// What to do about an exploited bug dash can't see the version of, per product.
var checkHint = map[string]string{
	"WINDOWS": "WINDOWS UPDATE", "FIREFOX": "FIREFOX UPDATE", "THUNDERBIRD": "THUNDERBIRD UPDATE",
	"STEAM": "STEAM CLIENT", "ANDROID": "SYSTEM UPDATE", "PIXEL": "SYSTEM UPDATE",
}

// osvWatch is a package OSV tracks. With versioned set, the query carries what
// is running and anything that comes back is open, instead of every advisory
// the package has ever had.
type osvWatch struct {
	label, ecosystem, name string
	versioned              bool
}

var osvWatchlist = []osvWatch{
	{"CLAUDE CODE", "npm", "@anthropic-ai/claude-code", false},
	{"BITWARDEN", "npm", "@bitwarden/cli", false},
	{"DOCKER", "Go", "github.com/docker/docker", false},
	{"RUNC", "Go", "github.com/opencontainers/runc", false},
	{"CONTAINERD", "Go", "github.com/containerd/containerd/v2", false},
	{"GO", "Go", "stdlib", true},
}

// Projects whose advisories are only on their own GitHub repository, read from
// the repository advisories API, which answers without a token.
var repoWatchlist = []struct{ label, repo string }{
	{"BUN", "oven-sh/bun"},
	{"AEGIS", "beemdevelopment/Aegis"},
	{"UBLOCK ORIGIN", "gorhill/uBlock"},
	{"BITWARDEN", "bitwarden/clients"},
	{"CADDY", "caddyserver/caddy"},
	{"CLOUDFLARED", "cloudflare/cloudflared"},
	{"NTFY", "binwiederhier/ntfy"},
	{"RESTIC", "restic/restic"},
	{"TAILSCALE", "tailscale/tailscale"},
	{"MULLVAD", "mullvad/mullvadvpn-app"},
}

// Advisory is one row. State is open (the version here is affected), check
// (exploited and dash can't see the version), fixed (a fix is out and dash
// can't see the version), patched, or clear (exploited but not in this setup).
type Advisory struct {
	ID      string `json:"id"`
	Product string `json:"product"`
	Title   string `json:"title"`
	Tag     string `json:"tag"`
	Source  string `json:"source"`
	URL     string `json:"url"`
	Age     string `json:"age"`
	Detail  string `json:"detail"`
	State   string `json:"state"`
	Status  string `json:"status"`
	Note    string `json:"note"`
	Banner  bool   `json:"banner"`
	Added   string `json:"added"`

	at time.Time
}

type Advisories struct {
	Rows []Advisory `json:"rows"`

	// How many rows want doing something, and how many KEV entries the window
	// held against how many were on the watchlist, so a quiet panel reads as
	// quiet instead of broken.
	Act       int       `json:"act"`
	KEVAdded  int       `json:"kev_added"`
	KEVKept   int       `json:"kev_kept"`
	Checked   string    `json:"checked"`
	NextPatch string    `json:"next_patch"`
	At        time.Time `json:"at"`

	// Partial means a source failed and kept its old rows, so the next poll
	// runs instead of waiting out the full interval.
	Partial bool `json:"partial"`
}

type kevCatalog struct {
	Vulnerabilities []struct {
		CVE         string `json:"cveID"`
		Vendor      string `json:"vendorProject"`
		Product     string `json:"product"`
		Name        string `json:"vulnerabilityName"`
		Description string `json:"shortDescription"`
		DateAdded   string `json:"dateAdded"`
	} `json:"vulnerabilities"`
}

// fetchAdvisories builds the panel. Each source fails on its own, and a source
// that failed keeps its rows from the last run, so one dead upstream doesn't
// empty the panel for three hours.
func fetchAdvisories(ctx context.Context, g *Guard, prev Advisories, now time.Time) Advisories {
	host := readHostVersions()
	out := Advisories{Checked: now.In(easternTime()).Format("15:04"), NextPatch: nextPatchTuesday(now), At: now}

	var rows []Advisory
	kev, added, kept, err := kevAdvisories(ctx, g, host, now)
	if err != nil {
		pollFailed(ctx, "kev", "security", err)
		out.Partial = true
		rows = append(rows, keepSource(prev.Rows, "KEV")...)
		out.KEVAdded, out.KEVKept = prev.KEVAdded, prev.KEVKept
	} else {
		rows = append(rows, kev...)
		out.KEVAdded, out.KEVKept = added, kept
	}

	osv, err := osvAdvisories(ctx, g, host, now)
	if err != nil {
		pollFailed(ctx, "osv", "security", err)
		out.Partial = true
		rows = append(rows, keepSource(prev.Rows, "OSV")...)
	} else {
		rows = append(rows, osv...)
	}

	repo, err := repoAdvisories(ctx, g, now)
	if err != nil {
		pollFailed(ctx, "repo advisories", "security", err)
		out.Partial = true
		rows = append(rows, keepSource(prev.Rows, "GHSA")...)
	} else {
		rows = append(rows, repo...)
	}

	mfsa, err := mozillaAdvisories(ctx, g, now)
	if err != nil {
		pollFailed(ctx, "mfsa", "security", err)
		out.Partial = true
		rows = append(rows, keepSource(prev.Rows, "MFSA")...)
	} else {
		rows = append(rows, mfsa...)
	}

	out.Rows = finishRows(rows, now)
	for _, r := range out.Rows {
		if r.State == "open" || r.State == "check" {
			out.Act++
		}
	}
	return out
}

func keepSource(rows []Advisory, source string) []Advisory {
	var kept []Advisory
	for _, r := range rows {
		if r.Source == source {
			kept = append(kept, r)
		}
	}
	return kept
}

// finishRows drops duplicates by ID (KEV and a GHSA can name the same CVE),
// ages every row against now, and sorts what wants doing first.
func finishRows(rows []Advisory, now time.Time) []Advisory {
	seen := map[string]bool{}
	var out []Advisory
	for _, r := range rows {
		if seen[r.ID] {
			continue
		}
		seen[r.ID] = true
		if r.at.IsZero() {
			r.at, _ = time.Parse(time.DateOnly, r.Added)
		}
		if !r.at.IsZero() {
			r.Age = dayAge(now.Sub(r.at))
		}
		r.Banner = r.Tag == "EXPLOITED" && (r.State == "open" || r.State == "check") && now.Sub(r.at) < bannerFor
		out = append(out, r)
	}
	rank := map[string]int{"open": 0, "check": 1, "fixed": 2, "patched": 3, "clear": 4}
	slices.SortStableFunc(out, func(a, b Advisory) int {
		if d := rank[a.State] - rank[b.State]; d != 0 {
			return d
		}
		return b.at.Compare(a.at)
	})
	return out
}

func dayAge(d time.Duration) string {
	days := int(d.Hours() / 24)
	if days < 1 {
		return "TODAY"
	}
	return fmt.Sprintf("%dD AGO", days)
}

func kevAdvisories(ctx context.Context, g *Guard, host hostVersions, now time.Time) ([]Advisory, int, int, error) {
	var cat kevCatalog
	if err := getJSONHeaders(ctx, g, "kev", kevURL, politeAgent, &cat); err != nil {
		return nil, 0, 0, err
	}

	var rows []Advisory
	added := 0
	for _, v := range cat.Vulnerabilities {
		at, err := time.Parse(time.DateOnly, v.DateAdded)
		if err != nil || now.Sub(at) > advisoryWindow {
			continue
		}
		added++
		w, ok := kevMatch(v.Vendor, v.Product)
		if !ok {
			continue
		}
		r := Advisory{
			ID:      v.CVE,
			Product: w.label,
			Title:   kevTitle(v.Name, v.Vendor, v.Product),
			Tag:     "EXPLOITED",
			Source:  "KEV",
			URL:     "https://nvd.nist.gov/vuln/detail/" + v.CVE,
			Added:   v.DateAdded,
			at:      at,
		}
		if w.debian != "" {
			debianStatus(ctx, g, &r, w.debian, host)
		} else {
			r.State, r.Status = "check", "CHECK"
			r.Note = cmpOr(checkHint[w.label], w.where)
		}
		rows = append(rows, r)
	}
	return rows, added, len(rows), nil
}

// KEV names lead with the vendor and product ("Microsoft Windows Command
// Injection Vulnerability"), and the row already says the product.
func kevTitle(name, vendor, product string) string {
	t := strings.TrimSuffix(strings.TrimSpace(name), " Vulnerability")
	t = strings.TrimPrefix(t, vendor+" ")
	return strings.TrimPrefix(t, product+" ")
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// trackerFix is what Debian's tracker says about one CVE in one source
// package for the host's release.
type trackerFix struct {
	fixed       string // the version that fixes it, empty when there isn't one
	urgency     string
	notAffected bool
	known       bool
}

var (
	trRow  = regexp.MustCompile(`(?s)<tr>(.*?)</tr>`)
	trCell = regexp.MustCompile(`(?s)<td>(.*?)</td>`)
	trTag  = regexp.MustCompile(`<[^>]+>`)
)

func trackerCells(row string) []string {
	var cells []string
	for _, m := range trCell.FindAllStringSubmatch(row, -1) {
		cells = append(cells, strings.TrimSpace(html.UnescapeString(trTag.ReplaceAllString(m[1], ""))))
	}
	return cells
}

// parseTracker reads the two tables on a tracker CVE page. The second, of fixed
// versions, is the one with the release's fix and the urgency, and the first
// says not-affected when the release never had the bug.
func parseTracker(page, pkg string) trackerFix {
	var f trackerFix
	var unstableUrgency string
	current := ""
	for _, m := range trRow.FindAllStringSubmatch(page, -1) {
		c := trackerCells(m[1])
		switch len(c) {
		case 4:
			if c[0] != "" {
				current = strings.Fields(c[0])[0]
			}
			if current == pkg && c[1] == debianRelease {
				f.known = true
				if c[3] == "not-affected" {
					f.notAffected = true
				}
			}
		case 7:
			if c[0] != pkg || c[1] != "source" {
				continue
			}
			switch c[2] {
			case debianRelease:
				f.known = true
				f.urgency = c[4]
				if c[3] == "<not-affected>" {
					f.notAffected = true
				} else if !strings.HasPrefix(c[3], "<") {
					f.fixed = c[3]
				}
			case "(unstable)":
				unstableUrgency = c[4]
			}
		}
	}
	if f.urgency == "" {
		f.urgency = unstableUrgency
	}
	return f
}

var trackerCache = struct {
	sync.Mutex
	m map[string]steamLookup[trackerFix]
}{m: map[string]steamLookup[trackerFix]{}}

// debianStatus fills in a row from the tracker and the host's own version. A
// tracker page changes when a DSA lands, so each one is reread every poll, but
// a failed read keeps the last answer.
func debianStatus(ctx context.Context, g *Guard, r *Advisory, pkg string, host hostVersions) {
	key := r.ID + "/" + pkg
	page, err := getPage(ctx, g, "debian", trackerURL+r.ID)
	var f trackerFix
	if err == nil {
		f = parseTracker(page, pkg)
		trackerCache.Lock()
		trackerCache.m[key] = steamLookup[trackerFix]{at: time.Now(), v: f}
		trackerCache.Unlock()
	} else {
		pollFailed(ctx, "debian tracker", "security", err)
		trackerCache.Lock()
		f = trackerCache.m[key].v
		trackerCache.Unlock()
	}

	have, where := host.version(pkg)
	switch {
	case !f.known:
		r.State, r.Status, r.Note = "check", "CHECK", "NOT IN TRACKER"
	case f.notAffected:
		r.State, r.Status, r.Note = "clear", "NOT AFFECTED", strings.ToUpper(debianRelease)
	case f.urgency == "unimportant":
		r.State, r.Status, r.Note = "clear", "NOT AFFECTED", strings.ToUpper(debianRelease)
	case f.fixed == "":
		r.State, r.Status, r.Note = "open", "NO FIX YET", strings.ToUpper(debianRelease)
	default:
		r.Detail = strings.ToUpper(debianRelease) + " FIXED " + f.fixed
		switch {
		case have == "":
			r.State, r.Status, r.Note = "check", "CHECK", "VERSION UNKNOWN"
		case compareDebian(have, f.fixed) >= 0:
			r.State, r.Status, r.Note = "patched", "PATCHED", where+" "+have
		default:
			r.State, r.Status, r.Note = "open", "UPDATE", where+" "+have
		}
	}
	if f.urgency == "unimportant" && r.Detail == "" {
		r.Detail = "DEBIAN UNIMPORTANT"
	}
}

type osvQuery struct {
	Package struct {
		Name      string `json:"name"`
		Ecosystem string `json:"ecosystem"`
	} `json:"package"`
	Version string `json:"version,omitempty"`
}

type osvVuln struct {
	ID        string   `json:"id"`
	Summary   string   `json:"summary"`
	Aliases   []string `json:"aliases"`
	Published string   `json:"published"`
	Database  struct {
		Severity string `json:"severity"`
	} `json:"database_specific"`
	Affected []struct {
		Ranges []struct {
			Events []struct {
				Fixed string `json:"fixed"`
			} `json:"events"`
		} `json:"ranges"`
	} `json:"affected"`
}

func (v osvVuln) fixedIn() string {
	for _, a := range v.Affected {
		for _, r := range a.Ranges {
			for _, e := range r.Events {
				if e.Fixed != "" {
					return e.Fixed
				}
			}
		}
	}
	return ""
}

func (v osvVuln) cve() string {
	for _, a := range v.Aliases {
		if strings.HasPrefix(a, "CVE-") {
			return a
		}
	}
	return ""
}

func osvAdvisories(ctx context.Context, g *Guard, host hostVersions, now time.Time) ([]Advisory, error) {
	var rows []Advisory
	var firstErr error
	for _, w := range osvWatchlist {
		var q osvQuery
		q.Package.Name, q.Package.Ecosystem = w.name, w.ecosystem
		if w.versioned {
			q.Version = host.goVersion
		}
		var resp struct {
			Vulns []osvVuln `json:"vulns"`
		}
		if err := postJSONHeaders(ctx, g, "osv", osvURL, politeAgent, q, &resp); err != nil {
			firstErr = cmpErr(firstErr, err)
			continue
		}
		if w.versioned {
			if r, ok := goRow(resp.Vulns, host.goVersion, now); ok {
				rows = append(rows, r)
			}
			continue
		}
		for _, v := range resp.Vulns {
			at, err := time.Parse(time.RFC3339, v.Published)
			if err != nil || now.Sub(at) > advisoryWindow || !bigSeverity(v.Database.Severity) {
				continue
			}
			rows = append(rows, fixedRow(w.label, cmpOr(v.cve(), v.ID), v.ID, v.Summary, v.Database.Severity, v.fixedIn(), "OSV", "https://osv.dev/vulnerability/"+v.ID, at))
		}
	}
	return rows, firstErr
}

func cmpErr(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

func bigSeverity(s string) bool {
	s = strings.ToUpper(s)
	return s == "HIGH" || s == "CRITICAL"
}

func fixedRow(product, id, ref, title, severity, fixed, source, url string, at time.Time) Advisory {
	r := Advisory{
		ID:      id,
		Product: product,
		Title:   trimProduct(strings.TrimSpace(title), product),
		Tag:     strings.ToUpper(severity),
		Source:  source,
		URL:     url,
		Added:   at.Format(time.DateOnly),
		State:   "fixed",
		Status:  "FIXED",
		at:      at,
	}
	if ref != id {
		r.Detail = ref
	}
	if fixed != "" {
		r.Note = "IN " + strings.TrimPrefix(fixed, "v")
	}
	return r
}

// GHSA summaries often open with the project's name and a colon.
func trimProduct(title, product string) string {
	prefix := product + ": "
	if len(title) > len(prefix) && strings.EqualFold(title[:len(prefix)], prefix) {
		return title[len(prefix):]
	}
	return title
}

// goRow collapses every stdlib advisory affecting the running Go into one row,
// since a Go point release fixes them together and they read as one job.
func goRow(vulns []osvVuln, running string, now time.Time) (Advisory, bool) {
	if running == "" || len(vulns) == 0 {
		return Advisory{}, false
	}
	var newest time.Time
	fixed := ""
	for _, v := range vulns {
		if at, err := time.Parse(time.RFC3339, v.Published); err == nil && at.After(newest) {
			newest = at
		}
		if f := v.fixedIn(); f != "" && (fixed == "" || compareDebian(f, fixed) > 0) {
			fixed = f
		}
	}
	title := fmt.Sprintf("%d standard library advisories affect go%s", len(vulns), running)
	if len(vulns) == 1 {
		title = cmpOr(vulns[0].Summary, title)
	}
	r := Advisory{
		ID:      vulns[0].ID,
		Product: "GO",
		Title:   title,
		Tag:     "STDLIB",
		Source:  "OSV",
		URL:     "https://pkg.go.dev/vuln/" + vulns[0].ID,
		State:   "open",
		Status:  "UPDATE",
		Note:    "GO" + running,
		at:      newest,
	}
	if fixed != "" {
		r.Detail = "FIXED GO" + fixed
	}
	if !newest.IsZero() {
		r.Added = newest.Format(time.DateOnly)
	}
	return r, true
}

type repoAdvisory struct {
	GHSA      string `json:"ghsa_id"`
	CVE       string `json:"cve_id"`
	Summary   string `json:"summary"`
	Severity  string `json:"severity"`
	URL       string `json:"html_url"`
	Published string `json:"published_at"`
	Vulns     []struct {
		Patched string `json:"patched_versions"`
	} `json:"vulnerabilities"`
}

func repoAdvisories(ctx context.Context, g *Guard, now time.Time) ([]Advisory, error) {
	var rows []Advisory
	var firstErr error
	for _, w := range repoWatchlist {
		var list []repoAdvisory
		if err := getJSONHeaders(ctx, g, "github", fmt.Sprintf(ghRepoURL, w.repo), politeAgent, &list); err != nil {
			firstErr = cmpErr(firstErr, err)
			continue
		}
		for _, a := range list {
			at, err := time.Parse(time.RFC3339, a.Published)
			if err != nil || now.Sub(at) > advisoryWindow || !bigSeverity(a.Severity) {
				continue
			}
			fixed := ""
			for _, v := range a.Vulns {
				fixed = cmpOr(fixed, v.Patched)
			}
			rows = append(rows, fixedRow(w.label, cmpOr(a.CVE, a.GHSA), a.GHSA, a.Summary, a.Severity, fixed, "GHSA", a.URL, at))
		}
	}
	return rows, firstErr
}

type mfsaFile struct {
	Name string `json:"name"`
}

// The advisory YAML files never change once published, so each is read once.
var mfsaCache = struct {
	sync.Mutex
	m map[string]mfsa
}{m: map[string]mfsa{}}

type mfsa struct {
	title, impact string
	announced     time.Time
}

var mfsaLine = regexp.MustCompile(`(?m)^(title|impact|announced):\s*(.+)$`)

func parseMFSA(body string) mfsa {
	var m mfsa
	for _, l := range mfsaLine.FindAllStringSubmatch(body, -1) {
		v := strings.Trim(strings.TrimSpace(l[2]), `"'`)
		switch l[1] {
		case "title":
			m.title = v
		case "impact":
			m.impact = strings.ToLower(v)
		case "announced":
			m.announced, _ = time.Parse("January 2, 2006", v)
		}
	}
	return m
}

// mozillaAdvisories reads the newest few files in Mozilla's advisory
// repository and keeps critical ones for Firefox and Thunderbird. ESR is left
// out, since nothing here runs it.
func mozillaAdvisories(ctx context.Context, g *Guard, now time.Time) ([]Advisory, error) {
	years := []int{now.Year()}
	if now.YearDay() <= 31 {
		years = append(years, now.Year()-1)
	}
	var rows []Advisory
	for _, year := range years {
		var files []mfsaFile
		if err := getJSONHeaders(ctx, g, "github", fmt.Sprintf(mfsaListURL, year), politeAgent, &files); err != nil {
			return nil, err
		}
		slices.SortFunc(files, func(a, b mfsaFile) int { return strings.Compare(b.Name, a.Name) })
		for _, f := range files[:min(len(files), 8)] {
			mfsaCache.Lock()
			m, ok := mfsaCache.m[f.Name]
			mfsaCache.Unlock()
			if !ok {
				body, err := getPage(ctx, g, "githubraw", fmt.Sprintf(mfsaRawURL, year, f.Name))
				if err != nil {
					return nil, err
				}
				m = parseMFSA(body)
				mfsaCache.Lock()
				mfsaCache.m[f.Name] = m
				mfsaCache.Unlock()
			}
			if m.impact != "critical" || m.announced.IsZero() || now.Sub(m.announced) > advisoryWindow || strings.Contains(m.title, "ESR") {
				continue
			}
			product := "FIREFOX"
			if strings.Contains(m.title, "Thunderbird") {
				product = "THUNDERBIRD"
			} else if !strings.Contains(m.title, "Firefox") {
				continue
			}
			id := strings.ToUpper(strings.TrimSuffix(f.Name, ".yml"))
			rows = append(rows, Advisory{
				ID:      id,
				Product: product,
				Title:   m.title,
				Tag:     "CRITICAL",
				Source:  "MFSA",
				URL:     "https://www.mozilla.org/security/advisories/" + strings.ToLower(id) + "/",
				Added:   m.announced.Format(time.DateOnly),
				State:   "fixed",
				Status:  "FIXED",
				Note:    "AUTO-UPDATES",
				at:      m.announced,
			})
		}
	}
	return rows, nil
}

// nextPatchTuesday is the second Tuesday of this month, or next month's once
// this one's has passed.
func nextPatchTuesday(now time.Time) string {
	ny := now.In(easternTime())
	for i := 0; i < 2; i++ {
		first := time.Date(ny.Year(), ny.Month()+time.Month(i), 1, 0, 0, 0, 0, easternTime())
		offset := (int(time.Tuesday) - int(first.Weekday()) + 7) % 7
		day := first.AddDate(0, 0, offset+7)
		if !day.Before(time.Date(ny.Year(), ny.Month(), ny.Day(), 0, 0, 0, 0, easternTime())) {
			return strings.ToUpper(day.Format("Mon Jan 2"))
		}
	}
	return ""
}

func advisoryNotices(a Advisories) []Notice {
	var out []Notice
	for _, r := range a.Rows {
		if !r.Banner {
			continue
		}
		out = append(out, Notice{
			ID:    "advisory:" + r.ID,
			Kind:  "security",
			Title: "Exploited: " + r.Product,
			Body:  r.Title,
			URL:   r.URL,
		})
	}
	return out
}

// The panel is saved after every run, so a deploy shows the last check and
// only asks the upstreams again once it's due, instead of on every restart.
func loadAdvisories(path string, now time.Time) (Advisories, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Advisories{}, false
	}
	var a Advisories
	if json.Unmarshal(raw, &a) != nil || a.At.IsZero() {
		return Advisories{}, false
	}
	a.Rows = finishRows(a.Rows, now)
	a.NextPatch = nextPatchTuesday(now)
	return a, true
}

func saveAdvisories(path string, a Advisories) {
	raw, err := json.Marshal(a)
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err = os.WriteFile(tmp, raw, 0o644); err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		slog.Warn("advisories not saved", slog.String("component", "security"), slog.Any("err", err))
	}
}
