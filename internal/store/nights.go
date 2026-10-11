package store

import (
	"context"
	"database/sql"
)

// Night is how long one phone actually listened (lay still and was
// processed) during one night. A "night" runs from 12:00 one day to 12:00
// the next in Tashkent time and is named by the first day.
type Night struct {
	DeviceID string
	Night    string // YYYY-MM-DD (evening date)
	StillSec float64
	FirstT   float64 // first listening second (unix)
	LastT    float64 // last listening second (unix)
	NoiseMin float64 // smallest quiet noise level seen (m/s^2)
	NoiseMax float64
	RateHz   float64 // last input sample rate
	JitterMS float64 // last timing jitter
	// filled by NightsFor
	Cohort   string
	Platform string
}

const nightsSchema = `CREATE TABLE IF NOT EXISTS nights (
  device_id TEXT NOT NULL,
  night TEXT NOT NULL,
  still_sec REAL NOT NULL DEFAULT 0,
  first_t REAL NOT NULL DEFAULT 0,
  last_t REAL NOT NULL DEFAULT 0,
  noise_min REAL NOT NULL DEFAULT 0,
  noise_max REAL NOT NULL DEFAULT 0,
  rate_hz REAL NOT NULL DEFAULT 0,
  jitter_ms REAL NOT NULL DEFAULT 0,
  PRIMARY KEY (device_id, night)
)`

// AddNight adds listening time to a device-night (async, like session stats).
func (s *Store) AddNight(n Night) {
	s.Async(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO nights(device_id, night, still_sec, first_t, last_t, noise_min, noise_max, rate_hz, jitter_ms)
			VALUES(?,?,?,?,?,?,?,?,?)
			ON CONFLICT(device_id, night) DO UPDATE SET
			  still_sec = still_sec + excluded.still_sec,
			  first_t = CASE WHEN first_t = 0 OR excluded.first_t < first_t THEN excluded.first_t ELSE first_t END,
			  last_t = MAX(last_t, excluded.last_t),
			  noise_min = CASE WHEN noise_min = 0 OR (excluded.noise_min > 0 AND excluded.noise_min < noise_min) THEN excluded.noise_min ELSE noise_min END,
			  noise_max = MAX(noise_max, excluded.noise_max),
			  rate_hz = excluded.rate_hz, jitter_ms = excluded.jitter_ms`,
			n.DeviceID, n.Night, n.StillSec, n.FirstT, n.LastT, n.NoiseMin, n.NoiseMax, n.RateHz, n.JitterMS)
		return err
	})
}

const nightCols = `n.device_id, n.night, n.still_sec, n.first_t, n.last_t, n.noise_min, n.noise_max, n.rate_hz, n.jitter_ms, COALESCE(d.cohort,''), COALESCE(d.platform,'')`

func scanNights(rows *sql.Rows) ([]Night, error) {
	defer rows.Close()
	var out []Night
	for rows.Next() {
		var n Night
		if err := rows.Scan(&n.DeviceID, &n.Night, &n.StillSec, &n.FirstT, &n.LastT, &n.NoiseMin, &n.NoiseMax, &n.RateHz, &n.JitterMS, &n.Cohort, &n.Platform); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// DeviceNights lists one device's nights, newest first.
func (s *Store) DeviceNights(ctx context.Context, deviceID string) ([]Night, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+nightCols+` FROM nights n LEFT JOIN devices d ON d.id = n.device_id WHERE n.device_id=? ORDER BY n.night DESC LIMIT 60`, deviceID)
	if err != nil {
		return nil, err
	}
	return scanNights(rows)
}

// NightsFor lists device-nights for a class (all classes if cohort is ""),
// optionally for one night, newest first.
func (s *Store) NightsFor(ctx context.Context, cohort, night string) ([]Night, error) {
	q := `SELECT ` + nightCols + ` FROM nights n LEFT JOIN devices d ON d.id = n.device_id WHERE (?='' OR d.cohort=?) AND (?='' OR n.night=?) ORDER BY n.night DESC, n.still_sec DESC LIMIT 20000`
	rows, err := s.db.QueryContext(ctx, q, cohort, cohort, night, night)
	if err != nil {
		return nil, err
	}
	return scanNights(rows)
}

// CountCohortDevices counts devices that joined with a class code.
func (s *Store) CountCohortDevices(ctx context.Context, cohort string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices WHERE cohort=?`, cohort).Scan(&n)
	return n, err
}
