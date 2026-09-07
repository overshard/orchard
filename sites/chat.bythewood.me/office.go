// docx, xlsx and pptx.
//
// All three are a zip of xml, so they need no dependency and no subprocess. The
// parsing is deliberately shallow: what a model wants out of a spreadsheet is
// the values in row and column order, not the formatting, and everything these
// formats carry beyond that is noise it would have to read past.
package main

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// officeKind names the format from the parts inside the zip rather than the
// extension, since all three look identical from the outside and a renamed file
// is common.
func officeKind(zr *zip.Reader) string {
	for _, f := range zr.File {
		switch {
		case f.Name == "word/document.xml":
			return "docx"
		case f.Name == "xl/workbook.xml":
			return "xlsx"
		case strings.HasPrefix(f.Name, "ppt/slides/slide"):
			return "pptx"
		}
	}
	return ""
}

func officeText(raw []byte) (string, string, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", "", fmt.Errorf("this file is not readable as an office document")
	}
	kind := officeKind(zr)
	switch kind {
	case "docx":
		text, err := docxText(zr)
		return text, kind, err
	case "xlsx":
		text, err := xlsxText(zr)
		return text, kind, err
	case "pptx":
		text, err := pptxText(zr)
		return text, kind, err
	}
	return "", "", fmt.Errorf("this file is a zip but not a document")
}

func openPart(zr *zip.Reader, name string) ([]byte, error) {
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(io.LimitReader(rc, 64<<20))
		}
	}
	return nil, fmt.Errorf("%s is missing", name)
}

// runsToText walks the xml pulling out the text runs and turning the elements
// that mean a line break into one. Decoding by token rather than into a struct
// keeps this indifferent to the parts of the schema it does not care about.
func runsToText(data []byte, breakOn map[string]bool) string {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var sb strings.Builder
	inText := false
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "t" {
				inText = true
			}
		case xml.EndElement:
			if t.Name.Local == "t" {
				inText = false
			}
			if breakOn[t.Name.Local] {
				sb.WriteString("\n")
			}
		case xml.CharData:
			if inText {
				sb.Write(t)
			}
		}
	}
	return collapseBlankLines(sb.String())
}

func docxText(zr *zip.Reader) (string, error) {
	data, err := openPart(zr, "word/document.xml")
	if err != nil {
		return "", fmt.Errorf("this docx has no document part")
	}
	// A paragraph and a table row both end a line, and a cell does not, so a
	// table comes out one row per line rather than one word per line.
	text := runsToText(data, map[string]bool{"p": true, "tr": true, "br": true})
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("this docx has no text in it")
	}
	return text, nil
}

func pptxText(zr *zip.Reader) (string, error) {
	names := []string{}
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "ppt/slides/slide") && strings.HasSuffix(f.Name, ".xml") {
			names = append(names, f.Name)
		}
	}
	// Zip order is not slide order, and slide10 sorts before slide2 as a
	// string, so they are ordered by the number in the name.
	sort.Slice(names, func(i, j int) bool { return slideNum(names[i]) < slideNum(names[j]) })

	var sb strings.Builder
	for i, name := range names {
		data, err := openPart(zr, name)
		if err != nil {
			continue
		}
		fmt.Fprintf(&sb, "--- slide %d ---\n", i+1)
		sb.WriteString(runsToText(data, map[string]bool{"p": true, "br": true}))
		sb.WriteString("\n")
	}
	out := collapseBlankLines(sb.String())
	if strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("this pptx has no text in it")
	}
	return out, nil
}

func slideNum(name string) int {
	base := strings.TrimSuffix(strings.TrimPrefix(name, "ppt/slides/slide"), ".xml")
	n, err := strconv.Atoi(base)
	if err != nil {
		return 1 << 30
	}
	return n
}

// xlsx keeps every string in one shared table and the cells hold indexes into
// it, so the table has to be read before any sheet means anything.
func xlsxText(zr *zip.Reader) (string, error) {
	shared := sharedStrings(zr)

	names := []string{}
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "xl/worksheets/sheet") && strings.HasSuffix(f.Name, ".xml") {
			names = append(names, f.Name)
		}
	}
	sort.Strings(names)

	var sb strings.Builder
	for _, name := range names {
		data, err := openPart(zr, name)
		if err != nil {
			continue
		}
		if len(names) > 1 {
			fmt.Fprintf(&sb, "--- %s ---\n", strings.TrimSuffix(strings.TrimPrefix(name, "xl/worksheets/"), ".xml"))
		}
		sb.WriteString(sheetText(data, shared))
		sb.WriteString("\n")
	}
	out := collapseBlankLines(sb.String())
	if strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("this xlsx has no values in it")
	}
	return out, nil
}

func sharedStrings(zr *zip.Reader) []string {
	data, err := openPart(zr, "xl/sharedStrings.xml")
	if err != nil {
		return nil
	}
	var out []string
	dec := xml.NewDecoder(bytes.NewReader(data))
	var cur strings.Builder
	inItem, inText := false, false
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "si":
				inItem, cur = true, strings.Builder{}
			case "t":
				inText = true
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "si":
				if inItem {
					out = append(out, cur.String())
				}
				inItem = false
			case "t":
				inText = false
			}
		case xml.CharData:
			if inItem && inText {
				cur.Write(t)
			}
		}
	}
	return out
}

// sheetText renders a sheet as tab separated rows, which is the shape a model
// reads a table in most reliably and is what a csv would have looked like.
func sheetText(data []byte, shared []string) string {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var sb strings.Builder
	var row []string
	var cell strings.Builder
	cellType, inValue := "", false

	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				row = row[:0]
			case "c":
				cellType, cell = "", strings.Builder{}
				for _, a := range t.Attr {
					if a.Name.Local == "t" {
						cellType = a.Value
					}
				}
			case "v", "t":
				inValue = true
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "v", "t":
				inValue = false
			case "c":
				row = append(row, resolveCell(cell.String(), cellType, shared))
			case "row":
				line := strings.TrimRight(strings.Join(row, "\t"), "\t")
				if strings.TrimSpace(line) != "" {
					sb.WriteString(line)
					sb.WriteString("\n")
				}
			}
		case xml.CharData:
			if inValue {
				cell.Write(t)
			}
		}
	}
	return sb.String()
}

// A cell of type s holds an index into the shared table rather than a value,
// which is the one thing that makes a spreadsheet unreadable if missed.
func resolveCell(raw, cellType string, shared []string) string {
	if cellType == "s" {
		if i, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && i >= 0 && i < len(shared) {
			return shared[i]
		}
		return ""
	}
	return strings.TrimSpace(raw)
}

// collapseBlankLines keeps a run of empty lines down to one. A docx paragraph
// per line plus a break element per line doubles them otherwise, and blank
// lines are tokens the model pays for.
func collapseBlankLines(s string) string {
	lines := strings.Split(normalise(s), "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, l := range lines {
		l = strings.TrimRight(l, " \t")
		if l == "" {
			if blank {
				continue
			}
			blank = true
		} else {
			blank = false
		}
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
