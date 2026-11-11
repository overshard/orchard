package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// A stand-in for what Vite writes, so the tests need no frontend build.
func testDist() fstest.MapFS {
	return fstest.MapFS{
		".vite/manifest.json": {Data: []byte(`{
			"src/style.js": {"file": "assets/style-aaa111.js", "src": "src/style.js", "isEntry": true, "css": ["assets/style-def456.css"]},
			"src/index.js": {"file": "assets/index-abc123.js", "src": "src/index.js", "isEntry": true},
			"src/key.js": {"file": "assets/key-abc123.js", "src": "src/key.js", "isEntry": true},
			"src/k.js": {"file": "assets/k-abc123.js", "src": "src/k.js", "isEntry": true},
			"src/share.js": {"file": "assets/share-abc123.js", "src": "src/share.js", "isEntry": true}
		}`)},
		"assets/share-abc123.js":  {Data: []byte("")},
		"assets/style-aaa111.js":  {Data: []byte("")},
		"assets/index-abc123.js":  {Data: []byte("")},
		"assets/key-abc123.js":    {Data: []byte("")},
		"assets/k-abc123.js":      {Data: []byte("")},
		"assets/style-def456.css": {Data: []byte("")},
	}
}

type browser struct {
	t      *testing.T
	h      http.Handler
	cookie *http.Cookie
}

func testSite(t *testing.T) (*browser, *store, *time.Time) {
	t.Helper()
	db, clock := testStore(t)
	s, err := newSite(testDist(), db)
	if err != nil {
		t.Fatal(err)
	}
	return &browser{t: t, h: s.handler()}, db, clock
}

func (b *browser) do(method, path, body string, hdr ...string) *httptest.ResponseRecorder {
	b.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	if b.cookie != nil {
		req.AddCookie(b.cookie)
	}
	rec := httptest.NewRecorder()
	b.h.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.Name == keyCookie {
			b.cookie = c
		}
	}
	return rec
}

func (b *browser) post(path, body string, into any) int {
	b.t.Helper()
	rec := b.do(http.MethodPost, path, body)
	if into != nil && rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), into); err != nil {
			b.t.Fatalf("%s: %v in %s", path, err, rec.Body.String())
		}
	}
	return rec.Code
}

func TestTemplatesParse(t *testing.T) {
	templates, err := fs.Sub(templateFS, "templates")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range append(append([]string{}, layoutTemplates...), pageTemplates...) {
		if _, err := fs.Stat(templates, name); err != nil {
			t.Errorf("%s is listed with no file behind it", name)
		}
	}
	if _, _, _ = testSite(t); t.Failed() {
		t.Fatal("the site did not build")
	}
}

func TestRoutes(t *testing.T) {
	b, _, _ := testSite(t)
	cases := []struct {
		path   string
		status int
		body   string
		cache  string
	}{
		{"/", http.StatusOK, "/static/assets/index-abc123.js", "public, max-age=300"},
		{"/key", http.StatusOK, "/static/assets/key-abc123.js", "no-store"},
		{"/k", http.StatusOK, "/static/assets/k-abc123.js", ""},
		{"/healthz", http.StatusOK, "ok", "no-store"},
		{"/robots.txt", http.StatusOK, "User-agent: *", ""},
		{"/static/assets/index-abc123.js", http.StatusOK, "", "immutable"},
		{"/api/pond", http.StatusUnauthorized, "no pond", "no-store"},
		{"/api/panel", http.StatusOK, `"house":true`, "no-store"},
		{"/nothing-here", http.StatusNotFound, "No such page", "no-store"},
	}
	for _, c := range cases {
		rec := b.do(http.MethodGet, c.path, "")
		if rec.Code != c.status {
			t.Errorf("%s: status %d, want %d", c.path, rec.Code, c.status)
		}
		if !strings.Contains(rec.Body.String(), c.body) {
			t.Errorf("%s: body does not contain %q", c.path, c.body)
		}
		if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, c.cache) {
			t.Errorf("%s: Cache-Control %q, want it to contain %q", c.path, got, c.cache)
		}
		if strings.HasPrefix(c.path, "/static/") || c.status != http.StatusOK || c.path == "/healthz" || c.path == "/robots.txt" || strings.HasPrefix(c.path, "/api/") {
			continue
		}
		if !strings.Contains(rec.Body.String(), "/static/assets/style-def456.css") {
			t.Errorf("%s does not link the stylesheet", c.path)
		}
	}
}

// Fetching the page lays nothing. The egg is laid by the page's own script.
func TestFirstVisitLaysAnEggAndReloadKeepsIt(t *testing.T) {
	b, db, _ := testSite(t)
	b.do(http.MethodGet, "/", "")
	var ponds int
	_ = db.r.QueryRow(`SELECT COUNT(*) FROM ponds`).Scan(&ponds)
	if ponds != 0 || b.cookie != nil {
		t.Fatalf("a plain GET laid %d eggs", ponds)
	}

	var first greeting
	if code := b.post("/api/greet", "{}", &first); code != http.StatusOK {
		t.Fatalf("greet: %d", code)
	}
	if !first.First || first.Present != nil || first.Pond.Pebbles != starterPebbles {
		t.Errorf("first greeting: %+v", first)
	}
	if first.Pond.Frog.Stage != "egg" || first.Pond.Frog.Name == "" || first.Pond.Frog.Rarity.N == 0 {
		t.Errorf("the new frog: %+v", first.Pond.Frog)
	}
	c := b.cookie
	if c == nil || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Domain != "" || c.MaxAge < 300*86400 {
		t.Fatalf("the cookie: %+v", c)
	}

	var again greeting
	b.post("/api/greet", "{}", &again)
	if again.First || again.Pond.Frog.Seed != first.Pond.Frog.Seed {
		t.Errorf("a reload got a different frog: %+v", again.Pond.Frog)
	}
	_ = db.r.QueryRow(`SELECT COUNT(*) FROM ponds`).Scan(&ponds)
	if ponds != 1 {
		t.Errorf("%d ponds after a reload, want 1", ponds)
	}
}

func TestPostsAreFenced(t *testing.T) {
	b, _, _ := testSite(t)
	cases := []struct {
		hdr  []string
		want int
	}{
		{[]string{"Sec-Fetch-Site", "same-site"}, http.StatusForbidden},
		{[]string{"Sec-Fetch-Site", "cross-site"}, http.StatusForbidden},
		{[]string{"Content-Type", "text/plain"}, http.StatusUnsupportedMediaType},
		{[]string{"Content-Type", "application/x-www-form-urlencoded"}, http.StatusUnsupportedMediaType},
	}
	for _, path := range []string{"/api/greet", "/api/wheel/spin", "/api/key/restore", "/api/hats/box"} {
		for _, c := range cases {
			if rec := b.do(http.MethodPost, path, "{}", c.hdr...); rec.Code != c.want {
				t.Errorf("%s with %v: %d, want %d", path, c.hdr, rec.Code, c.want)
			}
		}
	}
	if rec := b.do(http.MethodPost, "/api/wheel/spin", "{}"); rec.Code != http.StatusUnauthorized {
		t.Errorf("a spin with no pond: %d", rec.Code)
	}
	if rec := b.do(http.MethodPost, "/api/dev/clock", `{"hours":1}`); rec.Code != http.StatusOK {
		t.Errorf("the dev clock is missing from a dev build: %d", rec.Code)
	}
}

func TestKeyPageAndBringingAFrogBack(t *testing.T) {
	b, _, _ := testSite(t)
	var g greeting
	b.post("/api/greet", "{}", &g)
	key := b.cookie.Value
	words := strings.Split(key, "-")

	page := b.do(http.MethodGet, "/key", "")
	body := page.Body.String()
	for _, w := range words {
		if !strings.Contains(body, "<li>"+w+"</li>") {
			t.Errorf("the key page is missing the word %q", w)
		}
	}
	if !strings.Contains(body, baseURL+"/k#"+key) {
		t.Error("the key page has no QR address")
	}
	for name, rec := range map[string]*httptest.ResponseRecorder{"/key": page, "/k": b.do(http.MethodGet, "/k", "")} {
		if strings.Contains(rec.Body.String(), "collector") {
			t.Errorf("%s loads the analytics collector", name)
		}
		if csp := rec.Header().Get("Content-Security-Policy"); strings.Contains(csp, "analytics") || strings.Contains(csp, "script-src 'self' 'unsafe-inline'") {
			t.Errorf("%s allows a script that is not its own: %s", name, csp)
		}
		if name == "/key" && !strings.Contains(rec.Header().Get("Permissions-Policy"), "camera=(self)") {
			t.Error("/key cannot ask for the camera to scan a key")
		}
		if rec.Header().Get("Referrer-Policy") != "no-referrer" {
			t.Errorf("%s sends a referrer", name)
		}
	}

	// A fresh browser, with the words typed the way a person would.
	fresh := &browser{t: t, h: b.h}
	typed, _ := json.Marshal(map[string]string{"key": " " + strings.ToUpper(strings.Join(words, ",\n")) + " "})
	var back struct{ Name string }
	if code := fresh.post("/api/key/restore", string(typed), &back); code != http.StatusOK {
		t.Fatalf("restore: %d", code)
	}
	if back.Name != titled(g.Pond.Frog.Name) {
		t.Errorf("restore named %q, want %q", back.Name, titled(g.Pond.Frog.Name))
	}
	var seen greeting
	fresh.post("/api/greet", "{}", &seen)
	if seen.First || seen.Pond.Frog.Seed != g.Pond.Frog.Seed {
		t.Errorf("the fresh browser got another frog: %+v", seen.Pond.Frog)
	}

	// Changing the lock in the first browser shuts the second one's key.
	if code := b.post("/api/key/rotate", "{}", nil); code != http.StatusOK {
		t.Fatalf("rotate: %d", code)
	}
	if b.cookie.Value == key {
		t.Error("the lock changed and the cookie did not")
	}
	stale := &browser{t: t, h: b.h}
	if code := stale.post("/api/key/restore", string(typed), nil); code != http.StatusNotFound {
		t.Errorf("the old key after a change of lock: %d, want 404", code)
	}
	var after greeting
	b.post("/api/greet", "{}", &after)
	if after.Pond.Frog.Seed != g.Pond.Frog.Seed {
		t.Error("the frog changed with the lock")
	}

	for body, want := range map[string]int{
		`{"key":"moss fern"}`:                          http.StatusBadRequest,
		`{"key":"moss fern reed mud newt zzzzzz"}`:     http.StatusBadRequest,
		`{"key":"moss fern reed newt pike rain"}`:      http.StatusNotFound,
		`{"key":"moss fern reed newt pike rain lily"}`: http.StatusBadRequest,
	} {
		if code := stale.post("/api/key/restore", body, nil); code != want {
			t.Errorf("restore %s: %d, want %d", body, code, want)
		}
	}
}

func TestRestoreIsRateLimited(t *testing.T) {
	b, _, _ := testSite(t)
	try := func(ip string) int {
		return b.do(http.MethodPost, "/api/key/restore", `{"key":"moss fern reed newt pike rain"}`, "CF-Connecting-IP", ip).Code
	}
	for i := range 10 {
		if code := try(fmt.Sprintf("2001:db8:1:2::%d", i+1)); code != http.StatusNotFound {
			t.Fatalf("try %d: %d", i+1, code)
		}
	}
	// Another address in the same /64 is the same caller.
	if code := try("2001:db8:1:2:ffff::1"); code != http.StatusTooManyRequests {
		t.Errorf("the eleventh try from one network: %d", code)
	}
	if code := try("2001:db8:1:3::1"); code != http.StatusNotFound {
		t.Errorf("a different network was limited too: %d", code)
	}
}

func TestPanelIsTheVisitorsOwnFrog(t *testing.T) {
	b, _, _ := testSite(t)
	var house panel
	_ = json.Unmarshal(b.do(http.MethodGet, "/api/panel", "").Body.Bytes(), &house)
	if !house.House || house.Frog.Seed != houseSeed || house.Frog.Stage == "egg" {
		t.Errorf("with no cookie: %+v", house)
	}
	var g greeting
	b.post("/api/greet", "{}", &g)
	var mine panel
	rec := b.do(http.MethodGet, "/api/panel", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &mine)
	if mine.House || mine.Frog.Seed != g.Pond.Frog.Seed || mine.Frog.Name == "" {
		t.Errorf("with a cookie: %+v", mine)
	}
	if mine.Pebbles != g.Pond.Pebbles || len(mine.Todo) == 0 || mine.Todo[0].Kind != "wheel" {
		t.Errorf("the panel's pebbles and todo: %+v", mine)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("the panel answered cross origin with no Origin sent")
	}

	rec = b.do(http.MethodGet, "/api/panel", "", "Origin", "https://evil.example")
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("the panel answered an origin it doesn't know")
	}
	for _, o := range []string{"https://dash.bythewood.me", "https://status.bythewood.me"} {
		rec = b.do(http.MethodGet, "/api/panel", "", "Origin", o)
		if rec.Header().Get("Access-Control-Allow-Origin") != o || rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
			t.Errorf("%s wasn't allowed the visitor's frog: %v", o, rec.Header())
		}
	}
}

func TestAFrogFindsASitesHatOnce(t *testing.T) {
	b, _, clock := testSite(t)
	var g greeting
	b.post("/api/greet", "{}", &g)
	ask := func(origin string) panel {
		var p panel
		_ = json.Unmarshal(b.do(http.MethodGet, "/api/panel", "", "Origin", origin).Body.Bytes(), &p)
		return p
	}
	if p := ask("https://evil.example"); p.Found != nil {
		t.Errorf("a site the pond doesn't know handed out a hat: %+v", p.Found)
	}
	p := ask("https://repos.bythewood.me")
	if p.Found == nil || p.Found.Name != "Forked twig" || p.Found.Host != "repos.bythewood.me" {
		t.Fatalf("first time on repos: %+v", p.Found)
	}
	if p := ask("https://repos.bythewood.me"); p.Found == nil {
		t.Error("a second frog on the same page didn't hear about the hat")
	}
	*clock = clock.Add(2 * visitNews)
	if p := ask("https://repos.bythewood.me"); p.Found != nil {
		t.Error("repos' hat was still news later on")
	}
	var v view
	if code := b.post("/api/hats/wear", `{"shape":205,"colour":0}`, &v); code != http.StatusOK || v.Frog.Hat.Shape != 205 {
		t.Errorf("wearing the hat repos gave: %d %+v", code, v.Frog.Hat)
	}
	if code := b.post("/api/hats/wear", `{"shape":201,"colour":0}`, nil); code == http.StatusOK {
		t.Error("wore dash's hat without going there")
	}
	got := 0
	for _, st := range v.Sites {
		if st.Got {
			got++
		}
	}
	if got != 1 || len(v.Sites) != len(siteHats) {
		t.Errorf("sites on the view: %+v", v.Sites)
	}
	for i := range siteHats {
		if h := siteHatFor(i); h.tier() != 0 || !h.earned() {
			t.Errorf("%s should be common and worn, never held", h.name())
		}
	}
	if pw := powerOf(rarity{N: 2}, []hat{siteHatFor(0)}, nil, 0, 0); pw.Hats != 20 {
		t.Errorf("a site hat counts %d power, want 20", pw.Hats)
	}
}

// One pass through everything a visit can do, over HTTP.
func TestAVisitEndToEnd(t *testing.T) {
	b, db, clock := testSite(t)
	var g greeting
	b.post("/api/greet", "{}", &g)

	var sp spun
	if code := b.post("/api/wheel/spin", `{"paid":false}`, &sp); code != http.StatusOK {
		t.Fatalf("free spin: %d", code)
	}
	if sp.Slice < 0 || sp.Slice >= len(wheelSlices) || sp.Prize.Text == "" && sp.Prize.Hat == nil {
		t.Errorf("spin: %+v", sp)
	}
	if code := b.post("/api/wheel/spin", `{"paid":false}`, nil); code != http.StatusConflict {
		t.Errorf("a second free spin straight away: %d", code)
	}

	var lg turned
	if code := b.post("/api/log", `{"pick":2}`, &lg); code != http.StatusOK {
		t.Fatalf("log: %d", code)
	}
	if code := b.post("/api/log", `{"pick":7}`, nil); code != http.StatusBadRequest {
		t.Errorf("log 7: %d", code)
	}

	var run diving
	b.post("/api/dive/start", "{}", &run)
	*clock = clock.Add(40 * time.Second)
	var dv dived
	body, _ := json.Marshal(map[string]any{"run": run.Run, "score": 18, "gates": 12})
	if code := b.post("/api/dive/finish", string(body), &dv); code != http.StatusOK || dv.Paid != 36 || !dv.Best {
		t.Errorf("dive: %d %+v", code, dv)
	}

	if _, err := db.w.Exec(`UPDATE ponds SET pebbles = 500`); err != nil {
		t.Fatal(err)
	}
	var bx opened
	if code := b.post("/api/hats/box", "{}", &bx); code != http.StatusOK || bx.Prize.Hat == nil || bx.Prize.HatName == "" {
		t.Fatalf("box: %d %+v", code, bx)
	}
	if bx.Pond.Pebbles != 500-boxCost {
		t.Errorf("a box left %d pebbles", bx.Pond.Pebbles)
	}
	worn, _ := json.Marshal(bx.Prize.Hat)
	var v view
	if code := b.post("/api/hats/wear", string(worn), &v); code != http.StatusOK || v.Frog.Hat != *bx.Prize.Hat {
		t.Errorf("wear: %d %+v", code, v.Frog.Hat)
	}
	// The wheel can hand out the same hat first, and then the box's copy is a spare.
	if code := b.post("/api/hats/sell", string(worn), nil); !bx.Prize.Spare && code != http.StatusConflict {
		t.Errorf("sold the only copy of a hat: %d", code)
	}
	if code := b.post("/api/hats/wear", `{"shape":99,"colour":0}`, nil); code != http.StatusBadRequest {
		t.Errorf("wore hat 99: %d", code)
	}
	if code := b.post("/api/geode", "{}", nil); code != http.StatusConflict && code != http.StatusOK {
		t.Errorf("geode: %d", code)
	}

	rec := b.do(http.MethodGet, "/api/pond", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"slices"`) {
		t.Errorf("pond: %d", rec.Code)
	}
}

func TestShareTheFrog(t *testing.T) {
	b, _, _ := testSite(t)
	var g greeting
	if code := b.post("/api/greet", "{}", &g); code != http.StatusOK {
		t.Fatalf("greet: %d", code)
	}
	path := g.Pond.Share.Path
	if !strings.HasPrefix(path, "/f/") || g.Pond.Share.Power.Total != g.Pond.Share.Power.Frog {
		t.Fatalf("share: %+v", g.Pond.Share)
	}

	// The page is the same for whoever opens it, cookie or not.
	key := b.cookie.Value
	b.cookie = nil
	rec := b.do(http.MethodGet, path, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `property="og:image" content="`+baseURL+path+"/card.png?p=") {
		t.Fatalf("share page: %d\n%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), strings.Split(key, "-")[0]+"-") {
		t.Error("the share page carries a key")
	}
	rec = b.do(http.MethodGet, path+"/card.png", "")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" || !strings.HasPrefix(rec.Body.String(), "\x89PNG") {
		t.Errorf("card: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	code := path[strings.LastIndexByte(path, '-')+1:]
	if rec = b.do(http.MethodGet, "/f/somebody-else-"+code, ""); rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != path {
		t.Errorf("an old name: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	for _, bad := range []string{"/f/pip-0000000000000000", "/f/pip", "/f/pip-0000000000000000/card.png", "/f/pip-ZZZZ"} {
		if rec = b.do(http.MethodGet, bad, ""); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d", bad, rec.Code)
		}
	}
}

func TestPowerAddsUp(t *testing.T) {
	p := powerOf(rarity{N: 32}, []hat{{3, hatGolden}, {8, 0}}, map[string]bool{"t_acorn": true, "t_pearl": true, "moonstone": true, "k_frost": true}, 23, 0)
	if p.Frog != 600 || p.Hats != 920 || p.Trinkets != 410 || p.TrinketsFound != 2 || p.Rocks != 800 || p.Keepsakes != 50 || p.Dive != 575 || p.Total != 3355 || p.Rank != "POND REGULAR" {
		t.Errorf("power: %+v", p)
	}
	if p.Next != 5000-3355 || p.NextName != "LILY LORD" {
		t.Errorf("the next rung: %d more to %s", p.Next, p.NextName)
	}
	if p := powerOf(rarity{N: 10}, nil, nil, 0, 0); p.Rank != "NEW TO THE POND" {
		t.Errorf("a new frog's rank: %+v", p)
	}
	if p := powerOf(rarity{N: 10}, nil, nil, 500, 0); p.Dive != diveCounts*divePower {
		t.Errorf("a best Dive of 500 counts %d", p.Dive)
	}
}

// Everything there is to find, on the rarest frog an egg can hatch.
func fullPower() power {
	var held []hat
	for _, hs := range hatsByTier {
		held = append(held, hs...)
	}
	for i := range jobs {
		held = append(held, hoodFor(i))
	}
	for i := range siteHats {
		held = append(held, siteHatFor(i))
	}
	had := map[string]bool{}
	for _, x := range trinkets {
		had[x.kind] = true
	}
	for _, x := range rocks {
		had[x.kind] = true
	}
	for _, x := range keepsakes {
		had[x.kind] = true
	}
	return powerOf(rarity{N: 5600}, held, had, diveCounts, len(jobs)*maxLevel)
}

func TestTheTopRungIsTheTop(t *testing.T) {
	p := fullPower()
	top := ranks[len(ranks)-1]
	if p.Rank != top.name || p.Next != 0 || p.NextName != "" || p.Fill != 100 {
		t.Errorf("a full collection: %d, %s, %d more to %q, bar at %d", p.Total, p.Rank, p.Next, p.NextName, p.Fill)
	}
	// The top takes most of a full collection and can still be reached.
	if top.at < p.Total*6/10 || top.at > p.Total*9/10 {
		t.Errorf("the top rung is %d and a full collection is %d", top.at, p.Total)
	}
	for i := 1; i < len(ranks); i++ {
		if ranks[i].at <= ranks[i-1].at {
			t.Errorf("rung %s isn't above %s", ranks[i].name, ranks[i-1].name)
		}
		q := power{Total: ranks[i].at}
		if q.climb(); q.Next < 0 || q.Rank != ranks[i].name {
			t.Errorf("at %d: %s with %d to go", ranks[i].at, q.Rank, q.Next)
		}
	}
}

func TestOnlyTheKeyPageHasAFooter(t *testing.T) {
	b, _, _ := testSite(t)
	if body := b.do(http.MethodGet, "/", "").Body.String(); strings.Contains(body, "<footer") {
		t.Error("the game page has a footer")
	}
	if body := b.do(http.MethodGet, "/key", "").Body.String(); !strings.Contains(body, "<footer") {
		t.Error("the key page lost its footer")
	}
}
