package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Vault reads Vaultwarden's database. Names, notes and every secret in it are
// encrypted by the apps, so what's readable is counts, times, device names and
// the addresses in its event log. The one write is the authenticator setting.
type Vault struct {
	db   *sql.DB
	path string
}

// OpenVault opens read only. The volume is mounted writable anyway, since
// SQLite needs the -shm file to read a database in WAL mode.
func OpenVault(path string) (*Vault, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=busy_timeout(5000)&_pragma=query_only(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(2)
	return &Vault{db: db, path: path}, nil
}

func (v *Vault) Close() error { return v.db.Close() }

// Vaultwarden writes naive UTC datetimes as text.
const vwTime = "2006-01-02 15:04:05"

func parseVW(s string) time.Time {
	for _, l := range []string{"2006-01-02 15:04:05.999999999", vwTime, time.RFC3339Nano} {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func vwArg(t time.Time) string { return t.UTC().Format(vwTime) }

// The user events Vaultwarden records when ORG_EVENTS_ENABLED is on. Each one
// is also copied once per organization the user is in, so only the copy with
// no organization is read.
const (
	evLogin          = 1000
	evChangedPass    = 1001
	evUpdated2FA     = 1002
	evDisabled2FA    = 1003
	evRecovered2FA   = 1004
	evFailedLogin    = 1005
	evFailed2FA      = 1006
	evExported       = 1007
	evDeviceApproval = 1010
)

type Event struct {
	ID     string
	At     time.Time
	Type   int
	Device int
	IP     string
}

func (e Event) Failed() bool { return e.Type == evFailedLogin || e.Type == evFailed2FA }

// Loud is anything worth a push on its own, a failure or a change to the
// account itself.
func (e Event) Loud() bool { return e.Type != evLogin }

func (v *Vault) Events(since time.Time, limit int) ([]Event, error) {
	rows, err := v.db.Query(`SELECT uuid, CAST(event_date AS TEXT), event_type, COALESCE(device_type, -1), COALESCE(ip_address, '')
		FROM event WHERE org_uuid IS NULL AND event_type BETWEEN 1000 AND 1099 AND event_date >= ?
		ORDER BY event_date DESC LIMIT ?`, vwArg(since), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var at string
		if err := rows.Scan(&e.ID, &at, &e.Type, &e.Device, &e.IP); err != nil {
			return nil, err
		}
		e.At = parseVW(at)
		out = append(out, e)
	}
	return out, rows.Err()
}

type Device struct {
	ID       string
	Name     string
	Type     int
	Created  time.Time
	LastSeen time.Time
	Push     bool
}

func (v *Vault) Devices() ([]Device, error) {
	rows, err := v.db.Query(`SELECT uuid, name, atype, CAST(created_at AS TEXT), CAST(updated_at AS TEXT),
		COALESCE(push_token, '') != '' FROM devices ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		var d Device
		var c, u string
		if err := rows.Scan(&d.ID, &d.Name, &d.Type, &c, &u, &d.Push); err != nil {
			return nil, err
		}
		d.Created, d.LastSeen = parseVW(c), parseVW(u)
		out = append(out, d)
	}
	return out, rows.Err()
}

// Holdings is what the vault holds, counted. Nothing in it is a secret.
type Holdings struct {
	Users, TwoFactor          int
	Logins, Notes, Cards, IDs int
	SSHKeys, Other, TOTP      int
	Trash, Folders, Sends     int
	Files                     int
	FileBytes                 int64
	Changed                   time.Time
}

func (h Holdings) Items() int { return h.Logins + h.Notes + h.Cards + h.IDs + h.SSHKeys + h.Other }

func (v *Vault) Holdings() (Holdings, error) {
	var h Holdings
	rows, err := v.db.Query(`SELECT atype, deleted_at IS NOT NULL, COUNT(*) FROM ciphers GROUP BY 1, 2`)
	if err != nil {
		return h, err
	}
	for rows.Next() {
		var kind, n int
		var trashed bool
		if err := rows.Scan(&kind, &trashed, &n); err != nil {
			rows.Close()
			return h, err
		}
		if trashed {
			h.Trash += n
			continue
		}
		switch kind {
		case 1:
			h.Logins += n
		case 2:
			h.Notes += n
		case 3:
			h.Cards += n
		case 4:
			h.IDs += n
		case 5:
			h.SSHKeys += n
		default:
			h.Other += n
		}
	}
	rows.Close()
	// The data column is JSON with each value encrypted, so whether a login
	// has a code is readable even though the code isn't.
	var changed sql.NullString
	err = v.db.QueryRow(`SELECT
		(SELECT COUNT(*) FROM users WHERE enabled),
		(SELECT COUNT(DISTINCT user_uuid) FROM twofactor WHERE enabled AND atype IN (0, 1, 2, 3, 4, 7)),
		(SELECT COUNT(*) FROM ciphers WHERE deleted_at IS NULL AND atype = 1 AND json_valid(data)
			AND COALESCE(json_extract(data, '$.totp'), json_extract(data, '$.Totp')) IS NOT NULL),
		(SELECT COUNT(*) FROM folders),
		(SELECT COUNT(*) FROM sends),
		(SELECT COUNT(*) FROM attachments),
		(SELECT COALESCE(SUM(file_size), 0) FROM attachments),
		(SELECT CAST(MAX(updated_at) AS TEXT) FROM ciphers)`).Scan(
		&h.Users, &h.TwoFactor, &h.TOTP, &h.Folders, &h.Sends, &h.Files, &h.FileBytes, &changed)
	if changed.Valid {
		h.Changed = parseVW(changed.String)
	}
	return h, err
}

// deviceKind names Bitwarden's device types the way the apps do.
func deviceKind(t int) string {
	names := map[int]string{
		0: "Android", 1: "iOS", 2: "Chrome extension", 3: "Firefox extension", 4: "Opera extension",
		5: "Edge extension", 6: "Windows app", 7: "macOS app", 8: "Linux app", 9: "Chrome",
		10: "Firefox", 11: "Opera", 12: "Edge", 13: "Internet Explorer", 14: "a browser",
		15: "Android", 16: "Windows app", 17: "Safari", 18: "Vivaldi", 19: "Vivaldi extension",
		20: "Safari extension", 21: "SDK", 22: "server", 23: "Windows CLI", 24: "macOS CLI",
		25: "Linux CLI", 26: "DuckDuckGo",
	}
	if n, ok := names[t]; ok {
		return n
	}
	if t < 0 {
		return "this page"
	}
	return fmt.Sprintf("device type %d", t)
}

func eventWord(t int) string {
	switch t {
	case evLogin:
		return "signed in"
	case evChangedPass:
		return "master password changed"
	case evUpdated2FA:
		return "two step login changed"
	case evDisabled2FA:
		return "two step login turned off"
	case evRecovered2FA:
		return "two step login recovered"
	case evFailedLogin:
		return "wrong password"
	case evFailed2FA:
		return "wrong two step code"
	case evExported:
		return "vault exported"
	case evDeviceApproval:
		return "device approval asked"
	}
	return fmt.Sprintf("event %d", t)
}

func eventKind(t int) string {
	switch t {
	case evLogin:
		return "ok"
	case evFailedLogin, evFailed2FA, evDisabled2FA, evRecovered2FA:
		return "bad"
	}
	return "warn"
}

func missingTable(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "no such table") || strings.Contains(err.Error(), "unable to open"))
}

// Account is the one user, and whether the authenticator is on for it.
type Account struct {
	ID, Email string
	Created   time.Time
	TOTP      bool
	Secret    string
	Others    int
}

var errNoAccount = errors.New("no account yet")

func (v *Vault) Account() (Account, error) {
	var a Account
	var created string
	err := v.db.QueryRow(`SELECT uuid, email, CAST(created_at AS TEXT) FROM users WHERE enabled ORDER BY created_at LIMIT 1`).Scan(&a.ID, &a.Email, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return a, errNoAccount
	}
	if err != nil {
		return a, err
	}
	a.Created = parseVW(created)
	var secret sql.NullString
	err = v.db.QueryRow(`SELECT data FROM twofactor WHERE user_uuid = ? AND atype = 0 AND enabled`, a.ID).Scan(&secret)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return a, err
	}
	a.TOTP, a.Secret = secret.Valid, secret.String
	err = v.db.QueryRow(`SELECT COUNT(*) FROM twofactor WHERE user_uuid = ? AND enabled AND atype IN (1, 2, 3, 4, 7)`, a.ID).Scan(&a.Others)
	return a, err
}

// writer opens a separate read write handle for the few writes, so every
// read stays on the read only one.
func (v *Vault) writer() (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+v.path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// EnableTOTP saves the authenticator the way Vaultwarden's own API does, the
// secret as base32 with the step it was confirmed at, and a recovery code if
// the account has none. It returns the recovery code.
func (v *Vault) EnableTOTP(user, secret string, step int64, ip string) (string, error) {
	db, err := v.writer()
	if err != nil {
		return "", err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO twofactor (uuid, user_uuid, atype, enabled, data, last_used) VALUES (?, ?, 0, 1, ?, ?)
		ON CONFLICT (user_uuid, atype) DO UPDATE SET enabled = 1, data = excluded.data, last_used = excluded.last_used`,
		newUUID(), user, secret, step); err != nil {
		return "", err
	}
	var recover sql.NullString
	if err := tx.QueryRow(`SELECT totp_recover FROM users WHERE uuid = ?`, user).Scan(&recover); err != nil {
		return "", err
	}
	if !recover.Valid || recover.String == "" {
		b := make([]byte, 20)
		_, _ = rand.Read(b)
		recover.String = base32.StdEncoding.EncodeToString(b)
		if _, err := tx.Exec(`UPDATE users SET totp_recover = ? WHERE uuid = ?`, recover.String, user); err != nil {
			return "", err
		}
	}
	if err := logEvent(tx, user, evUpdated2FA, ip); err != nil {
		return "", err
	}
	return recover.String, tx.Commit()
}

// DisableTOTP removes the authenticator. Remembered devices are forgotten too,
// the same as Vaultwarden does, so none of them skips the next code.
func (v *Vault) DisableTOTP(user, ip string) error {
	db, err := v.writer()
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM twofactor WHERE user_uuid = ? AND atype = 0`, user); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE devices SET twofactor_remember = NULL WHERE user_uuid = ?`, user); err != nil {
		return err
	}
	if err := logEvent(tx, user, evDisabled2FA, ip); err != nil {
		return err
	}
	return tx.Commit()
}

// logEvent records a change made here in Vaultwarden's own event log, with no
// device type since no app made it.
func logEvent(tx *sql.Tx, user string, kind int, ip string) error {
	_, err := tx.Exec(`INSERT INTO event (uuid, event_type, user_uuid, act_user_uuid, ip_address, event_date) VALUES (?, ?, ?, ?, ?, ?)`,
		newUUID(), kind, user, user, ip, time.Now().UTC().Format("2006-01-02 15:04:05.000000"))
	return err
}
