package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	deviceCookie = "bw_device"
	// A browser keeps a cookie for at most 400 days whatever it's told.
	deviceTTL = 365 * 24 * time.Hour
)

// deviceKey signs the known device cookie. It sits in the database, and a copy
// of it only lets a browser skip the lockout, never sign in.
func deviceKey(db *sql.DB) ([]byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO device_key (id, key) VALUES (1, ?)`, raw); err != nil {
		return nil, err
	}
	var key []byte
	err := db.QueryRow(`SELECT key FROM device_key WHERE id = 1`).Scan(&key)
	return key, err
}

func deviceMAC(key []byte, issued string) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte("device:" + issued))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// knownDevice reports whether this browser has signed in here before. A
// stranger's wrong codes then can't lock it out of code sign in.
func knownDevice(db *sql.DB, r *http.Request) bool {
	c, err := r.Cookie(deviceCookie)
	if err != nil {
		return false
	}
	issued, mac, ok := strings.Cut(c.Value, ".")
	if !ok {
		return false
	}
	at, err := strconv.ParseInt(issued, 10, 64)
	if err != nil || time.Since(time.Unix(at, 0)) > deviceTTL {
		return false
	}
	key, err := deviceKey(db)
	if err != nil {
		return false
	}
	return hmac.Equal([]byte(mac), []byte(deviceMAC(key, issued)))
}

func writeDeviceCookie(db *sql.DB, w http.ResponseWriter) error {
	key, err := deviceKey(db)
	if err != nil {
		return err
	}
	issued := strconv.FormatInt(time.Now().Unix(), 10)
	http.SetCookie(w, &http.Cookie{
		Name:  deviceCookie,
		Value: issued + "." + deviceMAC(key, issued),
		Path:  "/",
		// Host-only like the pending cookie, since only auth reads it.
		SameSite: http.SameSiteStrictMode,
		HttpOnly: true,
		Secure:   true,
		MaxAge:   int(deviceTTL / time.Second),
	})
	return nil
}
