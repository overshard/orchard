package main

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"strings"
	"testing"
)

// headers runs the bytes through a real multipart round trip, since that is
// what the handler hands readFiles and a hand built FileHeader has no body.
func headers(t *testing.T, files map[string][]byte) []*multipart.FileHeader {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	for _, name := range names {
		p, err := w.CreateFormFile("files", name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Write(files[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	form, err := multipart.NewReader(&buf, w.Boundary()).ReadForm(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = form.RemoveAll() })
	return form.File["files"]
}

func TestReadsATextFile(t *testing.T) {
	parts := readFiles(headers(t, map[string][]byte{"notes.md": []byte("# Title\r\nbody\r\n")}))
	if len(parts) != 1 {
		t.Fatalf("got %d parts", len(parts))
	}
	p := parts[0]
	if p.Err != "" {
		t.Fatalf("unexpected error: %s", p.Err)
	}
	if p.Kind != "md" {
		t.Errorf("kind = %q, want md", p.Kind)
	}
	if strings.Contains(p.Text, "\r") {
		t.Error("carriage returns survived")
	}
	if p.Text != "# Title\nbody\n" {
		t.Errorf("text = %q", p.Text)
	}
}

// An unknown suffix is common on config and log files, and refusing by
// extension would drop exactly the files worth asking about.
func TestReadsAnUnknownExtension(t *testing.T) {
	parts := readFiles(headers(t, map[string][]byte{"hosts.conf.bak": []byte("listen 8000\n")}))
	if parts[0].Err != "" {
		t.Fatalf("unexpected error: %s", parts[0].Err)
	}
	if parts[0].Text != "listen 8000\n" {
		t.Errorf("text = %q", parts[0].Text)
	}
}

func TestRefusesAnImageAndSaysWhy(t *testing.T) {
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0x00, 0x01}, 64)...)
	parts := readFiles(headers(t, map[string][]byte{"shot.png": png}))
	if parts[0].Err == "" {
		t.Fatal("an image was accepted")
	}
	if !strings.Contains(parts[0].Err, "text only") {
		t.Errorf("err = %q, want it to name the reason", parts[0].Err)
	}
	if parts[0].Kind != "png" {
		t.Errorf("kind = %q, want png", parts[0].Kind)
	}
}

// A .txt holding a binary payload is still binary, so the sniff has to beat
// the extension in both directions.
func TestRefusesBinaryNamedAsText(t *testing.T) {
	parts := readFiles(headers(t, map[string][]byte{"payload.txt": {0x00, 0x01, 0x02, 0x03, 0x00}}))
	if parts[0].Err == "" {
		t.Fatal("a binary file was accepted")
	}
	if parts[0].Kind != "binary" {
		t.Errorf("kind = %q, want binary", parts[0].Kind)
	}
}

// One bad file must not lose the good ones, since a drop of five files where
// one is a screenshot is the ordinary case.
func TestOneBadFileDoesNotLoseTheRest(t *testing.T) {
	parts := readFiles(headers(t, map[string][]byte{
		"good.txt": []byte("hello"),
		"bad.png":  append([]byte("\x89PNG\r\n\x1a\n"), 0x00, 0x01),
	}))
	if len(parts) != 2 {
		t.Fatalf("got %d parts", len(parts))
	}
	var ok, bad int
	for _, p := range parts {
		if p.Err == "" {
			ok++
		} else {
			bad++
		}
	}
	if ok != 1 || bad != 1 {
		t.Errorf("ok = %d, bad = %d, want one of each", ok, bad)
	}
}

func TestTruncatesAtTheFileCeiling(t *testing.T) {
	big := bytes.Repeat([]byte("a"), maxFileChars+5000)
	parts := readFiles(headers(t, map[string][]byte{"big.txt": big}))
	if parts[0].Err != "" {
		t.Fatalf("unexpected error: %s", parts[0].Err)
	}
	if len(parts[0].Text) > maxFileChars+200 {
		t.Errorf("text is %d characters, want it capped near %d", len(parts[0].Text), maxFileChars)
	}
	if !strings.Contains(parts[0].Text, "truncated") {
		t.Error("truncation was silent")
	}
}

// The message goes last because that is where a small model reads most
// carefully, and a file after the question got answered as if it were one.
func TestComposeEndsOnTheMessage(t *testing.T) {
	got := composeTurn("what is wrong with this", []filePart{
		{Attachment: Attachment{Name: "a.go", Kind: "go", Size: 12}, Text: "package main"},
	})
	if !strings.HasSuffix(got, "what is wrong with this") {
		t.Errorf("does not end on the message:\n%s", got)
	}
	if strings.Index(got, "package main") > strings.Index(got, "what is wrong with this") {
		t.Error("the file came after the message")
	}
	if !strings.Contains(got, "a.go") {
		t.Error("the file was not named")
	}
}

// Files with no message is a real turn, and it needs an instruction or the
// model is handed a wall of text and no question.
func TestComposeSuppliesAMessageWhenThereIsNone(t *testing.T) {
	got := composeTurn("", []filePart{
		{Attachment: Attachment{Name: "a.txt", Kind: "txt", Size: 3}, Text: "abc"},
	})
	if !strings.Contains(got, "say what they are") {
		t.Errorf("no default instruction:\n%s", got)
	}
}

func TestComposeNamesAFileItCouldNotRead(t *testing.T) {
	got := composeTurn("read this", []filePart{
		{Attachment: Attachment{Name: "shot.png", Kind: "png", Size: 900, Err: "reads text only"}},
	})
	if !strings.Contains(got, "shot.png") || !strings.Contains(got, "not readable") {
		t.Errorf("the unreadable file was not reported:\n%s", got)
	}
}

func TestComposeLeavesAPlainTurnAlone(t *testing.T) {
	if got := composeTurn("hello", nil); got != "hello" {
		t.Errorf("got %q, want it untouched", got)
	}
}

// The title is generated from this, so a file's contents reaching it is how a
// conversation ends up named after line one of a csv.
func TestTitleSeedFallsBackToTheNames(t *testing.T) {
	seed := titleSeed("", []filePart{
		{Attachment: Attachment{Name: "budget.csv"}, Text: "date,amount\n2026-01-01,12"},
	})
	if !strings.Contains(seed, "budget.csv") {
		t.Errorf("seed = %q", seed)
	}
	if strings.Contains(seed, "2026-01-01") {
		t.Error("the file's contents reached the title")
	}
	if got := titleSeed("what is this", nil); got != "what is this" {
		t.Errorf("got %q, want the message", got)
	}
}

func TestHumanSize(t *testing.T) {
	for _, c := range []struct {
		in   int64
		want string
	}{{512, "512 B"}, {2048, "2.0 KB"}, {5 << 20, "5.0 MB"}} {
		if got := humanSize(c.in); got != c.want {
			t.Errorf("humanSize(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The failure that lost a whole turn: a tool call cut off by the token budget
// arrives as unparseable JSON and llama.cpp refuses the request outright.
func TestATruncatedToolCallIsRecognised(t *testing.T) {
	real := fmt.Errorf("the model refused this turn: server_error: Failed to parse tool call arguments as JSON: " +
		"[json.exception.parse_error.101] parse error at line 1, column 868: syntax error while parsing value - " +
		"invalid string: missing closing quote")
	if !isTruncatedToolCall(real) {
		t.Error("the real refusal was not recognised")
	}
	for _, other := range []error{
		nil,
		fmt.Errorf("the model is not answering: connection refused"),
		fmt.Errorf("the model answered 503 with no reason"),
		fmt.Errorf("failed to parse the response body"),
	} {
		if isTruncatedToolCall(other) {
			t.Errorf("wrongly matched: %v", other)
		}
	}
}
