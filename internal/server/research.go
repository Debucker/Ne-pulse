package server

// Research and Telegram endpoints for participants' phones:
//
//	POST /api/v1/devices/me/placement  where the phone lies (floor, surface, building)
//	POST /api/v1/devices/me/label      "something just happened": door, steps, ...
//	POST /api/v1/devices/me/telegram   one-time link that ties a Telegram chat to this phone
//
// Placement and labels are only written to the recording when the phone is
// in a research class (it has a cohort); placement is always stored on the
// device so it can be shown back to the participant.

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"ne-pulse/internal/geo"
	"ne-pulse/internal/store"
)

func contextTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

var (
	placeFloors    = []string{"0", "1", "2-3", "4-9", "10+"}
	placeSurfaces  = []string{"table", "floor", "shelf", "windowsill", "bed", "other"}
	placeBuildings = []string{"house", "apartment", "school", "office", "other"}
	labelKinds     = []string{"quiet", "door", "steps", "traffic", "knock", "music", "shake", "other"}
)

func oneOf(v string, set []string) bool {
	for _, s := range set {
		if v == s {
			return true
		}
	}
	return false
}

// telegramLinks holds short-lived codes for /start d<code> deep links.
type telegramLinks struct {
	mu    sync.Mutex
	codes map[string]linkCode
	label map[string]time.Time // per-device label rate limit
}

type linkCode struct {
	dev string
	exp time.Time
}

func (s *Server) links() *telegramLinks {
	s.linksOnce.Do(func() { s.tl = &telegramLinks{codes: map[string]linkCode{}, label: map[string]time.Time{}} })
	return s.tl
}

func (s *Server) handlePlacement(w http.ResponseWriter, r *http.Request) {
	d, err := s.authDevice(r)
	if err != nil {
		errJSON(w, 401, "unauthorized")
		return
	}
	var req struct{ Floor, Surface, Building string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil ||
		!oneOf(req.Floor, placeFloors) || !oneOf(req.Surface, placeSurfaces) || !oneOf(req.Building, placeBuildings) {
		errJSON(w, 400, "invalid placement")
		return
	}
	p := "floor=" + req.Floor + ";surface=" + req.Surface + ";building=" + req.Building
	if err := s.st.SetDevicePlacement(r.Context(), d.ID, p); err != nil {
		errJSON(w, 503, "could not save; retry")
		return
	}
	if d.Cohort != "" && s.rec != nil {
		s.rec.Note(d.ID, d.Cohort, float64(time.Now().UnixMilli()), "placement", p)
	}
	writeJSON(w, 200, map[string]string{"placement": p})
}

func (s *Server) handleLabel(w http.ResponseWriter, r *http.Request) {
	d, err := s.authDevice(r)
	if err != nil {
		errJSON(w, 401, "unauthorized")
		return
	}
	var req struct{ Kind string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256)).Decode(&req); err != nil || !oneOf(req.Kind, labelKinds) {
		errJSON(w, 400, "invalid label")
		return
	}
	if d.Cohort == "" || s.rec == nil {
		errJSON(w, 409, "this phone is not in a research class")
		return
	}
	now := time.Now()
	tl := s.links()
	tl.mu.Lock()
	ok := now.Sub(tl.label[d.ID]) > 800*time.Millisecond
	if ok {
		tl.label[d.ID] = now
	}
	tl.mu.Unlock()
	if !ok {
		errJSON(w, 429, "too fast")
		return
	}
	s.rec.Note(d.ID, d.Cohort, float64(now.UnixMilli()), req.Kind, "")
	writeJSON(w, 200, map[string]any{"kind": req.Kind, "t": now.UnixMilli()})
}

func (s *Server) handleTelegramLink(w http.ResponseWriter, r *http.Request) {
	d, err := s.authDevice(r)
	if err != nil {
		errJSON(w, 401, "unauthorized")
		return
	}
	if s.cfg.TelegramBot == "" {
		errJSON(w, 503, "telegram is not switched on for this server")
		return
	}
	if _, ok := s.deviceCell(d); !ok {
		errJSON(w, 409, "start listening once first, so the phone has an area")
		return
	}
	b := make([]byte, 10)
	rand.Read(b)
	code := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))
	tl := s.links()
	tl.mu.Lock()
	now := time.Now()
	for k, v := range tl.codes {
		if now.After(v.exp) {
			delete(tl.codes, k)
		}
	}
	tl.codes[code] = linkCode{dev: d.ID, exp: now.Add(30 * time.Minute)}
	tl.mu.Unlock()
	writeJSON(w, 200, map[string]string{"url": "https://t.me/" + s.cfg.TelegramBot + "?start=d" + code})
}

// ClaimTelegramLink resolves (once) a code from a /start d<code> deep link to
// the phone and the centre of its 1 km area. Used by the Telegram bot.
func (s *Server) ClaimTelegramLink(code string) (device string, lat, lng float64, ok bool) {
	tl := s.links()
	tl.mu.Lock()
	lc, found := tl.codes[code]
	delete(tl.codes, code)
	tl.mu.Unlock()
	if !found || time.Now().After(lc.exp) {
		return "", 0, 0, false
	}
	ctx, cancel := contextTimeout()
	defer cancel()
	d, err := s.st.GetDevice(ctx, lc.dev)
	if err != nil {
		return "", 0, 0, false
	}
	c, ok := s.deviceCell(d)
	if !ok {
		return "", 0, 0, false
	}
	lat, lng = s.cellCenter(c)
	return d.ID, lat, lng, true
}

// deviceCell is the phone's 1 km area: the stored one, or, during its very
// first session (stored only when a session ends), the live session's.
func (s *Server) deviceCell(d store.Device) (geo.Cell, bool) {
	if d.GridKM > 0 {
		return geo.Cell{Row: d.Row, Col: d.Col}, true
	}
	s.sessMu.Lock()
	defer s.sessMu.Unlock()
	if se := s.sessions[d.ID]; se != nil {
		return se.cell, true
	}
	return geo.Cell{}, false
}

// DeviceSeen reports whether a phone is listening right now and when it was
// last seen (unix s). exists is false once the phone has been deleted.
func (s *Server) DeviceSeen(id string) (connected bool, lastSeen float64, exists bool) {
	s.sessMu.Lock()
	live := s.sessions[id] != nil
	s.sessMu.Unlock()
	if live {
		return true, float64(time.Now().Unix()), true
	}
	ctx, cancel := contextTimeout()
	defer cancel()
	d, err := s.st.GetDevice(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return false, 0, false
	}
	if err != nil {
		return true, 0, true // unknown: say nothing
	}
	return false, d.LastSeen, true
}
