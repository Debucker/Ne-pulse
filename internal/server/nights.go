package server

import (
	"time"
	_ "time/tzdata" // Asia/Tashkent on hosts without a zoneinfo database
)

var tashkent = func() *time.Location {
	if l, err := time.LoadLocation("Asia/Tashkent"); err == nil {
		return l
	}
	return time.FixedZone("UZT", 5*3600)
}()

// NightOf names the night a moment belongs to: nights run from 12:00 to
// 12:00 Tashkent time and are named by the evening's date.
func NightOf(unix float64) string {
	t := time.Unix(int64(unix), 0).In(tashkent).Add(-12 * time.Hour)
	return t.Format("2006-01-02")
}
