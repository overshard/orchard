// Package suite is the look the tool sites share, one stylesheet, the Geist
// fonts, the header and status line templates and the spark drawing. Every
// copy is byte identical and make check holds it to that.
package suite

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

//go:embed files templates.html
var embedded embed.FS

var sums = map[string]string{}

func init() {
	entries, err := fs.ReadDir(embedded, "files")
	if err != nil {
		panic(err)
	}
	for _, e := range entries {
		b, err := fs.ReadFile(embedded, "files/"+e.Name())
		if err != nil {
			panic(err)
		}
		h := sha256.Sum256(b)
		sums[e.Name()] = hex.EncodeToString(h[:6])
	}
}

// URL is what a page links a suite file by. The hash changes with the file,
// so the file can be cached forever.
func URL(name string) string {
	return "/suite/" + name + "?v=" + sums[name]
}

// Handler serves the files under /suite/.
func Handler() http.Handler {
	sub, _ := fs.Sub(embedded, "files")
	files := http.StripPrefix("/suite/", http.FileServerFS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/suite/")
		if name == "index.json" {
			serveIndex(w, r)
			return
		}
		sum, ok := sums[name]
		if !ok {
			// Without this the edge stamps its own TTL on the 404 and holds it.
			w.Header().Set("Cache-Control", "no-store")
			http.NotFound(w, r)
			return
		}
		// The stylesheet asks for the fonts without a hash, so those get a day.
		if r.URL.Query().Get("v") == sum {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=86400")
		}
		files.ServeHTTP(w, r)
	})
}

// Funcs has to be on a template before Parse, since templates.html calls them.
func Funcs() template.FuncMap {
	return template.FuncMap{
		"suiteURL":   URL,
		"faviconURL": FaviconURL,
		"spark":      Spark,
		"sparkT":     SparkT,
		"tools":      func() []Tool { return Tools },
		"nothing":    Nothing,
	}
}

// Empty is what suite-empty draws where a list has nothing in it yet.
type Empty struct{ Label, Title, Text, Href, Action string }

// Nothing takes the label, the bold line and the sentence under it, then a
// link and its words when there's one obvious thing to do.
func Nothing(label, title, text string, action ...string) Empty {
	e := Empty{Label: label, Title: title, Text: text}
	if len(action) == 2 {
		e.Href, e.Action = action[0], action[1]
	}
	return e
}

// Parse adds the shared templates, suite-top, suite-status, suite-foot and the
// rest, to a site's template set.
func Parse(t *template.Template) (*template.Template, error) {
	return t.ParseFS(embedded, "templates.html")
}

// With adds the shared templates to a site's own as suite.html, for a renderer
// that parses one file system.
func With(site fs.FS) fs.FS { return overlay{site} }

type overlay struct{ fs.FS }

func (o overlay) Open(name string) (fs.File, error) {
	if name == "suite.html" {
		return embedded.Open("templates.html")
	}
	return o.FS.Open(name)
}

// FuncsWith is a site's own template functions with the suite's added.
func FuncsWith(own template.FuncMap) template.FuncMap {
	out := Funcs()
	for k, v := range own {
		out[k] = v
	}
	return out
}

type Tool struct{ Name, URL string }

// Tools is the order of the tabs along the top of every tool site.
var Tools = []Tool{
	{"auth", "https://auth.bythewood.me/"},
	{"status", "https://status.bythewood.me/"},
	{"logging", "https://logging.bythewood.me/"},
	{"analytics", "https://analytics.bythewood.me/"},
	{"cron", "https://cron.bythewood.me/"},
	{"llm", "https://llm.bythewood.me/"},
	{"repos", "https://repos.bythewood.me/"},
	{"mcp", "https://mcp.bythewood.me/"},
	{"gate", "https://gate.bythewood.me/"},
	{"dns", "https://dns.bythewood.me/"},
	{"pass", "https://pass.bythewood.me/"},
}

// Page is what the chrome templates read, one per rendered page.
type Page struct {
	Tool     string
	SignedIn bool
	Tabs     []Tab
	Meta     string
	Status   []Seg
	Right    []Seg
	Source   string
	Year     int
	Author   string
}

// Ways fills the three ways in band and the run your own band on a home page.
type Ways struct {
	Host                  string
	APITitle, APIText     string
	API                   template.HTML
	AgentTitle, AgentText string
	Agent                 template.HTML
	RunText               string
}

// Footer is the four column footer on a home page.
type Footer struct {
	Tool, About, Source, Author string
	Year                        int
	Pages                       []Tab
}

type Tab struct {
	Name, Href string
	On         bool
}

// Seg is one piece of the status line. Kind is ok, warn, bad or empty.
type Seg struct{ Text, Kind string }

func (p Page) Mode() string { return strings.ToUpper(p.Tool) }

// Spark draws a trend the way dash draws one: a line, a soft fill to the
// floor and a dotted rule at the median, stretched to whatever box holds it.
// Kind is ok, warn, bad or flat and only picks the colour.
func Spark(kind string, vals []float64) template.HTML {
	return spark(kind, vals, "")
}

// SparkT is Spark carrying its values and a label per point, so suite.js can
// move a crosshair across a row of tiles. Labels can be nil.
func SparkT(kind string, vals []float64, labels []string) template.HTML {
	nums := make([]string, len(vals))
	for i, v := range vals {
		nums[i] = strconv.FormatFloat(v, 'g', 6, 64)
	}
	data := ` data-v="` + strings.Join(nums, ",") + `"`
	if len(labels) == len(vals) {
		b, _ := json.Marshal(labels)
		data += ` data-l="` + template.HTMLEscapeString(string(b)) + `"`
	}
	return spark(kind, vals, data)
}

func spark(kind string, vals []float64, data string) template.HTML {
	if len(vals) < 2 {
		return ""
	}
	sorted := slices.Clone(vals)
	slices.Sort(sorted)
	base := sorted[len(sorted)/2]
	lo, hi := sorted[0], sorted[len(sorted)-1]
	if hi-lo < 1e-9 {
		lo, hi = lo-1, hi+1
	}
	const h, pad = 32.0, 1.5
	x := func(i int) float64 { return float64(i) / float64(len(vals)-1) * 100 }
	y := func(v float64) float64 { return pad + (1-(v-lo)/(hi-lo))*(h-2*pad) }
	var line strings.Builder
	for i, v := range vals {
		cmd := "L"
		if i == 0 {
			cmd = "M"
		}
		fmt.Fprintf(&line, "%s%.2f,%.2f ", cmd, x(i), y(v))
	}
	d := strings.TrimSpace(line.String())
	return template.HTML(fmt.Sprintf(`<svg class="o-spark" data-k="%s"%s viewBox="0 0 100 32" preserveAspectRatio="none" aria-hidden="true">`+
		`<path class="a" d="%s L100,32 L0,32 Z"/><line class="b" x1="0" x2="100" y1="%.2f" y2="%.2f"/><path class="l" d="%s"/></svg>`,
		template.HTMLEscapeString(kind), data, d, y(base), y(base), d))
}
