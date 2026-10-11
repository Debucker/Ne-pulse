package store

import (
	"context"
	"database/sql"
	"errors"
)

// Research credit: a participant may put their name on the public list of
// contributors. The name is stored apart from the recordings (which only
// carry a pseudonym) and is removed with the device. Invites count who
// brought whom, so people can be credited for growing the network.
const creditsSchema = `
CREATE TABLE IF NOT EXISTS credits (
  device_id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  hidden INTEGER NOT NULL DEFAULT 0,
  created REAL NOT NULL
);
CREATE TABLE IF NOT EXISTS refs (
  code TEXT PRIMARY KEY,
  device_id TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS invites (
  device_id TEXT PRIMARY KEY,
  inviter TEXT NOT NULL,
  created REAL NOT NULL
);
CREATE INDEX IF NOT EXISTS invites_inviter ON invites(inviter);`

// Contributor is one named participant with their total contribution.
type Contributor struct {
	DeviceID   string
	Name       string
	Hidden     bool
	Created    float64
	StillSec   float64 // total listening time over all nights
	FullNights int     // nights with at least the full-night minimum
	Invited    int     // invited phones that listened at least an hour
	Cohort     string
}

// SetCredit sets (or with "" removes) the name a device is credited under.
func (s *Store) SetCredit(ctx context.Context, id, name string, now float64) error {
	return s.Sync(ctx, func(tx *sql.Tx) error {
		if name == "" {
			_, err := tx.Exec(`DELETE FROM credits WHERE device_id=?`, id)
			return err
		}
		_, err := tx.Exec(`INSERT INTO credits(device_id, name, created) VALUES(?,?,?)
			ON CONFLICT(device_id) DO UPDATE SET name=excluded.name`, id, name, now)
		return err
	})
}

// Credit returns a device's credited name ("" if none) and whether the
// organiser hid it.
func (s *Store) Credit(ctx context.Context, id string) (name string, hidden bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT name, hidden FROM credits WHERE device_id=?`, id).Scan(&name, &hidden)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return
}

// SetCreditHidden lets the organiser hide an inappropriate name.
func (s *Store) SetCreditHidden(ctx context.Context, id string, hidden bool) error {
	return s.Sync(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE credits SET hidden=? WHERE device_id=?`, hidden, id)
		return err
	})
}

// PutRef registers a device's invite code (idempotent).
func (s *Store) PutRef(ctx context.Context, code, id string) error {
	return s.Sync(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT OR IGNORE INTO refs(code, device_id) VALUES(?,?)`, code, id)
		return err
	})
}

// RefOwner resolves an invite code to the inviting device ("" if unknown).
func (s *Store) RefOwner(ctx context.Context, code string) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT device_id FROM refs WHERE code=?`, code).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

// AddInvite records that device was invited by inviter (first invite wins).
func (s *Store) AddInvite(ctx context.Context, device, inviter string, now float64) error {
	return s.Sync(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT OR IGNORE INTO invites(device_id, inviter, created) VALUES(?,?,?)`, device, inviter, now)
		return err
	})
}

// Contributors lists every named participant with their totals.
func (s *Store) Contributors(ctx context.Context, fullNightSec float64) ([]Contributor, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.device_id, c.name, c.hidden, c.created,
		COALESCE((SELECT SUM(still_sec) FROM nights n WHERE n.device_id=c.device_id),0),
		(SELECT COUNT(*) FROM nights n WHERE n.device_id=c.device_id AND n.still_sec>=?),
		(SELECT COUNT(*) FROM invites i WHERE i.inviter=c.device_id
			AND (SELECT COALESCE(SUM(still_sec),0) FROM nights n2 WHERE n2.device_id=i.device_id) >= 3600),
		COALESCE((SELECT cohort FROM devices d WHERE d.id=c.device_id),'')
		FROM credits c`, fullNightSec)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Contributor
	for rows.Next() {
		var c Contributor
		if err := rows.Scan(&c.DeviceID, &c.Name, &c.Hidden, &c.Created, &c.StillSec, &c.FullNights, &c.Invited, &c.Cohort); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// InvitedCount is how many invited phones listened at least an hour, and how
// many joined at all.
func (s *Store) InvitedCount(ctx context.Context, inviter string) (active, joined int, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM((SELECT COALESCE(SUM(still_sec),0) FROM nights n WHERE n.device_id=i.device_id) >= 3600),0)
		FROM invites i WHERE i.inviter=?`, inviter).Scan(&joined, &active)
	return
}

// ListeningTotals: research-class phones that ever listened, and their total
// listening seconds (phones outside a class, such as load tests, don't count).
func (s *Store) ListeningTotals(ctx context.Context) (phones int, stillSec float64, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT n.device_id), COALESCE(SUM(n.still_sec),0)
		FROM nights n JOIN devices d ON d.id=n.device_id WHERE d.cohort<>''`).Scan(&phones, &stillSec)
	return
}
