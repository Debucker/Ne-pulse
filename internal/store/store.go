// Package store persists NE-PULSE state in SQLite (pure-Go driver, WAL
// mode). The database file is also the research dataset: devices
// (pseudonymous), picks with full feature vectors, events with their
// decision logs, waveform snippets, catalog matches and minute statistics.
//
// Writes from the real-time pipeline go through an asynchronous batching
// writer so a slow disk can never stall detection.
package store

import (
	"strings"
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS devices (
  id TEXT PRIMARY KEY,
  secret_hash BLOB NOT NULL,
  created_at REAL NOT NULL,
  last_seen REAL NOT NULL DEFAULT 0,
  platform TEXT NOT NULL DEFAULT 'other',
  loc_grid REAL NOT NULL DEFAULT 0,
  loc_row INTEGER NOT NULL DEFAULT 0,
  loc_col INTEGER NOT NULL DEFAULT 0,
  samples INTEGER NOT NULL DEFAULT 0,
  picks INTEGER NOT NULL DEFAULT 0,
  picks_accepted INTEGER NOT NULL DEFAULT 0,
  connected_sec REAL NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS picks (
  id INTEGER PRIMARY KEY,
  device_id TEXT NOT NULL,
  unit TEXT NOT NULL,
  onset REAL NOT NULL,
  sigma REAL NOT NULL,
  class TEXT NOT NULL,
  accepted INTEGER NOT NULL,
  features TEXT NOT NULL,
  arrived_at REAL NOT NULL
);
CREATE INDEX IF NOT EXISTS picks_onset ON picks(onset);
CREATE INDEX IF NOT EXISTS picks_device ON picks(device_id);
CREATE TABLE IF NOT EXISTS events (
  id TEXT PRIMARY KEY,
  level TEXT NOT NULL,
  active INTEGER NOT NULL,
  outcome TEXT NOT NULL DEFAULT '',
  origin REAL NOT NULL,
  lat REAL NOT NULL,
  lng REAL NOT NULL,
  created_at REAL NOT NULL,
  updated_at REAL NOT NULL,
  retracted INTEGER NOT NULL DEFAULT 0,
  retract_reason TEXT NOT NULL DEFAULT '',
  external TEXT NOT NULL DEFAULT '',
  doc TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS events_origin ON events(origin);
CREATE TABLE IF NOT EXISTS snippets (
  id INTEGER PRIMARY KEY,
  event_id TEXT NOT NULL,
  device_id TEXT NOT NULL,
  unit TEXT NOT NULL,
  lat REAL NOT NULL,
  lng REAL NOT NULL,
  t0 REAL NOT NULL,
  fs REAL NOT NULL,
  n INTEGER NOT NULL,
  data BLOB NOT NULL,
  created_at REAL NOT NULL
);
CREATE INDEX IF NOT EXISTS snippets_event ON snippets(event_id);
CREATE TABLE IF NOT EXISTS units (
  key INTEGER PRIMARY KEY,
  votes REAL NOT NULL,
  exposure REAL NOT NULL,
  updated_at REAL NOT NULL
);
CREATE TABLE IF NOT EXISTS catalog (
  source TEXT NOT NULL,
  id TEXT NOT NULL,
  time REAL NOT NULL,
  lat REAL NOT NULL,
  lng REAL NOT NULL,
  depth REAL NOT NULL,
  mag REAL NOT NULL,
  mag_type TEXT NOT NULL,
  place TEXT NOT NULL,
  url TEXT NOT NULL,
  fetched_at REAL NOT NULL,
  PRIMARY KEY (source, id)
);
CREATE INDEX IF NOT EXISTS catalog_time ON catalog(time);
CREATE TABLE IF NOT EXISTS stats_minute (
  t INTEGER PRIMARY KEY,
  doc TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS alerts (
  id INTEGER PRIMARY KEY,
  event_id TEXT NOT NULL,
  level TEXT NOT NULL,
  channel TEXT NOT NULL,
  at REAL NOT NULL,
  ok INTEGER NOT NULL,
  detail TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS alerts_once ON alerts(event_id, level, channel);
CREATE TABLE IF NOT EXISTS audit (
  id INTEGER PRIMARY KEY,
  at REAL NOT NULL,
  actor TEXT NOT NULL,
  action TEXT NOT NULL,
  detail TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS cohorts (
  code TEXT PRIMARY KEY,
  note TEXT NOT NULL DEFAULT '',
  created REAL NOT NULL
);
CREATE TABLE IF NOT EXISTS subscribers (
  chat_id INTEGER PRIMARY KEY,
  area_row INTEGER NOT NULL,
  area_col INTEGER NOT NULL,
  lang TEXT NOT NULL,
  created REAL NOT NULL,
  updated REAL NOT NULL
);
`

// Store wraps the database.
type Store struct {
	db      *sql.DB
	writes  chan func(*sql.Tx) error
	done    chan struct{}
	dropped atomic.Int64
	failed  atomic.Int64
	written atomic.Int64
}

// Open opens (creating if needed) the database at path. ":memory:" is
// supported for tests.
func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)"
	if path == ":memory:" {
		dsn = "file::memory:?cache=shared&_pragma=busy_timeout(5000)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	if path == ":memory:" {
		db.SetMaxOpenConns(1)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	if _, err := db.Exec(nightsSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	if _, err := db.Exec(creditsSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	if _, err := db.Exec(surveySchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	// Columns added after the first release. SQLite has no ADD COLUMN IF NOT
	// EXISTS, so a "duplicate column" error means the migration already ran.
	for _, m := range []string{
		`ALTER TABLE devices ADD COLUMN cohort TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE devices ADD COLUMN placement TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE subscribers ADD COLUMN device_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE subscribers ADD COLUMN nudged REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE subscribers ADD COLUMN morning TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE surveys ADD COLUMN tester TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE surveys ADD COLUMN meta TEXT NOT NULL DEFAULT '{}'`,
	} {
		if _, err := db.Exec(m); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			db.Close()
			return nil, fmt.Errorf("store: migrate: %w", err)
		}
	}
	s := &Store{db: db, writes: make(chan func(*sql.Tx) error, 8192), done: make(chan struct{})}
	go s.writer()
	return s, nil
}

// Close flushes pending writes and closes the database.
func (s *Store) Close() error {
	close(s.writes)
	<-s.done
	return s.db.Close()
}

// DB exposes the handle for read-only research queries.
func (s *Store) DB() *sql.DB { return s.db }

// WriterStats reports async writer health.
func (s *Store) WriterStats() (written, failed, dropped int64, queued int) {
	return s.written.Load(), s.failed.Load(), s.dropped.Load(), len(s.writes)
}

func (s *Store) writer() {
	defer close(s.done)
	batch := make([]func(*sql.Tx) error, 0, 256)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		tx, err := s.db.Begin()
		if err != nil {
			s.failed.Add(int64(len(batch)))
			log.Printf("store: begin: %v", err)
			batch = batch[:0]
			return
		}
		for _, w := range batch {
			if err := w(tx); err != nil {
				s.failed.Add(1)
				log.Printf("store: write: %v", err)
			} else {
				s.written.Add(1)
			}
		}
		if err := tx.Commit(); err != nil {
			log.Printf("store: commit: %v", err)
		}
		batch = batch[:0]
	}
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case w, ok := <-s.writes:
			if !ok {
				flush()
				return
			}
			batch = append(batch, w)
			if len(batch) >= 256 {
				flush()
			}
		case <-tick.C:
			flush()
		}
	}
}

// Async enqueues a write; it never blocks (the write is dropped and counted
// if the queue is full).
func (s *Store) Async(w func(*sql.Tx) error) {
	select {
	case s.writes <- w:
	default:
		s.dropped.Add(1)
	}
}

// Sync runs a write and waits for it.
func (s *Store) Sync(ctx context.Context, w func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := w(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// ---- Devices -----------------------------------------------------------------

// Device is a stored device record.
type Device struct {
	ID            string
	SecretHash    []byte
	CreatedAt     float64
	LastSeen      float64
	Platform      string
	GridKM        float64
	Row, Col      int32
	Samples       int64
	Picks         int64
	PicksAccepted int64
	ConnectedSec  float64
	Cohort        string // research class code; "" = not recorded
	Placement     string // research: where the phone lies, e.g. "floor=3;surface=table;building=apartment"
}

// ErrNotFound is returned for a missing row.
var ErrNotFound = errors.New("not found")

// CreateDevice inserts a new device.
func (s *Store) CreateDevice(ctx context.Context, id string, hash []byte, platform string, now float64) error {
	return s.Sync(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO devices(id, secret_hash, created_at, last_seen, platform) VALUES(?,?,?,?,?)`, id, hash, now, now, platform)
		return err
	})
}

// GetDevice loads a device.
func (s *Store) GetDevice(ctx context.Context, id string) (Device, error) {
	var d Device
	err := s.db.QueryRowContext(ctx, `SELECT id, secret_hash, created_at, last_seen, platform, loc_grid, loc_row, loc_col, samples, picks, picks_accepted, connected_sec, cohort, placement FROM devices WHERE id=?`, id).
		Scan(&d.ID, &d.SecretHash, &d.CreatedAt, &d.LastSeen, &d.Platform, &d.GridKM, &d.Row, &d.Col, &d.Samples, &d.Picks, &d.PicksAccepted, &d.ConnectedSec, &d.Cohort, &d.Placement)
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	return d, err
}

// UpdateDeviceSession records a session's contribution (async).
func (s *Store) UpdateDeviceSession(id string, lastSeen float64, gridKM float64, row, col int32, samples, picks, accepted int64, connected float64) {
	s.Async(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE devices SET last_seen=?, loc_grid=?, loc_row=?, loc_col=?, samples=samples+?, picks=picks+?, picks_accepted=picks_accepted+?, connected_sec=connected_sec+? WHERE id=?`,
			lastSeen, gridKM, row, col, samples, picks, accepted, connected, id)
		return err
	})
}

// DeleteDevice removes a device and everything linked to it.
func (s *Store) DeleteDevice(ctx context.Context, id string) error {
	return s.Sync(ctx, func(tx *sql.Tx) error {
		for _, q := range []string{`DELETE FROM picks WHERE device_id=?`, `DELETE FROM snippets WHERE device_id=?`, `DELETE FROM nights WHERE device_id=?`, `DELETE FROM credits WHERE device_id=?`, `DELETE FROM refs WHERE device_id=?`, `DELETE FROM invites WHERE device_id=?`, `DELETE FROM devices WHERE id=?`} {
			if _, err := tx.Exec(q, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// CountDevices returns total and recently-seen device counts.
func (s *Store) CountDevices(ctx context.Context, since float64) (total, recent int, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(last_seen >= ?),0) FROM devices`, since).Scan(&total, &recent)
	return
}

// ---- Picks ---------------------------------------------------------------------

// PickRow is one stored pick.
type PickRow struct {
	DeviceID  string          `json:"deviceId"`
	Unit      string          `json:"unit"`
	Onset     float64         `json:"onset"`
	Sigma     float64         `json:"sigma"`
	Class     string          `json:"class"`
	Accepted  bool            `json:"accepted"`
	Features  json.RawMessage `json:"features"`
	ArrivedAt float64         `json:"arrivedAt"`
}

// InsertPick stores a pick asynchronously.
func (s *Store) InsertPick(p PickRow) {
	s.Async(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO picks(device_id, unit, onset, sigma, class, accepted, features, arrived_at) VALUES(?,?,?,?,?,?,?,?)`,
			p.DeviceID, p.Unit, p.Onset, p.Sigma, p.Class, p.Accepted, string(p.Features), p.ArrivedAt)
		return err
	})
}

// RecentPicks lists the newest picks (admin).
func (s *Store) RecentPicks(ctx context.Context, limit int) ([]PickRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT device_id, unit, onset, sigma, class, accepted, features, arrived_at FROM picks ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PickRow
	for rows.Next() {
		var p PickRow
		var f string
		if err := rows.Scan(&p.DeviceID, &p.Unit, &p.Onset, &p.Sigma, &p.Class, &p.Accepted, &f, &p.ArrivedAt); err != nil {
			return nil, err
		}
		p.Features = json.RawMessage(f)
		out = append(out, p)
	}
	return out, rows.Err()
}

// PickClassCounts counts picks per class since t.
func (s *Store) PickClassCounts(ctx context.Context, since float64) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT class, COUNT(*) FROM picks WHERE arrived_at >= ? GROUP BY class`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var c string
		var n int
		if err := rows.Scan(&c, &n); err != nil {
			return nil, err
		}
		out[c] = n
	}
	return out, rows.Err()
}

// ---- Events --------------------------------------------------------------------

// EventRow is the stored form of an event.
type EventRow struct {
	ID            string          `json:"id"`
	Level         string          `json:"level"`
	Active        bool            `json:"active"`
	Outcome       string          `json:"outcome"`
	Origin        float64         `json:"origin"`
	Lat           float64         `json:"lat"`
	Lng           float64         `json:"lng"`
	CreatedAt     float64         `json:"createdAt"`
	UpdatedAt     float64         `json:"updatedAt"`
	Retracted     bool            `json:"retracted"`
	RetractReason string          `json:"retractReason"`
	External      json.RawMessage `json:"external,omitempty"`
	Doc           json.RawMessage `json:"doc"`
}

// UpsertEvent stores (or replaces) an event document asynchronously.
func (s *Store) UpsertEvent(id, level string, active bool, outcome string, origin, lat, lng, created, updated float64, doc []byte) {
	s.Async(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO events(id, level, active, outcome, origin, lat, lng, created_at, updated_at, doc) VALUES(?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET level=excluded.level, active=excluded.active, outcome=excluded.outcome, origin=excluded.origin,
			lat=excluded.lat, lng=excluded.lng, updated_at=excluded.updated_at, doc=excluded.doc`,
			id, level, active, outcome, origin, lat, lng, created, updated, string(doc))
		return err
	})
}

const eventCols = `id, level, active, outcome, origin, lat, lng, created_at, updated_at, retracted, retract_reason, external, doc`

func scanEvent(sc interface{ Scan(...any) error }) (EventRow, error) {
	var e EventRow
	var ext, doc string
	err := sc.Scan(&e.ID, &e.Level, &e.Active, &e.Outcome, &e.Origin, &e.Lat, &e.Lng, &e.CreatedAt, &e.UpdatedAt, &e.Retracted, &e.RetractReason, &ext, &doc)
	if ext != "" {
		e.External = json.RawMessage(ext)
	}
	e.Doc = json.RawMessage(doc)
	return e, err
}

// GetEvent loads one event.
func (s *Store) GetEvent(ctx context.Context, id string) (EventRow, error) {
	e, err := scanEvent(s.db.QueryRowContext(ctx, `SELECT `+eventCols+` FROM events WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	return e, err
}

// ListEvents returns events newest first. minLevel filters out lower levels
// ("" = all, including dismissed candidates).
func (s *Store) ListEvents(ctx context.Context, limit int, includeDismissed bool, since float64) ([]EventRow, error) {
	q := `SELECT ` + eventCols + ` FROM events WHERE origin >= ?`
	if !includeDismissed {
		q += ` AND outcome != 'dismissed'`
	}
	q += ` ORDER BY origin DESC LIMIT ?`
	rows, err := s.db.QueryContext(ctx, q, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EventRow
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// RetractEvent marks an event as retracted by an operator.
func (s *Store) RetractEvent(ctx context.Context, id, reason, actor string, now float64) error {
	return s.Sync(ctx, func(tx *sql.Tx) error {
		r, err := tx.Exec(`UPDATE events SET retracted=1, retract_reason=?, updated_at=? WHERE id=?`, reason, now, id)
		if err != nil {
			return err
		}
		if n, _ := r.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		_, err = tx.Exec(`INSERT INTO audit(at, actor, action, detail) VALUES(?,?,?,?)`, now, actor, "retract", id+": "+reason)
		return err
	})
}

// SetExternal records an external catalog match for an event.
func (s *Store) SetExternal(ctx context.Context, id string, match []byte) error {
	return s.Sync(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE events SET external=? WHERE id=?`, string(match), id)
		return err
	})
}

// ---- Snippets ------------------------------------------------------------------

// Snippet is a stored waveform window from one device.
type Snippet struct {
	EventID  string       `json:"eventId"`
	DeviceID string       `json:"-"`
	Unit     string       `json:"unit"`
	Lat      float64      `json:"lat"`
	Lng      float64      `json:"lng"`
	T0       float64      `json:"t0"`
	Fs       float64      `json:"fs"`
	XYZ      [][3]float32 `json:"-"`
}

func encodeXYZ(xyz [][3]float32) []byte {
	var raw bytes.Buffer
	for _, v := range xyz {
		for _, c := range v {
			_ = binary.Write(&raw, binary.LittleEndian, math.Float32bits(c))
		}
	}
	var out bytes.Buffer
	zw := gzip.NewWriter(&out)
	zw.Write(raw.Bytes())
	zw.Close()
	return out.Bytes()
}

func decodeXYZ(b []byte, n int) ([][3]float32, error) {
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		return nil, err
	}
	if len(raw) != n*12 {
		return nil, fmt.Errorf("snippet length %d != %d", len(raw), n*12)
	}
	out := make([][3]float32, n)
	for i := 0; i < n; i++ {
		for c := 0; c < 3; c++ {
			out[i][c] = math.Float32frombits(binary.LittleEndian.Uint32(raw[(i*3+c)*4:]))
		}
	}
	return out, nil
}

// InsertSnippet stores a waveform window asynchronously.
func (s *Store) InsertSnippet(sn Snippet, now float64) {
	data := encodeXYZ(sn.XYZ)
	s.Async(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO snippets(event_id, device_id, unit, lat, lng, t0, fs, n, data, created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			sn.EventID, sn.DeviceID, sn.Unit, sn.Lat, sn.Lng, sn.T0, sn.Fs, len(sn.XYZ), data, now)
		return err
	})
}

// Snippets loads all waveform windows for an event.
func (s *Store) Snippets(ctx context.Context, eventID string) ([]Snippet, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT event_id, device_id, unit, lat, lng, t0, fs, n, data FROM snippets WHERE event_id=? ORDER BY id`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Snippet
	for rows.Next() {
		var sn Snippet
		var n int
		var data []byte
		if err := rows.Scan(&sn.EventID, &sn.DeviceID, &sn.Unit, &sn.Lat, &sn.Lng, &sn.T0, &sn.Fs, &n, &data); err != nil {
			return nil, err
		}
		if sn.XYZ, err = decodeXYZ(data, n); err != nil {
			return nil, err
		}
		out = append(out, sn)
	}
	return out, rows.Err()
}

// ---- Units ---------------------------------------------------------------------

// UnitRow is a unit's persisted background history.
type UnitRow struct {
	Key      uint64
	Votes    float64
	Exposure float64
}

// SaveUnits replaces unit histories asynchronously.
func (s *Store) SaveUnits(units []UnitRow, now float64) {
	s.Async(func(tx *sql.Tx) error {
		for _, u := range units {
			if _, err := tx.Exec(`INSERT INTO units(key, votes, exposure, updated_at) VALUES(?,?,?,?) ON CONFLICT(key) DO UPDATE SET votes=excluded.votes, exposure=excluded.exposure, updated_at=excluded.updated_at`,
				int64(u.Key), u.Votes, u.Exposure, now); err != nil {
				return err
			}
		}
		return nil
	})
}

// LoadUnits reads all unit histories.
func (s *Store) LoadUnits(ctx context.Context) ([]UnitRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, votes, exposure FROM units`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UnitRow
	for rows.Next() {
		var u UnitRow
		var k int64
		if err := rows.Scan(&k, &u.Votes, &u.Exposure); err != nil {
			return nil, err
		}
		u.Key = uint64(k)
		out = append(out, u)
	}
	return out, rows.Err()
}

// ---- Catalog -------------------------------------------------------------------

// CatalogEvent is an earthquake from an authoritative external catalog.
type CatalogEvent struct {
	Source  string  `json:"source"`
	ID      string  `json:"id"`
	Time    float64 `json:"time"`
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	DepthKM float64 `json:"depthKm"`
	Mag     float64 `json:"mag"`
	MagType string  `json:"magType"`
	Place   string  `json:"place"`
	URL     string  `json:"url"`
}

// UpsertCatalog stores catalog events.
func (s *Store) UpsertCatalog(ctx context.Context, evs []CatalogEvent, now float64) error {
	return s.Sync(ctx, func(tx *sql.Tx) error {
		for _, e := range evs {
			if _, err := tx.Exec(`INSERT INTO catalog(source, id, time, lat, lng, depth, mag, mag_type, place, url, fetched_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)
				ON CONFLICT(source, id) DO UPDATE SET time=excluded.time, lat=excluded.lat, lng=excluded.lng, depth=excluded.depth, mag=excluded.mag, mag_type=excluded.mag_type, place=excluded.place, url=excluded.url, fetched_at=excluded.fetched_at`,
				e.Source, e.ID, e.Time, e.Lat, e.Lng, e.DepthKM, e.Mag, e.MagType, e.Place, e.URL, now); err != nil {
				return err
			}
		}
		return nil
	})
}

// CatalogSince lists catalog events with time >= since.
func (s *Store) CatalogSince(ctx context.Context, since float64, limit int) ([]CatalogEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT source, id, time, lat, lng, depth, mag, mag_type, place, url FROM catalog WHERE time >= ? ORDER BY time DESC LIMIT ?`, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CatalogEvent
	for rows.Next() {
		var e CatalogEvent
		if err := rows.Scan(&e.Source, &e.ID, &e.Time, &e.Lat, &e.Lng, &e.DepthKM, &e.Mag, &e.MagType, &e.Place, &e.URL); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---- Stats, alerts, audit ------------------------------------------------------

// InsertStatsMinute stores one minute of network statistics.
func (s *Store) InsertStatsMinute(t int64, doc []byte) {
	s.Async(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT OR REPLACE INTO stats_minute(t, doc) VALUES(?,?)`, t, string(doc))
		return err
	})
}

// StatsSince returns minute statistics since t.
func (s *Store) StatsSince(ctx context.Context, since int64) ([]json.RawMessage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT doc FROM stats_minute WHERE t >= ? ORDER BY t`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []json.RawMessage
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out = append(out, json.RawMessage(d))
	}
	return out, rows.Err()
}

// ClaimAlert records that an alert for (event, level, channel) is being
// sent; it returns false if one was already recorded (exactly-once across
// restarts).
func (s *Store) ClaimAlert(ctx context.Context, eventID, level, channel string, now float64) (bool, error) {
	var claimed bool
	err := s.Sync(ctx, func(tx *sql.Tx) error {
		r, err := tx.Exec(`INSERT OR IGNORE INTO alerts(event_id, level, channel, at, ok, detail) VALUES(?,?,?,?,0,'')`, eventID, level, channel, now)
		if err != nil {
			return err
		}
		n, _ := r.RowsAffected()
		claimed = n == 1
		return nil
	})
	return claimed, err
}

// FinishAlert records the delivery outcome.
func (s *Store) FinishAlert(eventID, level, channel string, ok bool, detail string) {
	s.Async(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE alerts SET ok=?, detail=? WHERE event_id=? AND level=? AND channel=?`, ok, detail, eventID, level, channel)
		return err
	})
}

// Audit appends to the audit log.
func (s *Store) Audit(actor, action, detail string, now float64) {
	s.Async(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO audit(at, actor, action, detail) VALUES(?,?,?,?)`, now, actor, action, detail)
		return err
	})
}

// Retention holds data-retention periods in seconds (0 = keep forever).
type Retention struct {
	Picks, Snippets, Stats, IdleDevices float64
}

// Prune deletes data older than the retention periods.
func (s *Store) Prune(ctx context.Context, r Retention, now float64) error {
	return s.Sync(ctx, func(tx *sql.Tx) error {
		if r.Picks > 0 {
			if _, err := tx.Exec(`DELETE FROM picks WHERE arrived_at < ?`, now-r.Picks); err != nil {
				return err
			}
		}
		if r.Snippets > 0 {
			if _, err := tx.Exec(`DELETE FROM snippets WHERE created_at < ?`, now-r.Snippets); err != nil {
				return err
			}
		}
		if r.Stats > 0 {
			if _, err := tx.Exec(`DELETE FROM stats_minute WHERE t < ?`, int64(now-r.Stats)); err != nil {
				return err
			}
		}
		if r.IdleDevices > 0 {
			if _, err := tx.Exec(`DELETE FROM picks WHERE device_id IN (SELECT id FROM devices WHERE last_seen < ?)`, now-r.IdleDevices); err != nil {
				return err
			}
			if _, err := tx.Exec(`DELETE FROM snippets WHERE device_id IN (SELECT id FROM devices WHERE last_seen < ?)`, now-r.IdleDevices); err != nil {
				return err
			}
			if _, err := tx.Exec(`DELETE FROM devices WHERE last_seen < ?`, now-r.IdleDevices); err != nil {
				return err
			}
		}
		return nil
	})
}
