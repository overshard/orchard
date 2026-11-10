package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"errors"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS ponds (
    id              INTEGER PRIMARY KEY,
    key_hmac        TEXT NOT NULL UNIQUE,
    public_id       TEXT NOT NULL UNIQUE,
    created_at      INTEGER NOT NULL,
    last_seen_at    INTEGER NOT NULL,
    last_gift_at    INTEGER NOT NULL,
    pebbles         INTEGER NOT NULL,
    free_spins      INTEGER NOT NULL,
    spin_at         INTEGER NOT NULL,
    paid_spins      INTEGER NOT NULL DEFAULT 0,
    paid_spins_day  TEXT NOT NULL DEFAULT '',
    log_day         TEXT NOT NULL DEFAULT '',
    dive_best       INTEGER NOT NULL DEFAULT 0,
    dive_day        TEXT NOT NULL DEFAULT '',
    dive_runs       INTEGER NOT NULL DEFAULT 0,
    dive_paid       INTEGER NOT NULL DEFAULT 0,
    dive_run        TEXT NOT NULL DEFAULT '',
    dive_started_at INTEGER NOT NULL DEFAULT 0,
    dive_ghosts     TEXT NOT NULL DEFAULT '',
    dive_podium     INTEGER NOT NULL DEFAULT 0,
    pads            INTEGER NOT NULL DEFAULT 3,
    gravel_level    INTEGER NOT NULL DEFAULT 0,
    gravel_at       INTEGER NOT NULL DEFAULT 0,
    garden_level    INTEGER NOT NULL DEFAULT 0,
    dive_form       REAL NOT NULL DEFAULT 0,
    dye_level       INTEGER NOT NULL DEFAULT 0,
    dye_at          INTEGER NOT NULL DEFAULT 0,
    dye_batch       TEXT NOT NULL DEFAULT '',
    trinkets_out    TEXT NOT NULL DEFAULT '',
    forage_day      TEXT NOT NULL DEFAULT '',
    key_saved_at    INTEGER
);
CREATE INDEX IF NOT EXISTS ponds_created ON ponds(created_at);

CREATE TABLE IF NOT EXISTS frogs (
    id          INTEGER PRIMARY KEY,
    pond_id     INTEGER NOT NULL REFERENCES ponds(id) ON DELETE CASCADE,
    seed        INTEGER NOT NULL,
    name        TEXT NOT NULL,
    hatched_at  INTEGER NOT NULL,
    out_front   INTEGER NOT NULL DEFAULT 1,
    hat_shape   INTEGER NOT NULL DEFAULT 0,
    hat_colour  INTEGER NOT NULL DEFAULT 0,
    genome_json TEXT NOT NULL DEFAULT '',
    laid_at     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS frogs_pond ON frogs(pond_id);

CREATE TABLE IF NOT EXISTS hats (
    pond_id  INTEGER NOT NULL REFERENCES ponds(id) ON DELETE CASCADE,
    shape    INTEGER NOT NULL,
    colour   INTEGER NOT NULL,
    count    INTEGER NOT NULL,
    PRIMARY KEY (pond_id, shape, colour)
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS items (
    pond_id  INTEGER NOT NULL REFERENCES ponds(id) ON DELETE CASCADE,
    kind     TEXT NOT NULL,
    count    INTEGER NOT NULL,
    PRIMARY KEY (pond_id, kind)
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS plots (
    pond_id  INTEGER NOT NULL REFERENCES ponds(id) ON DELETE CASCADE,
    slot     INTEGER NOT NULL,
    sown     REAL NOT NULL,
    watered  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (pond_id, slot)
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS cookbook (
    pond_id  INTEGER NOT NULL REFERENCES ponds(id) ON DELETE CASCADE,
    dish     TEXT NOT NULL,
    first_at INTEGER NOT NULL,
    PRIMARY KEY (pond_id, dish)
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS bought (
    pond_id  INTEGER NOT NULL REFERENCES ponds(id) ON DELETE CASCADE,
    stock    TEXT NOT NULL,
    slot     INTEGER NOT NULL,
    n        INTEGER NOT NULL,
    day      TEXT NOT NULL,
    PRIMARY KEY (pond_id, stock, slot)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS bought_day ON bought(day);

CREATE TABLE IF NOT EXISTS trinkets_found (
    pond_id  INTEGER NOT NULL REFERENCES ponds(id) ON DELETE CASCADE,
    kind     TEXT NOT NULL,
    PRIMARY KEY (pond_id, kind)
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS rocks_found (
    pond_id  INTEGER NOT NULL REFERENCES ponds(id) ON DELETE CASCADE,
    kind     TEXT NOT NULL,
    PRIMARY KEY (pond_id, kind)
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS keepsakes (
    pond_id  INTEGER NOT NULL REFERENCES ponds(id) ON DELETE CASCADE,
    kind     TEXT NOT NULL,
    day      TEXT NOT NULL,
    PRIMARY KEY (pond_id, kind, day)
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS stall (
    pond_id   INTEGER NOT NULL REFERENCES ponds(id) ON DELETE CASCADE,
    slot      INTEGER NOT NULL,
    kind      TEXT NOT NULL,
    price     INTEGER NOT NULL,
    listed_at INTEGER NOT NULL,
    sells_at  INTEGER NOT NULL,
    buyer     TEXT NOT NULL,
    PRIMARY KEY (pond_id, slot)
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS job_xp (
    pond_id  INTEGER NOT NULL REFERENCES ponds(id) ON DELETE CASCADE,
    frog_id  INTEGER NOT NULL REFERENCES frogs(id) ON DELETE CASCADE,
    job      TEXT NOT NULL,
    xp       INTEGER NOT NULL,
    PRIMARY KEY (frog_id, job)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS job_xp_pond ON job_xp(pond_id);

CREATE TABLE IF NOT EXISTS neighbours (
    pond_id  INTEGER NOT NULL REFERENCES ponds(id) ON DELETE CASCADE,
    who      INTEGER NOT NULL,
    points   INTEGER NOT NULL DEFAULT 0,
    gift_day TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (pond_id, who)
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS arcade (
    pond_id    INTEGER NOT NULL REFERENCES ponds(id) ON DELETE CASCADE,
    game       TEXT NOT NULL,
    day        TEXT NOT NULL DEFAULT '',
    runs       INTEGER NOT NULL DEFAULT 0,
    paid       INTEGER NOT NULL DEFAULT 0,
    best       INTEGER NOT NULL DEFAULT 0,
    run        TEXT NOT NULL DEFAULT '',
    started_at INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (pond_id, game)
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS market_days (
    day    TEXT NOT NULL,
    key    TEXT NOT NULL,
    close  REAL NOT NULL,
    PRIMARY KEY (key, day)
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS shares (
    pond_id  INTEGER NOT NULL REFERENCES ponds(id) ON DELETE CASCADE,
    key      TEXT NOT NULL,
    count    INTEGER NOT NULL,
    cost     INTEGER NOT NULL,
    PRIMARY KEY (pond_id, key)
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS growth (
    id    INTEGER PRIMARY KEY CHECK (id = 1),
    at    INTEGER NOT NULL,
    total REAL NOT NULL
);
`

// Columns added since the first deploy. CREATE TABLE IF NOT EXISTS leaves a
// table that is already there alone, so these are added by hand.
var addedColumns = []struct{ table, name, def string }{
	{"ponds", "dive_ghosts", "TEXT NOT NULL DEFAULT ''"},
	{"ponds", "dive_podium", "INTEGER NOT NULL DEFAULT 0"},
	{"ponds", "pads", "INTEGER NOT NULL DEFAULT 3"},
	{"frogs", "genome_json", "TEXT NOT NULL DEFAULT ''"},
	{"frogs", "laid_at", "INTEGER NOT NULL DEFAULT 0"},
	{"ponds", "gravel_level", "INTEGER NOT NULL DEFAULT 0"},
	{"ponds", "gravel_at", "INTEGER NOT NULL DEFAULT 0"},
	{"ponds", "garden_level", "INTEGER NOT NULL DEFAULT 0"},
	{"ponds", "dive_form", "REAL NOT NULL DEFAULT 0"},
	{"ponds", "dye_level", "INTEGER NOT NULL DEFAULT 0"},
	{"ponds", "dye_at", "INTEGER NOT NULL DEFAULT 0"},
	{"ponds", "dye_batch", "TEXT NOT NULL DEFAULT ''"},
	{"ponds", "trinkets_out", "TEXT NOT NULL DEFAULT ''"},
	{"ponds", "forage_day", "TEXT NOT NULL DEFAULT ''"},
	{"frogs", "fed", "TEXT NOT NULL DEFAULT ''"},
	{"frogs", "fed_at", "INTEGER NOT NULL DEFAULT 0"},
	{"frogs", "job", "TEXT NOT NULL DEFAULT ''"},
	{"frogs", "job_at", "INTEGER NOT NULL DEFAULT 0"},
	{"frogs", "recipe", "TEXT NOT NULL DEFAULT ''"},
	{"frogs", "carry", "REAL NOT NULL DEFAULT 0"},
	{"ponds", "favour_day", "TEXT NOT NULL DEFAULT ''"},
	{"ponds", "burrow_level", "INTEGER NOT NULL DEFAULT 0"},
	{"ponds", "burrow_at", "INTEGER NOT NULL DEFAULT 0"},
	{"ponds", "lodgers", "TEXT NOT NULL DEFAULT ''"},
	{"ponds", "rent_day", "TEXT NOT NULL DEFAULT ''"},
	{"ponds", "starter", "INTEGER NOT NULL DEFAULT 16"},
}

func ensureColumns(db *sql.DB) error {
	have := map[string]bool{}
	rows, err := db.Query(`SELECT m.name || '.' || c.name FROM sqlite_schema m
		JOIN pragma_table_info(m.name) c WHERE m.type = 'table'`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		have[name] = true
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, c := range addedColumns {
		if have[c.table+"."+c.name] {
			continue
		}
		if _, err := db.Exec("ALTER TABLE " + c.table + " ADD COLUMN " + c.name + " " + c.def); err != nil {
			return err
		}
	}
	return nil
}

var (
	errNoPond    = errors.New("no pond")
	errFull      = errors.New("the pond is full for the moment, try again in a little while")
	errPoor      = errors.New("not enough pebbles")
	errNotYet    = errors.New("not yet")
	errNotHeld   = errors.New("you don't have that one")
	errBadRun    = errors.New("that run doesn't count")
	errFullPads  = errors.New("every pad has a frog on it, so the pond needs another pad first")
	errSoldOut   = errors.New("you've bought all of that there is for now")
	errBadName   = errors.New("that name isn't on the list")
	errFed       = errors.New("it's still full from its last meal")
	errBadPrice  = errors.New("that price is off the scale")
	errStallFull = errors.New("the stall is full")
)

// store keeps one writing connection and a small pool that only reads. Every
// writing transaction opens with BEGIN IMMEDIATE, since a deferred one that
// reads first can fail to upgrade without ever waiting on busy_timeout.
type store struct {
	w, r   *sql.DB
	pepper []byte
	day    almanac
	now    func() time.Time

	// Seconds added to the clock, moved by the dev route and nothing else.
	skew atomic.Int64
}

func openStore(path string, pepper []byte) (*store, error) {
	// modernc ignores mattn's parameter names, so the pragmas are _pragma and
	// the store test reads them back. temp_store is memory because the image
	// has no /tmp for SQLite to spill a sort into.
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)&_pragma=temp_store(MEMORY)"
	w, err := sql.Open("sqlite", dsn+"&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	if _, err := w.Exec(schema); err != nil {
		w.Close()
		return nil, err
	}
	if err := ensureColumns(w); err != nil {
		w.Close()
		return nil, err
	}
	if err := backfillRocks(w); err != nil {
		w.Close()
		return nil, err
	}
	// A frog fed before meals ran on a clock is full from now.
	if _, err := w.Exec(`UPDATE frogs SET fed_at = unixepoch() WHERE fed != '' AND fed_at = 0`); err != nil {
		w.Close()
		return nil, err
	}
	r, err := sql.Open("sqlite", dsn+"&_pragma=query_only(1)")
	if err != nil {
		w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(4)
	_, _ = w.Exec("PRAGMA optimize=0x10002")
	s := &store{w: w, r: r, pepper: pepper}
	s.now = func() time.Time { return time.Now().Add(time.Duration(s.skew.Load()) * time.Second) }
	return s, nil
}

func (s *store) Close() {
	_, _ = s.w.Exec("PRAGMA optimize")
	s.r.Close()
	s.w.Close()
}

func (s *store) write(fn func(tx *sql.Tx) error) error {
	tx, err := s.w.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

type querier interface {
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
}

type pond struct {
	ID         int64
	PublicID   string
	CreatedAt  int64
	LastSeenAt int64
	LastGiftAt int64
	Pebbles    int
	FreeSpins  int
	SpinAt     int64
	PaidSpins  int
	PaidDay    string
	LogDay     string
	DiveBest   int
	DiveDay    string
	DiveRuns   int
	DivePaid   int
	DiveRun    string
	DiveStart  int64
	DiveGhosts string
	DivePodium int
	Pads       int
	KeySavedAt sql.NullInt64

	GravelLevel int
	GravelAt    int64
	GardenLevel int
	DiveForm    float64
	DyeLevel    int
	DyeAt       int64
	DyeBatch    string
	TrinketsOut string
	ForageDay   string

	FrogID    int64
	Seed      uint32
	ModsJSON  string
	HatchedAt int64
	Wearing   hat
	FrogName  string
}

const pondColumns = `p.id, p.public_id, p.created_at, p.last_seen_at, p.last_gift_at,
	p.pebbles, p.free_spins, p.spin_at, p.paid_spins, p.paid_spins_day, p.log_day,
	p.dive_best, p.dive_day, p.dive_runs, p.dive_paid, p.dive_run, p.dive_started_at,
	p.dive_ghosts, p.dive_podium, p.pads, p.key_saved_at,
	p.gravel_level, p.gravel_at, p.garden_level, p.dive_form,
	p.dye_level, p.dye_at, p.dye_batch, p.trinkets_out, p.forage_day, f.id, f.seed, f.genome_json, f.hatched_at,
	f.hat_shape, f.hat_colour, f.name`

func scanPond(row *sql.Row) (*pond, error) {
	var p pond
	err := row.Scan(&p.ID, &p.PublicID, &p.CreatedAt, &p.LastSeenAt, &p.LastGiftAt,
		&p.Pebbles, &p.FreeSpins, &p.SpinAt, &p.PaidSpins, &p.PaidDay, &p.LogDay,
		&p.DiveBest, &p.DiveDay, &p.DiveRuns, &p.DivePaid, &p.DiveRun, &p.DiveStart,
		&p.DiveGhosts, &p.DivePodium, &p.Pads, &p.KeySavedAt,
		&p.GravelLevel, &p.GravelAt, &p.GardenLevel, &p.DiveForm,
		&p.DyeLevel, &p.DyeAt, &p.DyeBatch, &p.TrinketsOut, &p.ForageDay, &p.FrogID, &p.Seed, &p.ModsJSON, &p.HatchedAt,
		&p.Wearing.Shape, &p.Wearing.Colour, &p.FrogName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNoPond
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func loadPond(q querier, id int64) (*pond, error) {
	return scanPond(q.QueryRow(`SELECT `+pondColumns+` FROM ponds p
		JOIN frogs f ON f.pond_id = p.id AND f.out_front = 1 WHERE p.id = ?`, id))
}

func savePond(tx *sql.Tx, p *pond) error {
	if _, err := tx.Exec(`UPDATE ponds SET last_seen_at = ?, last_gift_at = ?, pebbles = ?,
		free_spins = ?, spin_at = ?, paid_spins = ?, paid_spins_day = ?, log_day = ?,
		dive_best = ?, dive_day = ?, dive_runs = ?, dive_paid = ?, dive_run = ?,
		dive_started_at = ?, dive_ghosts = ?, dive_podium = ?, pads = ?, key_saved_at = ?,
		gravel_level = ?, gravel_at = ?, garden_level = ?, dive_form = ?,
		dye_level = ?, dye_at = ?, dye_batch = ?, trinkets_out = ?, forage_day = ? WHERE id = ?`,
		p.LastSeenAt, p.LastGiftAt, p.Pebbles, p.FreeSpins, p.SpinAt, p.PaidSpins, p.PaidDay,
		p.LogDay, p.DiveBest, p.DiveDay, p.DiveRuns, p.DivePaid, p.DiveRun, p.DiveStart,
		p.DiveGhosts, p.DivePodium, p.Pads, p.KeySavedAt,
		p.GravelLevel, p.GravelAt, p.GardenLevel, p.DiveForm,
		p.DyeLevel, p.DyeAt, p.DyeBatch, p.TrinketsOut, p.ForageDay, p.ID); err != nil {
		return err
	}
	_, err := tx.Exec(`UPDATE frogs SET hat_shape = ?, hat_colour = ? WHERE id = ?`,
		p.Wearing.Shape, p.Wearing.Colour, p.FrogID)
	return err
}

// settle brings the counters that run on the clock up to now. Free spins come
// in one every spinEvery up to the most a pond holds, and the daily counts
// start again on a new day.
func (p *pond) settle(now time.Time) {
	if p.FreeSpins >= maxFreeSpins {
		p.SpinAt = now.Unix()
	} else if got := int((now.Unix() - p.SpinAt) / int64(spinEvery/time.Second)); got > 0 {
		p.FreeSpins += got
		p.SpinAt += int64(got) * int64(spinEvery/time.Second)
		if p.FreeSpins >= maxFreeSpins {
			p.FreeSpins = maxFreeSpins
			p.SpinAt = now.Unix()
		}
	}
	today := dayOf(now)
	if p.PaidDay != today {
		p.PaidDay, p.PaidSpins = today, 0
	}
	if p.DiveDay != today {
		p.DiveDay, p.DiveRuns, p.DivePaid, p.DivePodium = today, 0, 0, 0
	}
}

// idOf finds the pond a key opens, or errNoPond.
func (s *store) idOf(key string) (int64, error) {
	var id int64
	err := s.r.QueryRow(`SELECT id FROM ponds WHERE key_hmac = ?`, keyHMAC(s.pepper, key)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errNoPond
	}
	return id, err
}

func randomSeed() uint32 {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic(err)
	}
	return binary.BigEndian.Uint32(buf[:])
}

// create lays an egg in a new pond and hands back the key that opens it.
func (s *store) create() (string, int64, error) {
	key, now := newKey(), s.now().Unix()
	var id int64
	err := s.write(func(tx *sql.Tx) error {
		var recent int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM ponds WHERE created_at > ?`,
			now-3600).Scan(&recent); err != nil {
			return err
		}
		if recent >= pondsAnHour {
			return errFull
		}
		res, err := tx.Exec(`INSERT INTO ponds (key_hmac, public_id, created_at, last_seen_at,
			last_gift_at, pebbles, free_spins, spin_at, starter) VALUES (?, ?, ?, ?, ?, ?, 1, ?, 0)`,
			keyHMAC(s.pepper, key), randomHex(8), now, now, now, starterPebbles, now)
		if err != nil {
			return err
		}
		id, _ = res.LastInsertId()
		seed := randomSeed()
		_, err = tx.Exec(`INSERT INTO frogs (pond_id, seed, name, hatched_at) VALUES (?, ?, ?, ?)`,
			id, seed, newGenome(seed).Name, now)
		return err
	})
	return key, id, err
}

// rotate gives the pond a new key, and the old one stops opening anything.
func (s *store) rotate(id int64) (string, error) {
	key := newKey()
	err := s.write(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE ponds SET key_hmac = ? WHERE id = ?`, keyHMAC(s.pepper, key), id)
		return err
	})
	return key, err
}

func (s *store) keySeen(id int64) error {
	return s.write(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE ponds SET key_saved_at = ? WHERE id = ? AND key_saved_at IS NULL`,
			s.now().Unix(), id)
		return err
	})
}

// sweep clears eggs nobody came back to within a week. A pond that has done
// anything at all is kept for good.
func (s *store) sweep() (int64, error) {
	var gone int64
	err := s.write(func(tx *sql.Tx) error {
		res, err := tx.Exec(`DELETE FROM ponds WHERE created_at < ?
			AND last_seen_at < created_at + 3600 AND pebbles = ? AND dive_best = 0
			AND log_day = '' AND key_saved_at IS NULL
			AND NOT EXISTS (SELECT 1 FROM hats WHERE pond_id = ponds.id)
			AND NOT EXISTS (SELECT 1 FROM items WHERE pond_id = ponds.id)`,
			s.now().Unix()-7*86400, starterPebbles)
		if err != nil {
			return err
		}
		gone, _ = res.RowsAffected()
		// What was bought only counts against today's stock.
		_, err = tx.Exec(`DELETE FROM bought WHERE day < ?`, dayOf(s.now().AddDate(0, 0, -2)))
		return err
	})
	return gone, err
}

func addHat(tx *sql.Tx, id int64, h hat) (int, error) {
	var count int
	err := tx.QueryRow(`INSERT INTO hats (pond_id, shape, colour, count) VALUES (?, ?, ?, 1)
		ON CONFLICT (pond_id, shape, colour) DO UPDATE SET count = count + 1
		RETURNING count`, id, h.Shape, h.Colour).Scan(&count)
	return count, err
}

func addItem(tx *sql.Tx, id int64, kind string, n int) error {
	if isRock(kind) {
		if _, err := tx.Exec(`INSERT INTO rocks_found (pond_id, kind) VALUES (?, ?) ON CONFLICT DO NOTHING`, id, kind); err != nil {
			return err
		}
	}
	_, err := tx.Exec(`INSERT INTO items (pond_id, kind, count) VALUES (?, ?, ?)
		ON CONFLICT (pond_id, kind) DO UPDATE SET count = count + excluded.count`, id, kind, n)
	return err
}

// award puts a prize in the pond and fills in what the page says about a hat,
// which depends on whether it was already there.
func award(tx *sql.Tx, p *pond, z *prize) error {
	switch {
	case z.Hat != nil:
		count, err := addHat(tx, p.ID, *z.Hat)
		if err != nil {
			return err
		}
		z.HatName, z.HatTier, z.Spare = z.Hat.name(), z.Hat.tier(), count > 1
	case strings.HasPrefix(z.Item, "t_"):
		count, err := addTrinket(tx, p.ID, z.Item)
		if err != nil {
			return err
		}
		t, _ := trinketOf(z.Item)
		z.HatTier, z.Spare = t.tier, count > 1
	case z.Item != "":
		return addItem(tx, p.ID, z.Item, 1)
	default:
		p.Pebbles += z.Pebbles
	}
	return nil
}

// ---------------------------------------------------------------- the view

type hatView struct {
	hat
	Name  string `json:"name"`
	Tier  int    `json:"tier"`
	Count int    `json:"count"`
	Wren  int    `json:"wren"`
}

type itemView struct {
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type frogView struct {
	genome
	Mods   *mods  `json:"mods"`
	Stage  string `json:"stage"`
	Day    int    `json:"day"`
	Rarity rarity `json:"rarity"`
	Hat    hat    `json:"hat"`
}

// reedView is a frog in the collection, out front or not.
type reedView struct {
	frogView
	ID    int64 `json:"id"`
	Front bool  `json:"front"`
	// Seconds until it can lay, 0 for now and -1 while it's too young.
	LayIn int64     `json:"lay_in"`
	Fed   *mealView `json:"fed,omitempty"`
	Work  *workView `json:"work,omitempty"`
}

type mealView struct {
	Kind  string  `json:"kind"`
	Name  string  `json:"name"`
	Tier  int     `json:"tier"`
	Takes float64 `json:"takes"`
	Eyes  float64 `json:"eyes"`
	Boost float64 `json:"boost"`
	// Seconds until it's hungry again.
	Left int64 `json:"left"`
}

type view struct {
	Frog    frogView `json:"frog"`
	Pebbles int      `json:"pebbles"`
	Wheel   struct {
		Slices   []string `json:"slices"`
		Hat      hatView  `json:"hat"`
		Dye      itemView `json:"dye"`
		Free     int      `json:"free"`
		NextIn   int64    `json:"next_in"`
		PaidLeft int      `json:"paid_left"`
		PaidCost int      `json:"paid_cost"`
	} `json:"wheel"`
	Log struct {
		Ready bool `json:"ready"`
	} `json:"log"`
	Dive struct {
		Best int `json:"best"`
		Paid int `json:"paid"`
		Cap  int `json:"cap"`
		Runs int `json:"runs"`
		Full int `json:"full"`
		// Which of first, second and third have paid their prize today.
		Podium [3]bool `json:"podium"`
	} `json:"dive"`
	Box struct {
		Cost int            `json:"cost"`
		Odds [tierCount]int `json:"odds"`
	} `json:"box"`
	Frogs   []reedView `json:"frogs"`
	Pads    int        `json:"pads"`
	Jobs    jobsView   `json:"jobs"`
	PadCost int        `json:"pad_cost"`
	Hats    []hatView  `json:"hats"`
	Hoods   []hoodView `json:"hoods"`
	Items   []itemView `json:"items"`
	Share   struct {
		Path  string `json:"path"`
		Power power  `json:"power"`
	} `json:"share"`
	Day        dayView             `json:"day"`
	Market     marketView          `json:"market"`
	Gazette    gazette             `json:"gazette"`
	Bank       bankView            `json:"bank"`
	Kitchen    kitchenView         `json:"kitchen"`
	Shops      map[string]shopView `json:"shops"`
	Trinkets   trinketsView        `json:"trinkets"`
	Rocks      []rockView          `json:"rocks"`
	Keepsakes  []keepsakeView      `json:"keepsakes"`
	Neighbours neighboursView      `json:"neighbours"`
	Arcade     []arcadeView        `json:"arcade"`
	// What Fenn sold this pond, drawn on the water.
	Furniture []string   `json:"furniture"`
	Bag       []bagThing `json:"bag"`
	Stall     stallView  `json:"stall"`
	Wizard    struct {
		Takes int `json:"takes"`
		Up    int `json:"up"`
		Same  int `json:"same"`
	} `json:"wizard"`

	Todo    []todo       `json:"todo"`
	Starter *starterView `json:"starter,omitempty"`

	held  map[string]int
	found []hat
	Nudge bool `json:"nudge"`
	Dev   bool `json:"dev"`
}

func frogOf(p *pond, now time.Time) frogView {
	return frogViewOf(frogRow{Seed: p.Seed, Mods: parseMods(p.ModsJSON), HatchedAt: p.HatchedAt, Hat: p.Wearing, Name: p.FrogName}, now)
}

func frogViewOf(f frogRow, now time.Time) frogView {
	g := genomeOf(f.Seed, f.Mods)
	if f.Name != "" {
		g.Name = f.Name
	}
	stage, day := stageOf(now.Sub(time.Unix(f.HatchedAt, 0)))
	return frogView{genome: g, Mods: f.Mods, Stage: stage, Day: day, Rarity: rarityOf(g), Hat: f.Hat}
}

// viewOf settles a copy, so reading a pond never writes to it.
func viewOf(q querier, p pond, now time.Time) (*view, error) {
	p.settle(now)
	v := &view{Frog: frogOf(&p, now), Pebbles: p.Pebbles, Dev: devBuild}
	v.Wheel.Slices = wheelSlices
	dayHat, dayDye := wheelOfDay(dayOf(now))
	v.Wheel.Hat = hatView{hat: dayHat, Name: dayHat.name(), Tier: dayHat.tier()}
	v.Wheel.Dye = itemView{Kind: dayDye, Name: itemNames[dayDye]}
	v.Wheel.Free = p.FreeSpins
	if p.FreeSpins < maxFreeSpins {
		v.Wheel.NextIn = p.SpinAt + int64(spinEvery/time.Second) - now.Unix()
	}
	v.Wheel.PaidLeft = paidSpinsADay - p.PaidSpins
	v.Wheel.PaidCost = paidSpinCost
	v.Log.Ready = p.LogDay != dayOf(now)
	v.Dive.Best, v.Dive.Paid, v.Dive.Cap = p.DiveBest, p.DivePaid, diveCap
	v.Dive.Runs, v.Dive.Full = p.DiveRuns, diveFullRuns
	for i := range v.Dive.Podium {
		v.Dive.Podium[i] = p.DivePodium&(1<<i) != 0
	}
	v.Box.Cost, v.Box.Odds = boxCost, boxOdds
	// Three days in, the page says once to save the key, until it has been.
	v.Nudge = !p.KeySavedAt.Valid && now.Unix()-p.CreatedAt >= 3*86400

	held := map[hat]int{}
	rows, err := q.Query(`SELECT shape, colour, count FROM hats WHERE pond_id = ?`, p.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var h hat
		var n int
		if err := rows.Scan(&h.Shape, &h.Colour, &n); err != nil {
			rows.Close()
			return nil, err
		}
		held[h] = n
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	var found []hat
	for t := range hatsByTier {
		for _, h := range hatsByTier[t] {
			v.Hats = append(v.Hats, hatView{h, h.name(), t, held[h], wrenPays(h)})
			if held[h] > 0 {
				found = append(found, h)
			}
		}
	}
	frogs, err := loadFrogs(q, p.ID)
	if err != nil {
		return nil, err
	}
	xp, err := loadXP(q, p.ID)
	if err != nil {
		return nil, err
	}
	v.Jobs = jobsView{Defs: jobDefs(), Recipes: recipeViews(), Hold: int(workHold / time.Hour), Total: bestLevels(xp)}
	earned := hoodsEarned(xp)
	found = append(found, earned...)
	for i, j := range jobs {
		h := hoodFor(i)
		v.Hoods = append(v.Hoods, hoodView{h, h.name(), j.key, slices.Contains(earned, h)})
	}
	for _, f := range frogs {
		if f.Front {
			f.Hat = p.Wearing
		}
		in := layIn(f, now)
		if in > 0 {
			in = in/time.Second + 1
		}
		rv := reedView{frogView: frogViewOf(f, now), ID: f.ID, Front: f.Front, LayIn: int64(in)}
		if d, ok := dishOfKind(f.Fed); ok && fullUntil(f) > now.Unix() {
			rv.Fed = &mealView{f.Fed, d.name, d.tier, mealTakes[d.tier], mealEyes[d.tier], mealBoost[d.tier], fullUntil(f) - now.Unix()}
		}
		if canWork(f, now) {
			rv.Work = workOf(f, xpOf(xp, f.ID), now)
			v.Jobs.Waiting += rv.Work.Hours
		}
		v.Frogs = append(v.Frogs, rv)
	}
	v.Pads = p.Pads
	bought, err := boughtPads(q, &p)
	if err != nil {
		return nil, err
	}
	if bought < mostPads {
		v.PadCost = padCost(bought)
	}
	v.Share.Path = "/f/" + slug(v.Frog.Name, p.PublicID)
	v.found = found

	v.Items = []itemView{}
	v.Furniture = []string{}
	v.held = map[string]int{}
	rows, err = q.Query(`SELECT kind, count FROM items WHERE pond_id = ? AND count > 0`, p.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var it itemView
		if err := rows.Scan(&it.Kind, &it.Count); err != nil {
			return nil, err
		}
		v.held[it.Kind] = it.Count
		if strings.HasPrefix(it.Kind, "t_") {
			continue
		}
		if strings.HasPrefix(it.Kind, "f_") {
			v.Furniture = append(v.Furniture, it.Kind)
			continue
		}
		if _, food := ingredientOf(it.Kind); food || strings.HasPrefix(it.Kind, "dish_") {
			continue
		}
		it.Name = itemNames[it.Kind]
		v.Items = append(v.Items, it)
	}
	sort.Slice(v.Items, func(a, b int) bool { return v.Items[a].Name < v.Items[b].Name })
	return v, rows.Err()
}

func (s *store) view(id int64) (*view, error) {
	p, err := loadPond(s.r, id)
	if err != nil {
		return nil, err
	}
	now := s.now()
	v, err := viewOf(s.r, *p, now)
	if err != nil {
		return nil, err
	}
	return v, s.fill(s.r, v, p, now)
}

// act runs one move on a pond: load it, settle it, let fn change it, save it,
// and hand back the pond as the page should now show it.
func (s *store) act(id int64, fn func(tx *sql.Tx, p *pond, now time.Time) error) (*view, error) {
	var v *view
	err := s.write(func(tx *sql.Tx) error {
		p, err := loadPond(tx, id)
		if err != nil {
			return err
		}
		now := s.now()
		p.settle(now)
		if err := fn(tx, p, now); err != nil {
			return err
		}
		if err := savePond(tx, p); err != nil {
			return err
		}
		v, err = viewOf(tx, *p, now)
		if err != nil {
			return err
		}
		return s.fill(tx, v, p, now)
	})
	return v, err
}

// fill adds the parts of the view that read the real day.
func (s *store) fill(q querier, v *view, p *pond, now time.Time) error {
	r := s.day.reading(now)
	v.Day = dayOfReading(r, now)
	season(&v.Day, now)
	today := dayOf(now)
	if v.Frog.Stage != "egg" {
		v.Day.Hobby = hobbyOf(r, v.Day.Mood, p.Seed, frogOf(p, now).genome.Nature, today)
	}
	v.Day.Lines = append(v.Day.Lines, [2]string{"Fortune", fortuneOf(p.ID, today).Line})
	var err error
	v.Bank, v.Kitchen, err = s.bankOf(q, p, now, v.held)
	if err != nil {
		return err
	}
	if v.Shops, err = s.shopsOf(q, p.ID, now); err != nil {
		return err
	}
	if v.Trinkets, err = trinketsOf(q, p, v.held); err != nil {
		return err
	}
	if v.Rocks, err = rocksOf(q, p.ID, v.held); err != nil {
		return err
	}
	if v.Keepsakes, err = keepsakesOf(q, p.ID); err != nil {
		return err
	}
	if v.Stall, err = stallOf(q, p.ID, now); err != nil {
		return err
	}
	if v.Market, err = marketOf(q, p, r, now); err != nil {
		return err
	}
	if v.Neighbours, err = neighboursOf(q, p.ID, now); err != nil {
		return err
	}
	for i := range v.Neighbours.All {
		for _, f := range v.Market.Firms {
			if f.Key == residentFirm(i).Key {
				v.Neighbours.All[i].Firm = f.Name
				v.Neighbours.All[i].Talk = firmTalk(f, residents[i].seed)
			}
		}
		v.Neighbours.All[i].Gossip = gossipOf(i, today)
	}
	v.Gazette = gazetteFor(v.Day.Mood, now)
	if v.Day.Mood != moodSombre {
		v.Gazette.Business = marketHeadline(v.Market.Firms, today)
	}
	if v.Arcade, err = arcadeViews(q, p.ID, now); err != nil {
		return err
	}
	v.Bag = bagOf(v.held, v.Hats, v.Hoods)
	v.Wizard.Takes, v.Wizard.Up, v.Wizard.Same = wizardTakes, wizardOdds.up, wizardOdds.same
	had := map[string]bool{}
	for _, t := range v.Trinkets.All {
		had[t.Kind] = t.Found
	}
	for _, r := range v.Rocks {
		had[r.Kind] = r.Found
	}
	for _, k := range v.Keepsakes {
		had[k.Kind] = k.First != ""
	}
	v.Share.Power = powerOf(v.Frog.Rarity, v.found, had, p.DiveBest, v.Jobs.Total)
	v.Todo = todoOf(v)
	if v.Starter, err = starterOf(q, p); err != nil {
		return err
	}
	return nil
}
