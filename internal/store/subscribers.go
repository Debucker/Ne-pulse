package store

import (
	"context"
	"database/sql"
)

// Subscriber is one Telegram chat that asked for alerts. Only the chat id
// (needed to deliver a message), a 10 km public area and a language are
// kept, plus, if the person linked their phone from the website, that
// phone's device id (so the bot can say when it stopped listening). /stop
// deletes the row.
type Subscriber struct {
	ChatID   int64
	Row      int32
	Col      int32
	Lang     string
	Created  float64
	Updated  float64
	DeviceID string  // linked phone, "" if none
	Nudged   float64 // last "your phone stopped listening" message (unix s)
	Morning  string  // last night summarised to this chat (YYYY-MM-DD)
}

const subCols = `chat_id, area_row, area_col, lang, created, updated, device_id, nudged, morning`

func scanSub(sc interface{ Scan(...any) error }, sub *Subscriber) error {
	return sc.Scan(&sub.ChatID, &sub.Row, &sub.Col, &sub.Lang, &sub.Created, &sub.Updated, &sub.DeviceID, &sub.Nudged, &sub.Morning)
}

// UpsertSubscriber creates or moves a subscription. An empty DeviceID keeps
// the phone that is already linked.
func (s *Store) UpsertSubscriber(ctx context.Context, sub Subscriber) error {
	return s.Sync(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO subscribers(chat_id, area_row, area_col, lang, created, updated, device_id) VALUES(?,?,?,?,?,?,?)
			ON CONFLICT(chat_id) DO UPDATE SET area_row=excluded.area_row, area_col=excluded.area_col, lang=excluded.lang, updated=excluded.updated,
			device_id=CASE WHEN excluded.device_id<>'' THEN excluded.device_id ELSE subscribers.device_id END`,
			sub.ChatID, sub.Row, sub.Col, sub.Lang, sub.Created, sub.Updated, sub.DeviceID)
		return err
	})
}

// SetSubscriberNudged records when the "stopped listening" reminder went out.
func (s *Store) SetSubscriberNudged(ctx context.Context, chatID int64, at float64) error {
	return s.Sync(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE subscribers SET nudged=? WHERE chat_id=?`, at, chatID)
		return err
	})
}

// SetSubscriberMorning records which night's summary went to a chat.
func (s *Store) SetSubscriberMorning(ctx context.Context, chatID int64, night string) error {
	return s.Sync(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE subscribers SET morning=? WHERE chat_id=?`, night, chatID)
		return err
	})
}

// UnlinkDevice forgets a phone in every chat that linked it (the phone was deleted).
func (s *Store) UnlinkDevice(ctx context.Context, deviceID string) error {
	return s.Sync(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE subscribers SET device_id='' WHERE device_id=?`, deviceID)
		return err
	})
}

// DeviceLinked reports whether any chat has linked this phone.
func (s *Store) DeviceLinked(ctx context.Context, deviceID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscribers WHERE device_id=?`, deviceID).Scan(&n)
	return n > 0, err
}

// SetSubscriberLang changes only the language of an existing subscription.
func (s *Store) SetSubscriberLang(ctx context.Context, chatID int64, lang string, now float64) error {
	return s.Sync(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE subscribers SET lang=?, updated=? WHERE chat_id=?`, lang, now, chatID)
		return err
	})
}

// DeleteSubscriber removes a subscription (no-op if absent).
func (s *Store) DeleteSubscriber(ctx context.Context, chatID int64) error {
	return s.Sync(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`DELETE FROM subscribers WHERE chat_id=?`, chatID)
		return err
	})
}

// GetSubscriber returns one subscription; ok is false if there is none.
func (s *Store) GetSubscriber(ctx context.Context, chatID int64) (Subscriber, bool, error) {
	var sub Subscriber
	err := scanSub(s.db.QueryRowContext(ctx, `SELECT `+subCols+` FROM subscribers WHERE chat_id=?`, chatID), &sub)
	if err == sql.ErrNoRows {
		return sub, false, nil
	}
	return sub, err == nil, err
}

// Subscribers lists every subscription.
func (s *Store) Subscribers(ctx context.Context) ([]Subscriber, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+subCols+` FROM subscribers`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Subscriber
	for rows.Next() {
		var sub Subscriber
		if err := scanSub(rows, &sub); err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}
