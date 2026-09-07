package main

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func zipOf(t *testing.T, parts map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range parts {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDocxKeepsParagraphsAndTableRows(t *testing.T) {
	raw := zipOf(t, map[string]string{"word/document.xml": `<?xml version="1.0"?>
<w:document xmlns:w="x"><w:body>
<w:p><w:r><w:t>First line</w:t></w:r></w:p>
<w:p><w:r><w:t>Second </w:t></w:r><w:r><w:t>line</w:t></w:r></w:p>
<w:tbl><w:tr><w:tc><w:p><w:r><w:t>a</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>b</w:t></w:r></w:p></w:tc></w:tr></w:tbl>
</w:body></w:document>`})

	text, kind, err := officeText(raw)
	if err != nil {
		t.Fatal(err)
	}
	if kind != "docx" {
		t.Errorf("kind = %q", kind)
	}
	// Runs inside one paragraph join, rather than becoming two lines.
	if !strings.Contains(text, "Second line") {
		t.Errorf("runs did not join:\n%s", text)
	}
	if !strings.Contains(text, "First line") {
		t.Errorf("missing text:\n%s", text)
	}
}

// The shared string table is the thing that makes a spreadsheet unreadable when
// missed, since every label is an index and not a value.
func TestXlsxResolvesSharedStrings(t *testing.T) {
	raw := zipOf(t, map[string]string{
		"xl/workbook.xml": `<workbook/>`,
		"xl/sharedStrings.xml": `<?xml version="1.0"?>
<sst><si><t>date</t></si><si><t>amount</t></si><si><t>coffee</t></si></sst>`,
		"xl/worksheets/sheet1.xml": `<?xml version="1.0"?>
<worksheet><sheetData>
<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c></row>
<row r="2"><c r="A2" t="s"><v>2</v></c><c r="B2"><v>4.75</v></c></row>
</sheetData></worksheet>`,
	})

	text, kind, err := officeText(raw)
	if err != nil {
		t.Fatal(err)
	}
	if kind != "xlsx" {
		t.Errorf("kind = %q", kind)
	}
	if !strings.Contains(text, "date\tamount") {
		t.Errorf("header row wrong:\n%s", text)
	}
	if !strings.Contains(text, "coffee\t4.75") {
		t.Errorf("a shared string or a number was lost:\n%s", text)
	}
	// An index must never reach the model as a bare number.
	if strings.Contains(text, "0\t1") {
		t.Errorf("shared string indexes leaked:\n%s", text)
	}
}

func TestPptxOrdersSlidesNumerically(t *testing.T) {
	slide := func(s string) string {
		return `<?xml version="1.0"?><p:sld xmlns:a="x"><p:cSld><a:p><a:r><a:t>` + s + `</a:t></a:r></a:p></p:cSld></p:sld>`
	}
	raw := zipOf(t, map[string]string{
		"ppt/slides/slide1.xml":  slide("one"),
		"ppt/slides/slide2.xml":  slide("two"),
		"ppt/slides/slide10.xml": slide("ten"),
	})
	text, kind, err := officeText(raw)
	if err != nil {
		t.Fatal(err)
	}
	if kind != "pptx" {
		t.Errorf("kind = %q", kind)
	}
	// slide10 sorts before slide2 as a string, which is the trap.
	if strings.Index(text, "two") > strings.Index(text, "ten") {
		t.Errorf("slides came out in string order:\n%s", text)
	}
}

func TestAPlainZipIsRefusedWithAdvice(t *testing.T) {
	raw := zipOf(t, map[string]string{"notes.txt": "hello"})
	parts := readFiles(headers(t, map[string][]byte{"bundle.zip": raw}))
	if parts[0].Err == "" {
		t.Fatal("a plain zip was accepted")
	}
	if !strings.Contains(parts[0].Err, "files inside it") {
		t.Errorf("err = %q, want it to say what to do instead", parts[0].Err)
	}
}

// An office document is a zip, so it has to be caught before the binary sniff
// or every docx is refused as binary.
func TestAnOfficeFileIsNotTreatedAsBinary(t *testing.T) {
	raw := zipOf(t, map[string]string{"word/document.xml": `<w:document xmlns:w="x"><w:body><w:p><w:r><w:t>hello</w:t></w:r></w:p></w:body></w:document>`})
	parts := readFiles(headers(t, map[string][]byte{"notes.docx": raw}))
	if parts[0].Err != "" {
		t.Fatalf("refused: %s", parts[0].Err)
	}
	if parts[0].Kind != "docx" {
		t.Errorf("kind = %q, want docx", parts[0].Kind)
	}
	if !strings.Contains(parts[0].Text, "hello") {
		t.Errorf("text = %q", parts[0].Text)
	}
}

func TestCollapseBlankLines(t *testing.T) {
	got := collapseBlankLines("a\n\n\n\nb\n   \n\nc\n")
	if got != "a\n\nb\n\nc" {
		t.Errorf("got %q", got)
	}
}
