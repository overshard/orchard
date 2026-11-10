package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/binary"
	"encoding/hex"
	"strings"
)

// A pond key is six words off this list. At 1,024 words that is 60 bits, and
// the list can never change once a key is out, since a dropped word locks
// somebody out of their frog.
//
//go:embed words.txt
var wordFile string

const keyWords = 6

var (
	wordList = strings.Fields(wordFile)
	wordSet  = func() map[string]bool {
		set := make(map[string]bool, len(wordList))
		for _, w := range wordList {
			set[w] = true
		}
		return set
	}()
)

func newKey() string {
	var buf [2 * keyWords]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic(err)
	}
	words := make([]string, keyWords)
	for i := range words {
		// The list length is a power of two, so the modulo has no bias.
		n := int(binary.BigEndian.Uint16(buf[2*i:]))
		words[i] = wordList[n%len(wordList)]
	}
	return strings.Join(words, "-")
}

// normalizeKey takes the words however they were typed, with spaces, dashes,
// commas, capitals or line breaks, and reports whether they could be a key.
func normalizeKey(typed string) (string, bool) {
	words := strings.FieldsFunc(strings.ToLower(typed), func(r rune) bool {
		return r < 'a' || r > 'z'
	})
	if len(words) != keyWords {
		return "", false
	}
	for _, w := range words {
		if !wordSet[w] {
			return "", false
		}
	}
	return strings.Join(words, "-"), true
}

// The database holds this and never the key, and the pepper is not in the
// database, so a stolen copy of the file opens nobody's pond.
func keyHMAC(pepper []byte, key string) string {
	mac := hmac.New(sha256.New, pepper)
	mac.Write([]byte(key))
	return hex.EncodeToString(mac.Sum(nil))
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buf)
}
