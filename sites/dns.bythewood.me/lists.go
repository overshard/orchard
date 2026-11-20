package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// List is one blocklist. Each line of its file is a domain that covers its
// subdomains too, hagezi's wildcard format.
type List struct {
	Key   string
	Name  string
	About string
	URL   string
}

// lists is every blocklist, in the order a block is credited to. No more than
// eight, since a table entry carries them as the bits of one byte.
var lists = []List{
	{"pro", "hagezi Pro", "ads, trackers, telemetry and metrics", hagezi("pro")},
	{"tif", "hagezi Threat Intelligence, medium", "malware, phishing, scams and cryptojacking", hagezi("tif.medium")},
	{"winoffice", "hagezi native, Windows and Office", "Windows and Office telemetry", hagezi("native.winoffice")},
	{"doh", "hagezi encrypted DNS", "DNS servers an app could use to go around this one", hagezi("doh")},
}

func hagezi(name string) string {
	return "https://cdn.jsdelivr.net/gh/hagezi/dns-blocklists@latest/wildcard/" + name + "-onlydomains.txt"
}

func listByKey(key string) (List, bool) {
	for _, l := range lists {
		if l.Key == key {
			return l, true
		}
	}
	return List{}, false
}

// table is every listed domain sorted in one string, so a few hundred
// thousand of them cost a few bytes of overhead each instead of a map entry.
type table struct {
	blob string
	off  []uint32
	mask []uint8
	// size is the entries each list contributed, by list index.
	size []int
}

func (t *table) at(i int) string {
	end := len(t.blob)
	if i+1 < len(t.off) {
		end = int(t.off[i+1])
	}
	return t.blob[t.off[i]:end]
}

func (t *table) find(name string) uint8 {
	lo, hi := 0, len(t.off)
	for lo < hi {
		m := (lo + hi) / 2
		switch v := t.at(m); {
		case v == name:
			return t.mask[m]
		case v < name:
			lo = m + 1
		default:
			hi = m
		}
	}
	return 0
}

// match walks name and each parent of it, and gives the first listed one and
// which lists have it.
func (t *table) match(name string) (string, uint8) {
	if t == nil {
		return "", 0
	}
	for n := name; ; {
		if m := t.find(n); m != 0 {
			return n, m
		}
		i := strings.IndexByte(n, '.')
		if i < 0 {
			return "", 0
		}
		n = n[i+1:]
	}
}

func build(sets [][]string) *table {
	type e struct {
		s string
		m uint8
	}
	n := 0
	for _, s := range sets {
		n += len(s)
	}
	all := make([]e, 0, n)
	t := &table{size: make([]int, len(sets))}
	for i, s := range sets {
		t.size[i] = len(s)
		for _, d := range s {
			all = append(all, e{d, 1 << i})
		}
	}
	sort.Slice(all, func(a, b int) bool { return all[a].s < all[b].s })
	var sb strings.Builder
	for i := 0; i < len(all); {
		j, m := i, uint8(0)
		for ; j < len(all) && all[j].s == all[i].s; j++ {
			m |= all[j].m
		}
		t.off = append(t.off, uint32(sb.Len()))
		t.mask = append(t.mask, m)
		sb.WriteString(all[i].s)
		i = j
	}
	t.blob = sb.String()
	return t
}

// parse reads a list file, skipping comments, and gives its domains and the
// Version line hagezi stamps on each build.
func parse(r io.Reader) (domains []string, version string, err error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 64*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if line[0] == '#' || line[0] == '!' {
			if v, ok := strings.CutPrefix(line, "# Version:"); ok {
				version = strings.TrimSpace(v)
			}
			continue
		}
		d := cleanName(line)
		if looksLikeDomain(d) {
			domains = append(domains, d)
		}
	}
	return domains, version, sc.Err()
}

// lister keeps the lists on disk and the compiled table in memory, and swaps
// a new table in whole so a lookup never sees half a refresh.
type lister struct {
	dir   string
	store *Store
	tab   atomic.Pointer[table]
	http  *http.Client
}

func newLister(dir string, store *Store) *lister {
	return &lister{dir: dir, store: store, http: &http.Client{Timeout: 2 * time.Minute}}
}

func (l *lister) path(key string) string { return filepath.Join(l.dir, key+".txt") }

// load builds the table from the copies on disk, so a restart blocks the same
// things straight away without waiting on the network.
func (l *lister) load() {
	sets := make([][]string, len(lists))
	for i, li := range lists {
		f, err := os.Open(l.path(li.Key))
		if err != nil {
			continue
		}
		sets[i], _, _ = parse(f)
		f.Close()
	}
	l.tab.Store(build(sets))
}

// Swings bigger than these are refused, since a list that halved or tripled
// overnight is far more likely a bad build than a real change.
const (
	minEntries = 50
	maxShrink  = 0.5
	maxGrow    = 3.0
)

var errSwing = errors.New("refused")

// refresh fetches every list and rebuilds the table if any changed. A list
// that fails keeps its last good copy and records why.
func (l *lister) refresh(ctx context.Context) {
	if err := os.MkdirAll(l.dir, 0o755); err != nil {
		slog.Error("lists dir", slog.Any("err", err))
		return
	}
	states, _ := l.store.ListStates()
	changed := false
	for _, li := range lists {
		n, version, err := l.fetch(ctx, li, states[li.Key].Entries)
		now := time.Now()
		if err != nil {
			slog.Warn("blocklist refresh failed", slog.String("list", li.Key), slog.Any("err", err))
			_ = l.store.ListFailed(li.Key, err.Error(), now)
			continue
		}
		moved := version != states[li.Key].Version
		changed = changed || moved
		_ = l.store.ListFetched(li.Key, n, version, moved, now)
	}
	if changed || l.tab.Load() == nil {
		l.load()
		slog.Info("blocklists loaded", slog.Int("entries", len(l.tab.Load().off)))
	}
}

func (l *lister) fetch(ctx context.Context, li List, had int) (int, string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, li.URL, nil)
	resp, err := l.http.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, "", fmt.Errorf("answered %d", resp.StatusCode)
	}
	tmp := l.path(li.Key) + ".new"
	f, err := os.Create(tmp)
	if err != nil {
		return 0, "", err
	}
	defer os.Remove(tmp)
	if _, err := io.Copy(f, io.LimitReader(resp.Body, 256<<20)); err != nil {
		f.Close()
		return 0, "", err
	}
	f.Close()
	f, err = os.Open(tmp)
	if err != nil {
		return 0, "", err
	}
	domains, version, err := parse(f)
	f.Close()
	if err != nil {
		return 0, "", err
	}
	n := len(domains)
	switch {
	case n < minEntries:
		return 0, "", fmt.Errorf("%w, only %d entries", errSwing, n)
	case had > 0 && float64(n) < float64(had)*maxShrink:
		return 0, "", fmt.Errorf("%w, %d entries down from %d", errSwing, n, had)
	case had > 0 && float64(n) > float64(had)*maxGrow:
		return 0, "", fmt.Errorf("%w, %d entries up from %d", errSwing, n, had)
	}
	return n, version, os.Rename(tmp, l.path(li.Key))
}
