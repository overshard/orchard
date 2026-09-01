package main

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// The account the ntfy container was created with by edge/setup-ntfy.sh. It is
// seeded as the username too, and the two must not be assumed equal after that:
// ntfy has no rename, so changing the username here can never change the
// account over there, and a code path that compares the strings instead of
// reading this row breaks delivery at the moment somebody is trying to log in.
const seedUsername = "isaac"

var errNoUser = errors.New("this installation has not been initialized")

type User struct {
	Username    string
	NtfyAccount string
	Created     time.Time
}

// loadUser reads the single row. Absent means `make auth-init` has not run.
func loadUser(db *sql.DB) (User, error) {
	var (
		u       User
		created int64
	)
	err := db.QueryRow(
		`SELECT username, ntfy_account, created FROM users WHERE id = 1`,
	).Scan(&u.Username, &u.NtfyAccount, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, errNoUser
	}
	if err != nil {
		return User{}, err
	}
	u.Created = time.Unix(created, 0).UTC()
	return u, nil
}

// createUser seeds the one row, reporting false if it was already there. The
// caller prints nothing in that case rather than handing back a set of recovery
// codes that were never applied.
func createUser(db *sql.DB, username string) (bool, error) {
	res, err := db.Exec(`
        INSERT OR IGNORE INTO users (id, username, ntfy_account, created)
        VALUES (1, ?, ?, ?)`,
		username, seedUsername, time.Now().Unix())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// usernameOK keeps the field to something that can be typed on a phone and
// cannot be confused with an address or a path.
func usernameOK(name string) error {
	if len(name) < 2 || len(name) > 32 {
		return fmt.Errorf("a username is between 2 and 32 characters")
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return fmt.Errorf("a username holds lowercase letters, digits, dashes and underscores")
		}
	}
	return nil
}

func setUsername(db *sql.DB, name string) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if err := usernameOK(name); err != nil {
		return err
	}
	// ntfy_account is left alone. Renaming there is a delete and recreate that
	// loses the access control entries and every token with them.
	_, err := db.Exec(`UPDATE users SET username = ? WHERE id = 1`, name)
	return err
}
