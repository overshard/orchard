package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"

	"rsc.io/qr"
)

// newSecret is 20 random bytes in base32, the length Vaultwarden insists on.
func newSecret() string {
	b := make([]byte, 20)
	_, _ = rand.Read(b)
	return base32.StdEncoding.EncodeToString(b)
}

func totpAt(secret string, step int64) (string, bool) {
	key, err := base32.StdEncoding.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil || len(key) == 0 {
		return "", false
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	m := hmac.New(sha1.New, key)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	n := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", n%1_000_000), true
}

// checkTOTP takes a code one step either side of now, the same drift
// Vaultwarden allows, and returns the step it matched.
func checkTOTP(secret, code string, now time.Time) (int64, bool) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != 6 {
		return 0, false
	}
	step := now.Unix() / 30
	for d := int64(-1); d <= 1; d++ {
		if want, ok := totpAt(secret, step+d); ok && hmac.Equal([]byte(want), []byte(code)) {
			return step + d, true
		}
	}
	return 0, false
}

func otpURI(secret, email string) string {
	label := url.PathEscape("pass.bythewood.me:" + email)
	return "otpauth://totp/" + label + "?secret=" + secret + "&issuer=pass.bythewood.me&algorithm=SHA1&digits=6&period=30"
}

// qrPNG is the setup code as a data URI, which the CSP allows for images.
func qrPNG(text string) string {
	c, err := qr.Encode(text, qr.M)
	if err != nil {
		return ""
	}
	c.Scale = 6
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(c.PNG())
}

// spaced splits a secret into fours for typing into an app by hand.
func spaced(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && i%4 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}
