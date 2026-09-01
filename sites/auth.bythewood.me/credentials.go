package main

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"errors"
	"math/big"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters, the same shape repos uses for its push tokens. What is
// hashed here is either a 30 bit login code with a ten minute life and five
// attempts, or a 60 bit recovery code, so this exists to keep a leaked database
// from being a credential rather than to survive an offline dictionary attack.
const (
	argonTime    = 1
	argonMemory  = 64 * 1024
	argonThreads = 4
	argonKeyLen  = 32
	saltLen      = 16
)

func hashSecret(secret string) (hash, salt []byte, err error) {
	salt = make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, nil, err
	}
	return argon2.IDKey([]byte(secret), salt, argonTime, argonMemory, argonThreads, argonKeyLen), salt, nil
}

func secretMatches(secret string, hash, salt []byte) bool {
	candidate := argon2.IDKey([]byte(secret), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return subtle.ConstantTimeCompare(candidate, hash) == 1
}

// newCode returns six digits, zero padded, from crypto/rand. Not math/rand: the
// whole value of this is that it cannot be predicted from the last one.
func newCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	s := n.String()
	return strings.Repeat("0", 6-len(s)) + s, nil
}

// recoveryAlphabet drops the characters that are read wrong off a screen and
// typed wrong from paper: i, l, o, u, 0 and 1.
const recoveryAlphabet = "abcdefghjkmnpqrstvwxyz23456789"

const (
	recoveryCount  = 10
	recoveryGroups = 3
	recoveryPerRun = 4
	// The first four characters, stored in clear, which finds the row without
	// hashing all ten. Four of a 30 character alphabet is not enough to guess.
	recoveryPrefix = 4
)

// newRecoveryCode returns something like "k7pq-hm3n-wxbf".
func newRecoveryCode() (string, error) {
	var b strings.Builder
	for g := 0; g < recoveryGroups; g++ {
		if g > 0 {
			b.WriteByte('-')
		}
		for i := 0; i < recoveryPerRun; i++ {
			n, err := rand.Int(rand.Reader, big.NewInt(int64(len(recoveryAlphabet))))
			if err != nil {
				return "", err
			}
			b.WriteByte(recoveryAlphabet[n.Int64()])
		}
	}
	return b.String(), nil
}

// regenerateRecoveryCodes replaces the whole set in one transaction and returns
// the new codes, which is the only time they exist anywhere readable. An old
// code stops working the moment this returns, so a half-applied set would leave
// the account with no break-glass at all.
func regenerateRecoveryCodes(db *sql.DB) ([]string, error) {
	codes := make([]string, 0, recoveryCount)
	for i := 0; i < recoveryCount; i++ {
		code, err := newRecoveryCode()
		if err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM recovery_codes`); err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	for _, code := range codes {
		hash, salt, err := hashSecret(code)
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(`
            INSERT INTO recovery_codes (prefix, hash, salt, created)
            VALUES (?, ?, ?, ?)`,
			code[:recoveryPrefix], hash, salt, now); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return codes, nil
}

var errBadRecovery = errors.New("that recovery code is not valid")

// useRecoveryCode spends one code, reporting how many are left. Marking it used
// is part of the same statement that finds it, so the same code arriving twice
// at once cannot be spent twice.
func useRecoveryCode(db *sql.DB, code string) (remaining int, err error) {
	code = strings.ToLower(strings.TrimSpace(code))
	if len(code) < recoveryPrefix {
		return 0, errBadRecovery
	}

	rows, err := db.Query(`
        SELECT id, hash, salt FROM recovery_codes
        WHERE prefix = ? AND used_at = 0`, code[:recoveryPrefix])
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var matched int64 = -1
	for rows.Next() {
		var (
			id         int64
			hash, salt []byte
		)
		if err := rows.Scan(&id, &hash, &salt); err != nil {
			return 0, err
		}
		if secretMatches(code, hash, salt) {
			matched = id
			break
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	rows.Close()

	if matched < 0 {
		return 0, errBadRecovery
	}

	res, err := db.Exec(`UPDATE recovery_codes SET used_at = ? WHERE id = ? AND used_at = 0`,
		time.Now().Unix(), matched)
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, errBadRecovery
	}

	return countRecoveryCodes(db)
}

func countRecoveryCodes(db *sql.DB) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM recovery_codes WHERE used_at = 0`).Scan(&n)
	return n, err
}
