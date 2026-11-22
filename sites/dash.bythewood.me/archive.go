package main

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// archived is one line of a month's briefs-YYYY-MM.jsonl, every brief a desk
// has written, so a morning brief can still be read back after the 9am one
// replaces it.
type archived struct {
	Desk  string `json:"desk"`
	Brief Brief  `json:"brief"`
}

// briefArchive is a file a month, by the slot's month in UTC, so a restart's
// check for a slot reads one month and nothing grows without bound.
type briefArchive struct{ dir string }

func openArchive(dataDir string) briefArchive {
	a := briefArchive{dir: dataDir}
	a.split(filepath.Join(dataDir, "briefs.jsonl"))
	return a
}

func (a briefArchive) month(slot int64) string {
	return filepath.Join(a.dir, "briefs-"+time.Unix(slot, 0).UTC().Format("2006-01")+".jsonl")
}

// split moves the single file the archive used to be into months, and keeps
// it renamed rather than deleting it.
func (a briefArchive) split(legacy string) {
	f, err := os.Open(legacy)
	if err != nil {
		return
	}
	months := map[string][]byte{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var head struct {
			Brief struct {
				Slot int64 `json:"slot"`
			} `json:"brief"`
		}
		if json.Unmarshal(sc.Bytes(), &head) != nil || head.Brief.Slot <= 0 {
			continue
		}
		p := a.month(head.Brief.Slot)
		months[p] = append(append(months[p], sc.Bytes()...), '\n')
	}
	err = sc.Err()
	f.Close()
	for p, raw := range months {
		if err != nil {
			break
		}
		err = appendFile(p, raw)
	}
	if err == nil {
		err = os.Rename(legacy, legacy+".split")
	}
	if err != nil {
		slog.Warn("briefs.jsonl not split into months", slog.String("component", "brief"), slog.Any("err", err))
	}
}

// lines calls fn with every line of one month's file.
func (a briefArchive) lines(path string, fn func([]byte)) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		fn(sc.Bytes())
	}
}

// has is whether a desk's brief for a slot is already in the file, which is how
// the saved briefs read back on a restart avoid going in twice.
func (a briefArchive) has(desk string, slot int64) bool {
	found := false
	a.lines(a.month(slot), func(raw []byte) {
		var head struct {
			Desk  string `json:"desk"`
			Brief struct {
				Slot int64 `json:"slot"`
			} `json:"brief"`
		}
		if !found && json.Unmarshal(raw, &head) == nil && head.Desk == desk && head.Brief.Slot == slot {
			found = true
		}
	})
	return found
}

func appendFile(path string, raw []byte) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(raw)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func (a briefArchive) add(desk string, b Brief) {
	if len(b.Points) == 0 {
		return
	}
	b.Status = ""
	raw, err := json.Marshal(archived{desk, b})
	if err != nil {
		return
	}
	if err := appendFile(a.month(b.Slot), append(raw, '\n')); err != nil {
		slog.Warn("brief not archived", slog.String("component", "brief"), slog.String("desk", desk), slog.Any("err", err))
	}
}
