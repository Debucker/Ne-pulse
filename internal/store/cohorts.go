package store

import (
	"context"
	"database/sql"
	"regexp"
	"strings"
)

// Cohort is a research class code. Devices that join with a valid code have
// their raw motion recorded continuously for that class's study; everyone
// else is only recorded around detected events.
type Cohort struct {
	Code    string  `json:"code"`
	Note    string  `json:"note"`
	Created float64 `json:"created"`
	Devices int     `json:"devices"`
}

var cohortRe = regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]{1,23}$`)

// NormCohort upper-cases and validates a code ("" stays "").
func NormCohort(code string) (string, bool) {
	c := strings.ToUpper(strings.TrimSpace(code))
	if c == "" {
		return "", true
	}
	return c, cohortRe.MatchString(c)
}

// AddCohort creates a class code (idempotent; updates the note).
func (s *Store) AddCohort(ctx context.Context, code, note string, now float64) error {
	return s.Sync(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO cohorts(code, note, created) VALUES(?,?,?) ON CONFLICT(code) DO UPDATE SET note=excluded.note`, code, note, now)
		return err
	})
}

// CohortExists reports whether a code is registered.
func (s *Store) CohortExists(ctx context.Context, code string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cohorts WHERE code=?`, code).Scan(&n)
	return n > 0, err
}

// Cohorts lists class codes with their device counts.
func (s *Store) Cohorts(ctx context.Context) ([]Cohort, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.code, c.note, c.created, (SELECT COUNT(*) FROM devices d WHERE d.cohort=c.code) FROM cohorts c ORDER BY c.code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Cohort
	for rows.Next() {
		var c Cohort
		if err := rows.Scan(&c.Code, &c.Note, &c.Created, &c.Devices); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetDeviceCohort assigns (or with "" clears) a device's class code.
func (s *Store) SetDeviceCohort(ctx context.Context, id, code string) error {
	return s.Sync(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE devices SET cohort=? WHERE id=?`, code, id)
		return err
	})
}

// SetDevicePlacement records where a phone lies (research metadata).
func (s *Store) SetDevicePlacement(ctx context.Context, id, placement string) error {
	return s.Sync(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE devices SET placement=? WHERE id=?`, placement, id)
		return err
	})
}
