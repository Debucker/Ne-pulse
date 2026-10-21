package store

import (
	"context"
	"database/sql"
)

// SurveyResponse is one "test your phone" result: a 30-second still
// recording reduced to sensor quality numbers, plus tap-only answers.
type SurveyResponse struct {
	ID         string
	Created    float64
	Platform   string
	Model      string
	Lang       string
	Cohort     string
	RateHz     float64
	NoiseRMS   float64 // median 1-s band-passed RMS while still, m/s^2 (0 = not measured)
	Resolution float64 // smallest step of the sensor, m/s^2
	JitterMS   float64
	StillSec   float64 // seconds that counted as still
	DetMMI     float64 // weakest shaking this phone would trigger on
	Answers    string  // JSON object of the tap answers
	File       string  // raw recording, relative to the record dir
	Tester     string  // random browser id: the same person testing twice
	Meta       string  // JSON: how the test went
}

const surveySchema = `CREATE TABLE IF NOT EXISTS surveys (
  id TEXT PRIMARY KEY,
  created REAL NOT NULL,
  platform TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL DEFAULT '',
  lang TEXT NOT NULL DEFAULT '',
  cohort TEXT NOT NULL DEFAULT '',
  rate_hz REAL NOT NULL DEFAULT 0,
  noise_rms REAL NOT NULL DEFAULT 0,
  resolution REAL NOT NULL DEFAULT 0,
  jitter_ms REAL NOT NULL DEFAULT 0,
  still_sec REAL NOT NULL DEFAULT 0,
  det_mmi REAL NOT NULL DEFAULT 0,
  answers TEXT NOT NULL DEFAULT '{}',
  file TEXT NOT NULL DEFAULT ''
)`

// AddSurvey stores one response.
func (s *Store) AddSurvey(ctx context.Context, r SurveyResponse) error {
	return s.Sync(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO surveys(id, created, platform, model, lang, cohort, rate_hz, noise_rms, resolution, jitter_ms, still_sec, det_mmi, answers, file, tester, meta)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			r.ID, r.Created, r.Platform, r.Model, r.Lang, r.Cohort, r.RateHz, r.NoiseRMS, r.Resolution, r.JitterMS, r.StillSec, r.DetMMI, r.Answers, r.File, r.Tester, r.Meta)
		return err
	})
}

// Surveys lists every response, newest first (automated test submissions,
// class CITEST, are left out).
func (s *Store) Surveys(ctx context.Context) ([]SurveyResponse, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, created, platform, model, lang, cohort, rate_hz, noise_rms, resolution, jitter_ms, still_sec, det_mmi, answers, file, tester, meta
		FROM surveys WHERE cohort <> 'CITEST' ORDER BY created DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SurveyResponse
	for rows.Next() {
		var r SurveyResponse
		if err := rows.Scan(&r.ID, &r.Created, &r.Platform, &r.Model, &r.Lang, &r.Cohort, &r.RateHz, &r.NoiseRMS, &r.Resolution,
			&r.JitterMS, &r.StillSec, &r.DetMMI, &r.Answers, &r.File, &r.Tester, &r.Meta); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Volunteer is someone from the phone test who wants to join a night of
// recording. Kept apart from the survey answers so those stay anonymous.
type Volunteer struct {
	Handle   string  `json:"handle"` // Telegram username, without @
	Created  float64 `json:"created"`
	Platform string  `json:"platform"`
	Age      string  `json:"age"`
	Lang     string  `json:"lang"`
}

const volunteerSchema = `CREATE TABLE IF NOT EXISTS volunteers (
  handle TEXT PRIMARY KEY,
  created REAL NOT NULL,
  platform TEXT NOT NULL DEFAULT '',
  age TEXT NOT NULL DEFAULT '',
  lang TEXT NOT NULL DEFAULT ''
)`

// AddVolunteer stores (or refreshes) a volunteer.
func (s *Store) AddVolunteer(ctx context.Context, v Volunteer) error {
	return s.Sync(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO volunteers(handle, created, platform, age, lang) VALUES(?,?,?,?,?)
			ON CONFLICT(handle) DO UPDATE SET created=excluded.created, platform=excluded.platform, age=excluded.age, lang=excluded.lang`,
			v.Handle, v.Created, v.Platform, v.Age, v.Lang)
		return err
	})
}

// Volunteers lists them, oldest first.
func (s *Store) Volunteers(ctx context.Context) ([]Volunteer, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT handle, created, platform, age, lang FROM volunteers ORDER BY created`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Volunteer{}
	for rows.Next() {
		var v Volunteer
		if err := rows.Scan(&v.Handle, &v.Created, &v.Platform, &v.Age, &v.Lang); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
