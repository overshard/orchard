package main

import (
	"strings"
	"testing"
)

func TestCodeBlocksReadsFilesAndFences(t *testing.T) {
	md := "Here it is.\n\n**app.py**\n\n```python\nprint(1)\n```\n\n## Run it\n\n```sh\npython app.py\n```\n"
	blocks := codeBlocks(md)
	if len(blocks) != 2 {
		t.Fatalf("want 2 blocks, got %d", len(blocks))
	}
	if blocks[0].File != "app.py" || blocks[0].Lang != "python" || blocks[0].Code != "print(1)" {
		t.Errorf("first block wrong: %+v", blocks[0])
	}
	// The bold line above the second one is a heading and not a file name.
	if blocks[1].File != "" || blocks[1].Lang != "sh" {
		t.Errorf("second block wrong: %+v", blocks[1])
	}
	for _, b := range blocks {
		if !b.Closed {
			t.Errorf("block %q should be closed", b.Lang)
		}
	}
}

func TestCodeBlocksSpotsATruncatedOne(t *testing.T) {
	blocks := codeBlocks("```go\nfunc main() {\n")
	if len(blocks) != 1 || blocks[0].Closed {
		t.Fatalf("an unclosed fence should come back open: %+v", blocks)
	}
	checks := checkCode(blocks)
	if len(checks) != 1 || checks[0].OK {
		t.Fatalf("a truncated block should fail: %+v", checks)
	}
	if !checks[0].Truncated {
		t.Error("a block that ran out of room should be marked truncated")
	}
	// Running out of room is not something a second search fixes.
	if codeFailed(checks, nil) {
		t.Error("truncation should not trigger a re-search")
	}
}

func TestStripCodeCitationsLeavesIndexesAlone(t *testing.T) {
	md := "Reads the file [2].\n\n```python\nrows = data[0]  # [3]\nprint(rows) [4]\n```\n\nAnd that is it [5].\n"
	got := stripCodeCitations(md)
	if !strings.Contains(got, "rows = data[0]") {
		t.Error("an array index inside code was removed")
	}
	if strings.Contains(got, "print(rows) [4]") {
		t.Error("a trailing citation inside code survived")
	}
	if !strings.Contains(got, "Reads the file [2].") || !strings.Contains(got, "that is it [5].") {
		t.Error("prose citations were removed")
	}
}

func TestProseOnlyDropsTheCode(t *testing.T) {
	md := "One sentence [1].\n\n```python\nassert x == 1\n```\n\nAnother [2].\n"
	got := proseOnly(md)
	if strings.Contains(got, "assert") {
		t.Errorf("code survived into the prose: %q", got)
	}
	if !strings.Contains(got, "One sentence [1].") || !strings.Contains(got, "Another [2].") {
		t.Errorf("prose was lost: %q", got)
	}
}

func TestTidyCitationsSkipsCode(t *testing.T) {
	md := "```python\nprint(a[0], b[0], c[0])\n```\n"
	if got := tidyCitations(md); got != md {
		t.Errorf("code was rewritten:\n%s", got)
	}
}

func TestRenderDoesNotLinkifyInsideCode(t *testing.T) {
	html := renderMarkdown("Prose [1].\n\n```python\nx = rows[0]\n```\n")
	if strings.Contains(html, `data-passage="0"`) {
		t.Errorf("an index in a code block became a citation link:\n%s", html)
	}
	if !strings.Contains(html, `data-passage="1"`) {
		t.Errorf("the prose citation was not linked:\n%s", html)
	}
}

func TestGoSyntaxCatchesABrokenFile(t *testing.T) {
	good := CodeBlock{Lang: "go", Closed: true, Code: "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"}
	if note := inspect(good); note != "" {
		t.Errorf("good Go was rejected: %s", note)
	}
	// A snippet with no package clause is still valid Go to show somebody.
	snippet := CodeBlock{Lang: "go", Closed: true, Code: "func add(a, b int) int { return a + b }"}
	if note := inspect(snippet); note != "" {
		t.Errorf("a snippet was rejected: %s", note)
	}
	bad := CodeBlock{Lang: "go", Closed: true, Code: "package main\n\nfunc main() {\n\tprintln(\"hi\"\n}\n"}
	if note := inspect(bad); note == "" {
		t.Error("a missing paren was not caught")
	}
}

func TestBalanceIgnoresBracketsInStringsAndComments(t *testing.T) {
	code := strings.Join([]string{
		`# a comment with an unmatched ) in it`,
		`msg = "a string with ( and [ inside"`,
		`other = 'and ) here'`,
		`doc = """`,
		`  a docstring with { unmatched`,
		`"""`,
		`print(msg)`,
	}, "\n")
	if note := balance(code, pythonScan); note != "" {
		t.Errorf("false alarm on valid Python: %s", note)
	}
}

func TestBalanceCatchesARealUnclosedBracket(t *testing.T) {
	if note := balance("def f():\n    return g(1, 2\n", pythonScan); note == "" {
		t.Error("an unclosed call was not caught")
	}
	if note := balance("x = 1)\n", pythonScan); !strings.Contains(note, "closes nothing") {
		t.Errorf("a stray close was not caught, got %q", note)
	}
}

func TestPythonIndentCatchesMixedWhitespace(t *testing.T) {
	if note := pythonIndent("def f():\n    return 1\n\ndef g():\n\treturn 2\n"); note == "" {
		t.Error("mixed tabs and spaces were not caught")
	}
	if note := pythonIndent("def f():\n    return 1\n"); note != "" {
		t.Errorf("consistent spaces were flagged: %s", note)
	}
}

func TestDockerfileSyntax(t *testing.T) {
	ok := "# comment\nARG VERSION=1\nFROM ollama/ollama:latest\nENV OLLAMA_HOST=0.0.0.0\nRUN apt-get update \\\n    && apt-get install -y curl\nEXPOSE 11434\n"
	if note := dockerfileSyntax(ok); note != "" {
		t.Errorf("a good Dockerfile was rejected: %s", note)
	}
	if note := dockerfileSyntax("RUN echo hi\n"); note == "" {
		t.Error("an instruction before FROM was allowed")
	}
	if note := dockerfileSyntax("FROM alpine\nINSTALL curl\n"); !strings.Contains(note, "INSTALL") {
		t.Errorf("an invented instruction was allowed, got %q", note)
	}
	if note := dockerfileSyntax("# just a comment\n"); note == "" {
		t.Error("a Dockerfile with no FROM was allowed")
	}
}

func TestHTMLBalance(t *testing.T) {
	page := `<!doctype html><html><head><meta charset="utf-8"><title>x</title></head>` +
		`<body><div class="map"><p>hello<br><img src="a.png"></div><script>var a = 1;</script></body></html>`
	if note := htmlBalance(page); note != "" {
		t.Errorf("a good page was rejected: %s", note)
	}
	if note := htmlBalance("<html><body><div>stopped here"); note == "" {
		t.Error("a truncated page was not caught")
	}
}

func TestPlaceholderDetection(t *testing.T) {
	cases := map[string]bool{
		"def f():\n    ...\n":                              true,
		"# rest of the code here\n":                        true,
		"app.run()  # TODO: implement caching\n":           true,
		"headers = {}  # your code here\n":                 true,
		"# This is a conceptual example of the command.\n": true,
		"# In a real environment, this would invoke it.\n": true,
		"x = data[...]\n":                                  false,
		"print('this has an ellipsis in a string ...')\n":  false,
	}
	for code, want := range cases {
		if got := placeholderIn(code) != ""; got != want {
			t.Errorf("placeholderIn(%q) = %v, want %v", code, got, want)
		}
	}
}

func TestNormalLangFallsBackToTheFileName(t *testing.T) {
	if got := normalLang("", "server.py"); got != "python" {
		t.Errorf("got %q", got)
	}
	if got := normalLang("", "Dockerfile"); got != "dockerfile" {
		t.Errorf("got %q", got)
	}
	if got := normalLang("yml", ""); got != "yaml" {
		t.Errorf("got %q", got)
	}
}

func TestCodeWarningsOnlyNamesFailures(t *testing.T) {
	checks := []CodeCheck{
		{File: "app.py", Lang: "python", OK: true, Note: "brackets balance"},
		{File: "", Lang: "go", OK: false, Note: "does not parse: 3:1 expected }"},
	}
	warns := codeWarnings(checks)
	if len(warns) != 1 {
		t.Fatalf("want one warning, got %v", warns)
	}
	if !strings.Contains(warns[0], "the go block") {
		t.Errorf("an unnamed block should be described by its language, got %q", warns[0])
	}
}

func TestCodeWeightPrefersPagesWithCode(t *testing.T) {
	docs := &Page{Site: "docs.docker.com", Markdown: "text\n```sh\ndocker run\n```\n"}
	blog := &Page{Site: "someblog.example", Markdown: "no code here at all"}
	if codeWeight(docs) <= codeWeight(blog) {
		t.Errorf("docs page scored %d, blog scored %d", codeWeight(docs), codeWeight(blog))
	}
}

func TestFileNameWithAPrefix(t *testing.T) {
	blocks := codeBlocks("**File: world-heat-map.html**\n\n```html\n<p>hi</p>\n```\n")
	if len(blocks) != 1 || blocks[0].File != "world-heat-map.html" {
		t.Fatalf("got %+v", blocks)
	}
}

func TestFileInComment(t *testing.T) {
	blocks := codeBlocks("```python\n# restic_daily_backup.py\nimport os\n```\n")
	if len(blocks) != 1 || blocks[0].File != "restic_daily_backup.py" {
		t.Fatalf("a file name in a leading comment was not picked up: %+v", blocks)
	}
	// The comment goes with it, so the line count matches what the reader sees.
	if blocks[0].Code != "import os" {
		t.Errorf("the comment was left in the code: %q", blocks[0].Code)
	}
	// A first line that is a real comment is not a file name.
	plain := codeBlocks("```python\n# read the config\nimport os\n```\n")
	if plain[0].File != "" {
		t.Errorf("a comment was read as a file name: %q", plain[0].File)
	}
}

func TestDropMetaTakesTheOpenerAndLeavesTheCode(t *testing.T) {
	md := "This response provides a Dockerfile to run Ollama, citing the relevant passages.\n\n```dockerfile\nFROM ollama/ollama\n```\n"
	got := dropMeta(md)
	if strings.Contains(got, "This response") {
		t.Errorf("meta sentence survived:\n%s", got)
	}
	if !strings.Contains(got, "FROM ollama/ollama") {
		t.Errorf("code was lost:\n%s", got)
	}
	// A sentence mid-line goes without taking the useful half with it.
	mixed := dropMeta("Flask serves the cache. This answer uses two files.")
	if mixed != "Flask serves the cache." {
		t.Errorf("got %q", mixed)
	}
}

func TestDropMissingFieldsSkipsCode(t *testing.T) {
	md := "```python\nvalue = \"not provided\"\n```\n"
	if got := dropMissingFields(md); !strings.Contains(got, "not provided") {
		t.Errorf("a code line was dropped:\n%s", got)
	}
	if got := dropMissingFields("Total Time: Not specified\n"); got != "" {
		t.Errorf("a missing field line survived: %q", got)
	}
}

func TestUnusedConstants(t *testing.T) {
	dead := []CodeBlock{{Lang: "python", File: "api.py", Closed: true,
		Code: "CACHE_AGE = 3600\nPORT = 5000\n\ndef main():\n    app.run(port=PORT)\n"}}
	warns := unusedConstants(dead)
	if len(warns) != 1 || !strings.Contains(warns[0], "CACHE_AGE") {
		t.Fatalf("want one warning about CACHE_AGE, got %v", warns)
	}
}

// The failure this was written for: a package name with a dot in it is not a
// sentence boundary, and treating it as one opened an answer mid-word.
func TestDropMetaDoesNotSplitOnADottedName(t *testing.T) {
	in := "This response creates an HTML page using the `leaflet.webgl-temperature-map` library. It includes sample data."
	got := dropMeta(in)
	if got != "It includes sample data." {
		t.Errorf("got %q", got)
	}
}

// The failure that produced a complete looking page whose tooltip showed a
// hardcoded 50: the model argued with its sources in a comment and then wrote
// a stand-in value.
func TestMetaInCodeFailsTheBlock(t *testing.T) {
	code := strings.Join([]string{
		"// The provided text does not explicitly define a getValue method,",
		"// so we simulate a value for the purpose of the demo.",
		"var simulatedValue = 50;",
	}, "\n")
	if note := metaInCode(code); note == "" {
		t.Error("a comment reasoning about the sources was not caught")
	}
	if note := metaInCode("// cache the result so the page does not refetch\n"); note != "" {
		t.Errorf("an ordinary comment was flagged: %s", note)
	}
}

func TestStripCodeCitationsCleansComments(t *testing.T) {
	md := "```js\n// see the docs [1] for the rest\nvar x = [1];\nvar y = data[12];\n```\n"
	got := stripCodeCitations(md)
	if strings.Contains(got, "docs [1]") {
		t.Error("a citation in a comment survived")
	}
	if !strings.Contains(got, "var x = [1];") || !strings.Contains(got, "data[12]") {
		t.Errorf("real code was rewritten:\n%s", got)
	}
}

// The page that came back looking finished and rendered blank, because it
// called into a library nothing on the page had loaded.
func TestMissingLibrary(t *testing.T) {
	invented := `<html><body><div id="map"></div><script>const map = new SatMeteo.Map({zoom: 2});</script></body></html>`
	if note := missingLibrary(invented); note == "" {
		t.Error("a call into a library that is never loaded was not caught")
	}
	loaded := `<html><body><script src="https://unpkg.com/leaflet/dist/leaflet.js"></script>` +
		`<script>const map = L.map('map');</script></body></html>`
	if note := missingLibrary(loaded); note != "" {
		t.Errorf("a page that loads its library was flagged: %s", note)
	}
	plain := `<html><body><script>const box = document.getElementById('x'); box.textContent = JSON.stringify({a: 1});` +
		`const d = new Date(); const canvas = new Image();</script></body></html>`
	if note := missingLibrary(plain); note != "" {
		t.Errorf("a page using only browser globals was flagged: %s", note)
	}
	own := `<html><body><script>class Chart { draw() {} } const c = new Chart(); c.draw();</script></body></html>`
	if note := missingLibrary(own); note != "" {
		t.Errorf("a page defining its own class was flagged: %s", note)
	}
}

// The connection string that came back as f"DRIVER={IBMDB} ...", where IBMDB
// is a name the file never sets.
func TestUndefinedInFString(t *testing.T) {
	bad := "conn = ibm_db.connect(f\"DRIVER={IBMDB} HOST={DB_HOST}\")\nDB_HOST = 'localhost'\n"
	if note := undefinedInFString(bad); note == "" || !strings.Contains(note, "IBMDB") {
		t.Errorf("got %q", note)
	}
	good := "name = 'world'\nprint(f'hello {name}')\n"
	if note := undefinedInFString(good); note != "" {
		t.Errorf("a defined name was flagged: %s", note)
	}
	// An expression inside the braces is left alone, since it has too many
	// ways to be legal.
	expr := "print(f'{row.value:.1f} and {items[0]}')\n"
	if note := undefinedInFString(expr); note != "" {
		t.Errorf("an expression was flagged: %s", note)
	}
}

func TestLiftFileComments(t *testing.T) {
	md := "```python\n# app.py\nimport os\n```\n\n```sh\n# install it first\npip install flask\n```\n"
	got := liftFileComments(md)
	if strings.Contains(got, "# app.py") {
		t.Errorf("the file name comment was kept:\n%s", got)
	}
	if !strings.Contains(got, "# install it first") {
		t.Errorf("an ordinary first comment was removed:\n%s", got)
	}
	if !strings.Contains(got, "import os") {
		t.Errorf("code was lost:\n%s", got)
	}
}
