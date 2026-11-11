package main

import (
	"database/sql"
	"time"
)

// keepHome is the setting that keeps a pond's frog off every other pond, as
// a visitor and on a stamp. Off by default, so a frog is out and about.
func (s *store) keepHome(id int64, home bool) (*view, error) {
	return s.act(id, func(tx *sql.Tx, p *pond, now time.Time) error {
		_, err := tx.Exec(`UPDATE ponds SET hidden = ? WHERE id = ?`, home, p.ID)
		return err
	})
}
