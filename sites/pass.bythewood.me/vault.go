package main

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Vault reads Vaultwarden's database and never writes it. Names, notes and
// every secret in it are encrypted by the apps, so what's readable is counts,
// times, device names and the addresses in its event log.
type Vault struct{ db *sql.DB }

// OpenVault opens read only. The volume is mounted writable anyway, since
// SQLite needs the -shm file to read a database in WAL mode.
func OpenVault(path string) (*Vault, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=busy_timeout(5000)&_pragma=query_only(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(2)
	return &Vault{db: db}, nil
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
		return "unknown"
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
