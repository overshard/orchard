package main

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
)

// archived is one line of briefs.jsonl, every brief either desk has written, so
// a morning brief can still be read back after the 11am one replaces it.
type archived struct {
	Desk  string `json:"desk"`
	Brief Brief  `json:"brief"`
}

type briefArchive struct{ path string }

func openArchive(dataDir string) briefArchive {
	return briefArchive{path: filepath.Join(dataDir, "briefs.jsonl")}
}

// has is whether a desk's brief for a slot is already in the file, which is how
// the saved briefs read back on a restart avoid going in twice.
func (a briefArchive) has(desk string, slot int64) bool {
	f, err := os.Open(a.path)
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var head struct {
			Desk  string `json:"desk"`
			Brief struct {
				Slot int64 `json:"slot"`
			} `json:"brief"`
		}
		if json.Unmarshal(sc.Bytes(), &head) == nil && head.Desk == desk && head.Brief.Slot == slot {
			return true
		}
	}
	return false
}

func (a briefArchive) add(desk string, b Brief) {
	if len(b.Points) == 0 {
		return
	}
	b.Waiting, b.Status = "", ""
	raw, err := json.Marshal(archived{desk, b})
	if err != nil {
		return
	}
	f, err := os.OpenFile(a.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err == nil {
		_, err = f.Write(append(raw, '\n'))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		slog.Warn("brief not archived", slog.String("component", "brief"), slog.String("desk", desk), slog.Any("err", err))
	}
}
