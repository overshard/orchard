package main

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Turn is one exchange. Follow-ups need the previous question and answer to be
// rewritten into something a search engine can take.
type Turn struct {
	Question string
	Answer   string
}

// Sessions keeps conversations in memory and never on disk.
//
// This is the one place a question exists in readable form, and it is the
// reason it stays in RAM: the database holds pages and HMACed search keys
// precisely so that restarting the process is enough to forget what was asked.
type Sessions struct {
	mu   sync.Mutex
	data map[string]*conversation
}

type conversation struct {
	turns []Turn
	seen  time.Time
}

const (
	sessionTTL   = 2 * time.Hour
	maxTurnsKept = 4
)

func NewSessions() *Sessions {
	s := &Sessions{data: map[string]*conversation{}}
	go s.expire()
	return s
}

func NewSessionID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Sessions) History(id string) []Turn {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.data[id]
	if !ok {
		return nil
	}
	c.seen = time.Now()
	return append([]Turn(nil), c.turns...)
}

func (s *Sessions) Append(id string, t Turn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.data[id]
	if !ok {
		c = &conversation{}
		s.data[id] = c
	}
	c.turns = append(c.turns, t)
	if len(c.turns) > maxTurnsKept {
		c.turns = c.turns[len(c.turns)-maxTurnsKept:]
	}
	c.seen = time.Now()
}

func (s *Sessions) Reset(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, id)
}

func (s *Sessions) expire() {
	for range time.Tick(10 * time.Minute) {
		s.mu.Lock()
		for id, c := range s.data {
			if time.Since(c.seen) > sessionTTL {
				delete(s.data, id)
			}
		}
		s.mu.Unlock()
	}
}
