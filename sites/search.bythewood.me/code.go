package main

import (
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"regexp"
	"strings"
)

// The code shape is the one where the reader does not read the answer, they
// paste it, so the things worth checking are different. A sentence can be a
// little vague and still be useful. A file that does not parse is worth
// nothing, and neither is one with a hole in the middle where the model ran
// out of passages.
//
// Everything here is static and runs in process, because the image is FROM
// scratch and there is no interpreter in it to run anything against.

// CodeBlock is one fenced block lifted out of an answer.
type CodeBlock struct {
	Lang string
	// File is the bold file name on the line above the fence, when the model
	// wrote one, since the contract asks for **app.py** above each block.
	File string
	Code string
	Line int
	// Closed is false when the fence was never closed, which is what a block
	// truncated by the token budget looks like.
	Closed bool
}

// CodeCheck is one verdict, one per block, shown to the reader.
type CodeCheck struct {
	File  string
	Lang  string
	Lines int
	OK    bool
	Note  string
	// Truncated separates running out of tokens from writing something wrong.
	// Searching again cannot fix a length problem, so it is not worth the
	// three searches a second round costs.
	Truncated bool
}

var (
	fenceOpen = regexp.MustCompile("^\\s{0,3}```+\\s*([A-Za-z0-9_+#.-]*)\\s*$")
	fenceShut = regexp.MustCompile("^\\s{0,3}```+\\s*$")
	boldFile  = regexp.MustCompile(`^\s*\*\*([^*]+?)\*\*\s*:?\s*$`)
	// A file name is a name with an extension, or one of the few that carry
	// none. Without this the heading above a block ("**Run it**") is read as a
	// file name and the check panel lists a file nobody wrote.
	looksLikeFile = regexp.MustCompile(`^[\w./-]+\.[A-Za-z0-9]{1,10}$|^(Dockerfile|Makefile|Procfile|\.env|\.gitignore|docker-compose\.ya?ml)$`)
)

// codeBlocks pulls the fenced blocks out of an answer, keeping the file name
// written above each one.
func codeBlocks(md string) []CodeBlock {
	lines := strings.Split(md, "\n")
	var out []CodeBlock
	for i := 0; i < len(lines); i++ {
		m := fenceOpen.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		b := CodeBlock{Lang: strings.ToLower(m[1]), Line: i + 1, File: fileAbove(lines, i)}
		var body []string
		j := i + 1
		for ; j < len(lines); j++ {
			if fenceShut.MatchString(lines[j]) {
				b.Closed = true
				break
			}
			body = append(body, lines[j])
		}
		// The name comes off a leading comment when it was not written above
		// the fence, and the comment goes with it, so what is checked here is
		// what liftFileComments leaves for the reader to copy.
		if b.File == "" && len(body) > 0 {
			if name := fileInComment(body[0]); name != "" {
				b.File, body = name, body[1:]
			}
		}
		b.Code = strings.Join(body, "\n")
		out = append(out, b)
		i = j
	}
	return out
}

// fileAbove reads back over blank lines for the bold file name the contract
// asks for above each block.
func fileAbove(lines []string, fence int) string {
	for i := fence - 1; i >= 0 && i >= fence-3; i-- {
		t := strings.TrimSpace(lines[i])
		if t == "" {
			continue
		}
		m := boldFile.FindStringSubmatch(t)
		if m == nil {
			return ""
		}
		// "**File: server.py**" is as common as the bare name and means the
		// same thing.
		name := strings.TrimSpace(m[1])
		if i := strings.IndexByte(name, ':'); i >= 0 {
			name = strings.TrimSpace(name[i+1:])
		}
		if looksLikeFile.MatchString(name) {
			return name
		}
		return ""
	}
	return ""
}

// fileInComment reads a file name off a comment on the first line, which is
// where the model puts it about half the time whatever the contract says.
func fileInComment(line string) string {
	first := strings.TrimSpace(strings.SplitN(line, "\n", 2)[0])
	for _, marker := range []string{"#", "//", "--", "<!--"} {
		if !strings.HasPrefix(first, marker) {
			continue
		}
		name := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(first, marker)), "-->"))
		if looksLikeFile.MatchString(name) {
			return name
		}
	}
	return ""
}

// proseOnly is the answer with the code taken out.
//
// Validation checks a sentence against the passage it cites, and a line of
// Python is not a sentence: splitting it produces claims nothing can entail,
// each one a model call, and `data[0]` reads as a citation of passage 0. So
// the checker sees the prose and the code is checked by parsing it instead.
func proseOnly(md string) string {
	lines := strings.Split(md, "\n")
	var out []string
	skip := false
	for _, l := range lines {
		switch {
		case skip:
			if fenceShut.MatchString(l) || fenceOpen.MatchString(l) {
				skip = false
			}
		case fenceOpen.MatchString(l):
			skip = true
		default:
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// eachProseLine runs fn over the lines outside code blocks and keeps the rest
// byte for byte. Every tidy-up here is written for prose, and a line of Python
// that happens to match one of those patterns is not prose.
func eachProseLine(md string, fn func(string) (string, bool)) string {
	lines := strings.Split(md, "\n")
	out := make([]string, 0, len(lines))
	in := false
	for _, l := range lines {
		switch {
		case fenceOpen.MatchString(l) && !in:
			in = true
		case in && fenceShut.MatchString(l):
			in = false
		case !in:
			kept, ok := fn(l)
			if !ok {
				continue
			}
			l = kept
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// dropMeta removes a sentence that talks about the answer rather than
// answering. A 4B writes "This response provides a Dockerfile, citing the
// relevant passages" however plainly the contract forbids it, and it is the
// first line a reader sees.
//
// A sentence ends at a full stop with a space after it. Splitting on the stop
// alone cut "the `leaflet.webgl-temperature-map` library" in half and left the
// answer opening mid-word, which is worse than the sentence it removed.
var metaSentence = regexp.MustCompile(`(?i)(^|[.!?]\s+)(?:[^.!?]|[.!?]\S)*\b(?:this (?:response|answer|reply)|the following (?:response|answer)|as an ai|below (?:you will find|is the answer))\b(?:[^.!?]|[.!?]\S)*[.!?](\s+|$)`)

func dropMeta(text string) string {
	return eachProseLine(text, func(l string) (string, bool) {
		if !metaSentence.MatchString(l) {
			return l, true
		}
		cleaned := strings.TrimSpace(metaSentence.ReplaceAllString(l, "$1 "))
		// A line that was only meta goes entirely, and a list marker left on
		// its own goes with it.
		if cleaned == "" || cleaned == "-" || cleaned == "*" {
			return "", false
		}
		return cleaned, true
	})
}

// stripCodeCitations removes citation markers from inside code blocks.
//
// The prompt says not to put them there and a 4B does it anyway, and unlike a
// stray marker in prose this one is pasted into a file and stops it running.
// Prose keeps its markers, which is the whole point of them.
func stripCodeCitations(md string) string {
	lines := strings.Split(md, "\n")
	in := false
	for i, l := range lines {
		switch {
		case fenceOpen.MatchString(l) && !in:
			in = true
		case in && fenceShut.MatchString(l):
			in = false
		case in && citation.MatchString(l):
			// Only where it reads as a marker rather than as an index, so
			// `items[0]` and `x = [1]` survive and `print(x) [3]` does not.
			cleaned := trailingCitation.ReplaceAllString(l, "")
			if c := commentAt(cleaned); c >= 0 {
				cleaned = cleaned[:c] + citation.ReplaceAllString(cleaned[c:], "")
			}
			lines[i] = strings.TrimRight(cleaned, " \t")
		}
	}
	return strings.Join(lines, "\n")
}

// commentAt is where a line comment starts, or -1. Rough on purpose: it is
// used to decide whether a citation on this line is prose, and a "#" inside a
// string is not worth a lexer.
func commentAt(line string) int {
	best := -1
	for _, marker := range []string{"#", "//", "--", "/*", "<!--"} {
		if i := strings.Index(line, marker); i >= 0 && (best < 0 || i < best) {
			best = i
		}
	}
	return best
}

// A marker after code and before the end of the line, or after a comment
// character. An index is always attached to the name in front of it.
var trailingCitation = regexp.MustCompile(`(\s+\[\d{1,3}\])+\s*$`)

// liftFileComments removes the "# app.py" line from the top of a block whose
// name is already shown above it. The name belongs in one place, and inside
// the block it is a line the reader pastes into their own file.
func liftFileComments(md string) string {
	lines := strings.Split(md, "\n")
	out := make([]string, 0, len(lines))
	in, first := false, false
	for _, l := range lines {
		switch {
		case fenceOpen.MatchString(l) && !in:
			in, first = true, true
		case in && fenceShut.MatchString(l):
			in = false
		case in && first:
			first = false
			if fileInComment(l) != "" {
				continue
			}
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// checkCode parses what can be parsed and looks for the holes a model leaves
// when it is writing from fragments.
func checkCode(blocks []CodeBlock) []CodeCheck {
	var out []CodeCheck
	for _, b := range blocks {
		if strings.TrimSpace(b.Code) == "" {
			continue
		}
		c := CodeCheck{File: b.File, Lang: b.Lang, Lines: len(strings.Split(b.Code, "\n")), OK: true}
		if c.Lang == "" {
			c.Lang = "text"
		}
		switch {
		case !b.Closed:
			c.OK, c.Truncated = false, true
			c.Note = "ran out of room before the block ended, so it stops partway"
		default:
			if note := inspect(b); note != "" {
				c.OK, c.Note = false, note
			}
		}
		if c.OK {
			c.Note = passNote(b)
		}
		out = append(out, c)
	}
	return out
}

// inspect returns what is wrong with a block, or an empty string.
func inspect(b CodeBlock) string {
	if note := metaInCode(b.Code); note != "" {
		return note
	}
	if note := placeholderIn(b.Code); note != "" {
		return note
	}
	switch normalLang(b.Lang, b.File) {
	case "go":
		return goSyntax(b.Code)
	case "json":
		if !json.Valid([]byte(b.Code)) {
			return "not valid JSON"
		}
	case "python":
		if note := balance(b.Code, pythonScan); note != "" {
			return note
		}
		if note := pythonIndent(b.Code); note != "" {
			return note
		}
		return undefinedInFString(b.Code)
	case "javascript":
		return balance(b.Code, cLikeScan)
	case "html":
		if note := htmlBalance(b.Code); note != "" {
			return note
		}
		return missingLibrary(b.Code)
	case "yaml":
		return yamlTabs(b.Code)
	case "dockerfile":
		return dockerfileSyntax(b.Code)
	}
	return ""
}

// passNote says what was actually done, since "checked" with no detail invites
// the reader to think more was checked than was.
func passNote(b CodeBlock) string {
	switch normalLang(b.Lang, b.File) {
	case "go":
		return "parses"
	case "json":
		return "valid JSON"
	case "python", "javascript":
		return "brackets balance"
	case "html":
		return "tags balance"
	case "yaml":
		return "indented with spaces"
	case "dockerfile":
		return "instructions are real ones"
	}
	return "read, not parsed"
}

// normalLang folds the tags people write onto the ones checked here, and falls
// back to the file name when the fence carries no language.
func normalLang(lang, file string) string {
	switch lang {
	case "go", "golang":
		return "go"
	case "py", "python", "python3":
		return "python"
	case "js", "javascript", "jsx", "mjs", "node", "ts", "typescript", "tsx":
		return "javascript"
	case "json":
		return "json"
	case "html", "htm":
		return "html"
	case "yaml", "yml":
		return "yaml"
	case "dockerfile", "docker":
		return "dockerfile"
	}
	switch {
	case file == "":
		return lang
	case strings.HasPrefix(file, "Dockerfile"), file == "Dockerfile":
		return "dockerfile"
	case strings.HasSuffix(file, ".go"):
		return "go"
	case strings.HasSuffix(file, ".py"):
		return "python"
	case strings.HasSuffix(file, ".js"), strings.HasSuffix(file, ".ts"):
		return "javascript"
	case strings.HasSuffix(file, ".json"):
		return "json"
	case strings.HasSuffix(file, ".html"):
		return "html"
	case strings.HasSuffix(file, ".yml"), strings.HasSuffix(file, ".yaml"):
		return "yaml"
	}
	return lang
}

// goSyntax runs the real parser, which is the one language here that can be
// checked properly without leaving the standard library. A snippet is not a
// file, so a bare fragment gets wrapped before it is rejected.
func goSyntax(src string) string {
	tries := []string{src}
	if !strings.Contains(src, "package ") {
		tries = append(tries, "package p\n"+src)
		if !strings.Contains(src, "func ") {
			tries = append(tries, "package p\nfunc p() {\n"+src+"\n}")
		}
	}
	var first error
	for _, try := range tries {
		_, err := parser.ParseFile(token.NewFileSet(), "x.go", try, parser.AllErrors)
		if err == nil {
			return ""
		}
		if first == nil {
			first = err
		}
	}
	msg := first.Error()
	if i := strings.Index(msg, "\n"); i > 0 {
		msg = msg[:i]
	}
	return "does not parse: " + strings.TrimPrefix(msg, "x.go:")
}

// A model that cannot find how a library works argues with itself in a comment
// and then writes a stand-in value, which is the worst thing a code answer can
// do: it is complete, it runs, and it does nothing. Worth failing the block
// over, since the reader would have to read every comment to notice.
var metaComment = regexp.MustCompile(`(?i)(provided (?:text|passage|context|basic usage)|the passages?\b|in the (?:provided|given) \w+|based on the (?:provided|given)|not explicitly (?:defined|mentioned|in)|for the purpose of (?:the|this) demo|placeholder for)`)

func metaInCode(code string) string {
	if metaComment.MatchString(code) {
		return "argues about what the sources said in its own comments instead of doing the work"
	}
	return ""
}

var placeholders = []struct {
	re   *regexp.Regexp
	note string
}{
	{regexp.MustCompile(`(?im)^\s*(?:#|//|--|/\*|<!--)?\s*\.\.\.\s*(?:\*/|-->)?\s*$`), "has an ellipsis standing in for code that was not written"},
	{regexp.MustCompile(`(?i)(rest of (?:the |your )?(?:code|file|implementation)|your code here|code goes here|implement(?:ation)? (?:this|here|goes here)|same as (?:above|before)|remaining \w+ (?:here|omitted)|\bomitted for brevity\b|\betc\.\.\.)`), "says the rest of the code goes here rather than writing it"},
	{regexp.MustCompile(`(?i)TODO:?\s*(implement|fill|add your|complete)`), "leaves a TODO where working code should be"},
	// A file that describes itself as an example of what a real one would do
	// is not one, and it is what a model writes when the passages never showed
	// it the real thing.
	{regexp.MustCompile(`(?i)(conceptual (?:example|implementation)|in a real (?:environment|system|setup|deployment)|this is pseudo|pseudo-?code|for illustration only|does not actually|would actually (?:run|execute|invoke)|represents the logic)`), "describes what a working version would do rather than being one"},
}

func placeholderIn(code string) string {
	for _, p := range placeholders {
		if p.re.MatchString(code) {
			return p.note
		}
	}
	return ""
}

// scan describes how to skip the parts of a language where a bracket is not a
// bracket. Counting them naively calls every string holding a "(" unbalanced,
// which is a false alarm on almost every real file.
type scan struct {
	lineComment []string
	blockOpen   string
	blockClose  string
	quotes      []string
	// triples are Python's, and they have to be tried before the single
	// character quotes or the first two characters close an empty string.
	triples []string
	escape  bool
}

var (
	cLikeScan  = scan{lineComment: []string{"//"}, blockOpen: "/*", blockClose: "*/", quotes: []string{`"`, "'", "`"}, escape: true}
	pythonScan = scan{lineComment: []string{"#"}, triples: []string{`"""`, "'''"}, quotes: []string{`"`, "'"}, escape: true}
)

// balance walks the code outside strings and comments and reports the first
// bracket that does not close.
func balance(code string, s scan) string {
	var stack []byte
	pair := map[byte]byte{')': '(', ']': '[', '}': '{'}
	line := 1

	for i := 0; i < len(code); {
		c := code[i]
		if c == '\n' {
			line++
			i++
			continue
		}
		if skip, n := skipNonCode(code[i:], s); skip {
			for _, r := range code[i : i+n] {
				if r == '\n' {
					line++
				}
			}
			i += n
			continue
		}
		switch c {
		case '(', '[', '{':
			stack = append(stack, c)
		case ')', ']', '}':
			if len(stack) == 0 || stack[len(stack)-1] != pair[c] {
				return fmt.Sprintf("bracket mismatch on line %d, a %q closes nothing", line, string(c))
			}
			stack = stack[:len(stack)-1]
		}
		i++
	}
	if len(stack) > 0 {
		return fmt.Sprintf("%d bracket%s never closed, so the block is incomplete",
			len(stack), map[bool]string{true: "", false: "s"}[len(stack) == 1])
	}
	return ""
}

// skipNonCode reports how many bytes to skip when the text starts a comment or
// a string.
func skipNonCode(s string, sc scan) (bool, int) {
	for _, lc := range sc.lineComment {
		if strings.HasPrefix(s, lc) {
			if n := strings.IndexByte(s, '\n'); n >= 0 {
				return true, n
			}
			return true, len(s)
		}
	}
	if sc.blockOpen != "" && strings.HasPrefix(s, sc.blockOpen) {
		if n := strings.Index(s[len(sc.blockOpen):], sc.blockClose); n >= 0 {
			return true, len(sc.blockOpen) + n + len(sc.blockClose)
		}
		return true, len(s)
	}
	for _, t := range sc.triples {
		if strings.HasPrefix(s, t) {
			if n := strings.Index(s[len(t):], t); n >= 0 {
				return true, len(t) + n + len(t)
			}
			return true, len(s)
		}
	}
	for _, q := range sc.quotes {
		if !strings.HasPrefix(s, q) {
			continue
		}
		for i := len(q); i < len(s); i++ {
			if sc.escape && s[i] == '\\' {
				i++
				continue
			}
			// An unterminated string ends at the newline rather than eating
			// the whole file, which keeps one bad quote from hiding every
			// bracket after it.
			if s[i] == '\n' {
				return true, i
			}
			if strings.HasPrefix(s[i:], q) {
				return true, i + len(q)
			}
		}
		return true, len(s)
	}
	return false, 0
}

// fstring is an interpolated string and fslot a bare name inside one. Two
// alternatives rather than a back reference, which RE2 does not have. Only a
// plain identifier is read out, since an attribute, a call or a comprehension
// has too many ways to be defined elsewhere.
var (
	fstring = regexp.MustCompile(`\bf"([^"\n]*)"|\bf'([^'\n]*)'`)
	fslot   = regexp.MustCompile(`\{([A-Za-z_]\w*)\}`)
)

// undefinedInFString catches a name that appears only inside an f-string, which
// is a NameError the moment the line runs.
//
// It is the shape of mistake a model makes filling in a connection string it
// half remembers, and unlike a missing import nothing else in the file hints
// at it.
func undefinedInFString(code string) string {
	for _, m := range fstring.FindAllStringSubmatch(code, -1) {
		for _, slot := range fslot.FindAllStringSubmatch(m[1]+m[2], -1) {
			name := slot[1]
			if pyBuiltin[name] {
				continue
			}
			if strings.Count(code, name) > 1 {
				continue
			}
			return "uses " + name + " in an f-string and never defines it, so it fails as soon as it runs"
		}
	}
	return ""
}

var pyBuiltin = map[string]bool{
	"self": true, "cls": true, "True": true, "False": true, "None": true,
	"e": true, "i": true, "x": true,
}

// pythonIndent catches the one whitespace mistake that actually stops Python
// running, a file mixing tabs and spaces for its indentation.
func pythonIndent(code string) string {
	tabs, spaces := false, false
	for _, l := range strings.Split(code, "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		switch {
		case strings.HasPrefix(l, "\t"):
			tabs = true
		case strings.HasPrefix(l, " "):
			spaces = true
		}
	}
	if tabs && spaces {
		return "mixes tabs and spaces for indentation, which Python rejects"
	}
	return ""
}

func yamlTabs(code string) string {
	for i, l := range strings.Split(code, "\n") {
		if strings.HasPrefix(l, "\t") {
			return fmt.Sprintf("line %d is indented with a tab, which YAML does not allow", i+1)
		}
	}
	return ""
}

var (
	htmlTag  = regexp.MustCompile(`(?is)<(/?)([a-z][a-z0-9-]*)\b[^>]*?(/?)>`)
	voidTags = map[string]bool{
		"area": true, "base": true, "br": true, "col": true, "embed": true,
		"hr": true, "img": true, "input": true, "link": true, "meta": true,
		"param": true, "source": true, "track": true, "wbr": true,
		"!doctype": true,
	}
)

// htmlBalance is a tag counter rather than a parser. It only has to catch a
// page that stops halfway, which is what a truncated answer looks like.
func htmlBalance(code string) string {
	var stack []string
	for _, m := range htmlTag.FindAllStringSubmatch(code, -1) {
		closing, name, self := m[1] == "/", strings.ToLower(m[2]), m[3] == "/"
		if voidTags[name] || self {
			continue
		}
		if !closing {
			stack = append(stack, name)
			continue
		}
		// Unwind to the matching open rather than demanding the top of the
		// stack, since a real page leaves the odd <p> and <li> unclosed and
		// that is legal HTML.
		found := -1
		for i := len(stack) - 1; i >= 0; i-- {
			if stack[i] == name {
				found = i
				break
			}
		}
		if found < 0 {
			return fmt.Sprintf("closes </%s> without opening it", name)
		}
		stack = stack[:found]
	}
	for _, name := range stack {
		switch name {
		case "html", "head", "body", "script", "style", "div", "svg", "table":
			return fmt.Sprintf("<%s> is never closed, so the page is incomplete", name)
		}
	}
	return ""
}

var (
	scriptSrc  = regexp.MustCompile(`(?i)<script[^>]+src\s*=`)
	newGlobal  = regexp.MustCompile(`\bnew\s+(?:window\.)?([A-Z][A-Za-z0-9_]*)\s*[.(]`)
	callGlobal = regexp.MustCompile(`(?:^|[^\w.])(?:window\.)?([A-Z][A-Za-z0-9_]*)\.[a-z]\w*\s*\(`)
)

// browserGlobals is what a page already has without loading anything.
var browserGlobals = map[string]bool{
	"Array": true, "Boolean": true, "Date": true, "Error": true, "Function": true,
	"Image": true, "Intl": true, "JSON": true, "Map": true, "Math": true,
	"Number": true, "Object": true, "Promise": true, "Proxy": true, "Reflect": true,
	"RegExp": true, "Set": true, "String": true, "Symbol": true, "URL": true,
	"URLSearchParams": true, "WeakMap": true, "WeakSet": true, "XMLHttpRequest": true,
	"Audio": true, "Blob": true, "FormData": true, "Headers": true, "Request": true,
	"Response": true, "AbortController": true, "Event": true, "CustomEvent": true,
	"Worker": true, "WebSocket": true, "FileReader": true, "TextEncoder": true,
	"TextDecoder": true, "BigInt": true, "Notification": true, "ResizeObserver": true,
	"IntersectionObserver": true, "MutationObserver": true, "OffscreenCanvas": true,
	"Path2D": true, "DOMParser": true, "Option": true,
}

// missingLibrary catches a page calling into a library it never loads.
//
// The check only runs on a page with no external script at all, which is the
// case it can be sure about: the model was asked for a map, could not find a
// real library in the passages, and called `new SatMeteo.Map(...)` off a
// website that has no such thing. The page renders blank and looks finished.
func missingLibrary(code string) string {
	if scriptSrc.MatchString(code) {
		return ""
	}
	for _, m := range append(newGlobal.FindAllStringSubmatch(code, -1), callGlobal.FindAllStringSubmatch(code, -1)...) {
		name := m[1]
		if browserGlobals[name] || definedIn(code, name) {
			continue
		}
		return fmt.Sprintf("uses %s but loads no script that defines it, so the page will not do anything", name)
	}
	return ""
}

func definedIn(code, name string) bool {
	for _, form := range []string{
		"var " + name, "let " + name, "const " + name, "function " + name,
		"class " + name, "window." + name + " =", name + " =",
	} {
		if strings.Contains(code, form) {
			return true
		}
	}
	return false
}

var dockerInstructions = map[string]bool{
	"FROM": true, "RUN": true, "CMD": true, "LABEL": true, "MAINTAINER": true,
	"EXPOSE": true, "ENV": true, "ADD": true, "COPY": true, "ENTRYPOINT": true,
	"VOLUME": true, "USER": true, "WORKDIR": true, "ARG": true, "ONBUILD": true,
	"STOPSIGNAL": true, "HEALTHCHECK": true, "SHELL": true,
}

// dockerfileSyntax checks the two things that are always true of one: it opens
// on FROM, and every line is an instruction docker knows.
func dockerfileSyntax(code string) string {
	seenFrom := false
	continued := false
	for i, raw := range strings.Split(code, "\n") {
		l := strings.TrimSpace(raw)
		wasContinued := continued
		continued = strings.HasSuffix(l, "\\")
		if l == "" || strings.HasPrefix(l, "#") || wasContinued {
			continue
		}
		word := strings.ToUpper(strings.Fields(l)[0])
		if !dockerInstructions[word] {
			return fmt.Sprintf("line %d starts with %q, which is not a Dockerfile instruction", i+1, strings.Fields(l)[0])
		}
		if word == "FROM" {
			seenFrom = true
		}
		if !seenFrom && word != "ARG" {
			return fmt.Sprintf("line %d runs before any FROM, so there is no image to build on", i+1)
		}
	}
	if !seenFrom {
		return "has no FROM, so there is nothing to build"
	}
	return ""
}

// unusedConstants names a config knob the code defines and never reads.
//
// A model writing from fragments puts CACHE_AGE at the top because every
// example it saw had one, and then never uses it, which reads as a setting a
// person can change and is not. Upper case only, since a lower case name has
// too many ways to be used indirectly.
var constLine = regexp.MustCompile(`(?m)^([A-Z][A-Z0-9_]{2,})\s*(?::[^=\n]+)?=`)

func unusedConstants(blocks []CodeBlock) []string {
	var out []string
	for _, b := range blocks {
		switch normalLang(b.Lang, b.File) {
		case "python", "javascript", "go":
		default:
			continue
		}
		for _, m := range constLine.FindAllStringSubmatch(b.Code, -1) {
			name := m[1]
			if strings.Count(b.Code, name) > 1 {
				continue
			}
			where := b.File
			if where == "" {
				where = "the " + b.Lang + " block"
			}
			out = append(out, fmt.Sprintf("%s sets %s and never uses it, so changing it does nothing", where, name))
		}
	}
	return out
}

// codeWarnings turns failed checks into the lines shown above the answer. Only
// the failures, since a reader does not need telling that six files were fine
// when the panel below already says so.
func codeWarnings(checks []CodeCheck) []string {
	var out []string
	for _, c := range checks {
		if c.OK {
			continue
		}
		name := c.File
		if name == "" {
			name = "the " + c.Lang + " block"
		}
		out = append(out, fmt.Sprintf("%s %s", name, c.Note))
	}
	return out
}

// codeWeight scores a page for a code question. A page that shows code is
// worth more than one that talks about it, and the docs are worth more than a
// tutorial farm, which is most of what a search for a library name returns.
func codeWeight(p *Page) int {
	if p == nil {
		return 0
	}
	score := strings.Count(p.Markdown, "\n```")
	if score > 6 {
		score = 6
	}
	host := strings.ToLower(p.Site)
	switch {
	case strings.HasPrefix(host, "docs."), strings.Contains(host, "readthedocs"),
		strings.Contains(host, "developer."), strings.HasSuffix(host, ".dev"),
		strings.Contains(host, "github.com"), strings.Contains(host, "gitlab.com"),
		strings.Contains(host, "mozilla.org"), strings.Contains(host, "pkg.go.dev"),
		strings.Contains(host, "docker.com"), strings.Contains(host, "python.org"),
		strings.Contains(host, "ibm.com"), strings.Contains(host, "microsoft.com"),
		strings.Contains(host, "stackoverflow.com"):
		score += 4
	}
	return score
}

// codeFailed says whether the checks found something a second search could fix.
// A file that does not parse and a package that is not in its registry are both
// evidence the pages found did not show how this is really written.
func codeFailed(checks []CodeCheck, deps []Dependency) bool {
	for _, c := range checks {
		if !c.OK && !c.Truncated {
			return true
		}
	}
	for _, d := range deps {
		if d.Checked && !d.Found {
			return true
		}
	}
	return false
}

// codeHint tells the second plan what went wrong, in the terms the first one
// got wrong, so it searches for the library rather than rewording the answer.
func codeHint(checks []CodeCheck, deps []Dependency) string {
	var bad []string
	for _, c := range checks {
		if !c.OK {
			name := c.File
			if name == "" {
				name = "the " + c.Lang + " block"
			}
			bad = append(bad, name+" "+c.Note)
		}
	}
	for _, d := range deps {
		if d.Checked && !d.Found {
			bad = append(bad, "it used the package "+d.Name+", which does not exist on "+ecoName(d.Eco))
		}
	}
	return "A previous attempt wrote code with these problems: " + strings.Join(bad, "; ") +
		". Search for the official documentation and a working example of the library or tool this actually needs."
}
