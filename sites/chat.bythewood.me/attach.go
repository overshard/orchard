// Files a turn can carry.
//
// A file arrives with the turn, is read into text and the bytes are dropped, so
// there is no upload directory to age out and incognito stays true to its name.
// The extracted text is already in the conversation, so a follow up about a file
// works without the file being sent again. A picture is the exception, since
// there is no text in it and the image model needs the picture itself, so it is
// kept with the conversation the way a drawn one is.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
)

const (
	maxFiles      = 10
	maxFileBytes  = 20 << 20
	maxFileChars  = 40000
	maxTotalChars = 80000
)

// Attachment is what the UI shows on the message and what the database keeps.
// The text itself is not here, it is folded into the message content.
type Attachment struct {
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	Kind  string `json:"kind"`
	Chars int    `json:"chars,omitempty"`
	Err   string `json:"err,omitempty"`
	// A picture, by the id it is served and stored under.
	Image  string `json:"image,omitempty"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

type filePart struct {
	Attachment
	Text    string
	Picture []byte
}

// readFiles turns the uploaded parts into text. A file that cannot be read is
// not an error for the turn: it comes back with Err set and the model is told
// which files it did not get, which is better than refusing the whole message
// because one of five was a screenshot.
func readFiles(headers []*multipart.FileHeader) []filePart {
	if len(headers) > maxFiles {
		headers = headers[:maxFiles]
	}
	out := make([]filePart, 0, len(headers))
	budget := maxTotalChars
	for _, h := range headers {
		p := filePart{Attachment: Attachment{Name: filepath.Base(h.Filename), Size: h.Size}}
		switch {
		case h.Size > maxFileBytes:
			p.Kind, p.Err = "skipped", fmt.Sprintf("larger than %s", humanSize(maxFileBytes))
		default:
			raw, err := readPart(h)
			if err != nil {
				p.Kind, p.Err = "unreadable", err.Error()
				break
			}
			if kind, ok := imageKind(raw); ok {
				p.Kind = kind
				pic, w, h, err := asReference(raw)
				if err != nil {
					p.Err = err.Error()
					break
				}
				p.Picture, p.Image, p.Width, p.Height = pic, newID(), w, h
				break
			}
			text, kind, err := extract(h.Filename, raw)
			p.Kind = kind
			if err != nil {
				p.Err = err.Error()
			} else {
				if len(text) > maxFileChars {
					text = text[:maxFileChars] + fmt.Sprintf("\n\n[truncated, this is the first %d characters of %d]", maxFileChars, len(text))
				}
				if len(text) > budget {
					text = text[:budget] + "\n\n[truncated, the turn ran out of room for the rest]"
				}
				budget -= len(text)
				if budget < 0 {
					budget = 0
				}
				p.Text, p.Chars = text, len(text)
			}
		}
		out = append(out, p)
	}
	return out
}

func readPart(h *multipart.FileHeader) ([]byte, error) {
	f, err := h.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxFileBytes))
}

func extract(name string, raw []byte) (string, string, error) {
	if strings.EqualFold(filepath.Ext(name), ".pdf") || bytes.HasPrefix(raw, []byte("%PDF-")) {
		text, err := pdfText(raw)
		return text, "pdf", err
	}
	if bytes.HasPrefix(raw, []byte("PK\x03\x04")) {
		text, kind, err := officeText(raw)
		if err == nil || kind != "" {
			return text, kind, err
		}
		return "", "zip", fmt.Errorf("an archive cannot be read, send the files inside it instead")
	}
	if !looksTextual(raw) {
		return "", "binary", fmt.Errorf("not a text file, so there is nothing to read out of it")
	}
	return normalise(string(raw)), textKind(name), nil
}

// pdfText tries poppler first and the pure Go reader second.
//
// A bank statement is normally encrypted with an empty owner password, which the
// Go reader will not open at all, and what it does open comes back in object
// order with no column alignment. pdftotext -layout keeps the columns.
func pdfText(raw []byte) (string, error) {
	if out, err := popplerText(raw); err == nil {
		return out, nil
	} else if !errors.Is(err, errNoPoppler) {
		// Poppler ran and could not read it, so the Go reader will not do
		// better. Its own message is the useful one.
		return "", err
	}
	return goPDFText(raw)
}

var errNoPoppler = errors.New("pdftotext is not installed")

func popplerText(raw []byte) (string, error) {
	bin, err := exec.LookPath("pdftotext")
	if err != nil {
		return "", errNoPoppler
	}
	dir, err := os.MkdirTemp("", "pdf")
	if err != nil {
		return "", errNoPoppler
	}
	defer os.RemoveAll(dir)
	in := filepath.Join(dir, "in.pdf")
	if err := os.WriteFile(in, raw, 0o600); err != nil {
		return "", errNoPoppler
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	// -layout keeps the columns, -nopgbrk drops the form feeds, and the empty
	// -opw is what opens a statement encrypted with no password set.
	cmd := exec.CommandContext(ctx, bin, "-layout", "-nopgbrk", "-enc", "UTF-8", "-opw", "", in, "-")
	var out, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errBuf
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("this pdf took too long to read")
		}
		// poppler's exit codes are specific and its stderr wording is not, so
		// the code decides and the text only ever adds detail.
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			switch ee.ExitCode() {
			case 1:
				return "", fmt.Errorf("this pdf could not be opened, it may be damaged")
			case 3:
				return "", fmt.Errorf("this pdf is locked against copying text")
			}
		}
		if msg := strings.TrimSpace(errBuf.String()); msg != "" {
			return "", fmt.Errorf("this pdf could not be read: %s", firstLine(msg))
		}
		return "", fmt.Errorf("this pdf could not be read")
	}
	text := normalise(out.String())
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("this pdf has no text in it, it is probably a scan and would need OCR")
	}
	return text, nil
}

// goPDFText is the fallback for a development run outside the container, where
// poppler is not on the path. It recovers rather than propagating a panic,
// because the reader panics on a malformed cross reference table instead of
// returning an error.
func goPDFText(raw []byte) (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			text, err = "", fmt.Errorf("this pdf could not be parsed")
		}
	}()
	r, err := pdf.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", fmt.Errorf("this pdf could not be parsed")
	}
	rd, err := r.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("this pdf has no extractable text, it is probably scanned")
	}
	var sb strings.Builder
	if _, err := io.Copy(&sb, io.LimitReader(rd, 8<<20)); err != nil {
		return "", fmt.Errorf("this pdf could not be read to the end")
	}
	out := normalise(sb.String())
	if strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("this pdf has no extractable text, it is probably scanned")
	}
	return out, nil
}

// looksTextual decides by content and not by extension, so an unknown suffix on
// a config file still reads and a .txt holding a binary blob still does not.
func looksTextual(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	head := raw
	if len(head) > 8192 {
		head = head[:8192]
	}
	if bytes.IndexByte(head, 0) >= 0 || !utf8.Valid(head) {
		return false
	}
	odd := 0
	for _, b := range head {
		if b < 0x09 || (b > 0x0d && b < 0x20) {
			odd++
		}
	}
	return odd*20 < len(head)
}

func imageKind(raw []byte) (string, bool) {
	switch {
	case bytes.HasPrefix(raw, []byte("\x89PNG\r\n\x1a\n")):
		return "png", true
	case bytes.HasPrefix(raw, []byte{0xff, 0xd8, 0xff}):
		return "jpeg", true
	case bytes.HasPrefix(raw, []byte("GIF8")):
		return "gif", true
	case bytes.HasPrefix(raw, []byte("RIFF")) && len(raw) > 12 && bytes.Equal(raw[8:12], []byte("WEBP")):
		return "webp", true
	case bytes.HasPrefix(raw, []byte("BM")):
		return "bmp", true
	}
	return "", false
}

func textKind(name string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	if ext == "" {
		return "text"
	}
	return ext
}

// normalise strips carriage returns and the byte order mark Windows editors
// leave at the front, both of which reach the model as noise it has to spend
// attention on.
func normalise(s string) string {
	s = strings.TrimPrefix(s, "\ufeff")
	return strings.ReplaceAll(s, "\r\n", "\n")
}

// composeTurn builds what the model is handed. Files come first and the message
// last, since the question is what the answer has to follow and the end of the
// prompt is where a small model reads most carefully.
func composeTurn(message string, parts []filePart) string {
	if len(parts) == 0 {
		return message
	}
	var sb strings.Builder
	sb.WriteString("The user attached ")
	if len(parts) == 1 {
		sb.WriteString("a file. Its contents are below.\n\n")
	} else {
		fmt.Fprintf(&sb, "%d files. Their contents are below.\n\n", len(parts))
	}
	for _, p := range parts {
		if p.Err != "" {
			fmt.Fprintf(&sb, "--- %s (%s): not readable, %s ---\n\n", p.Name, humanSize(p.Size), p.Err)
			continue
		}
		if p.Image != "" {
			fmt.Fprintf(&sb, "--- %s (a %dx%d picture) ---\nYou cannot see this, so never describe what is in it. The image tool starts from it, so pass what he wants done to it, in his words.\n--- end of %s ---\n\n", p.Name, p.Width, p.Height, p.Name)
			continue
		}
		fmt.Fprintf(&sb, "--- %s (%s, %s) ---\n%s\n--- end of %s ---\n\n", p.Name, p.Kind, humanSize(p.Size), p.Text, p.Name)
	}
	sb.WriteString("The user's message about them:\n\n")
	if strings.TrimSpace(message) == "" {
		sb.WriteString("Read these and say what they are and what is in them.")
	} else {
		sb.WriteString(message)
	}
	return sb.String()
}

// pictures is the ids of the pictures among a turn's files.
func pictures(parts []filePart) []string {
	var ids []string
	for _, p := range parts {
		if p.Image != "" {
			ids = append(ids, p.Image)
		}
	}
	return ids
}

func attachments(parts []filePart) []Attachment {
	out := make([]Attachment, 0, len(parts))
	for _, p := range parts {
		out = append(out, p.Attachment)
	}
	return out
}

func humanSize(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
}

// titleSeed keeps the text of a file out of the conversation title. A turn that
// is only attachments is named after them instead.
func titleSeed(message string, parts []filePart) string {
	if strings.TrimSpace(message) != "" {
		return message
	}
	if len(parts) == 0 {
		return ""
	}
	names := make([]string, 0, len(parts))
	for _, p := range parts {
		names = append(names, p.Name)
	}
	return "A message attaching " + strings.Join(names, ", ")
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
