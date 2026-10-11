package server

// Research credit and invites:
//
//	GET  /api/v1/devices/me/credit   my name, totals, invite code and how many I brought
//	POST /api/v1/devices/me/credit   {"name": "..."} sets (or "" removes) my name
//	GET  /api/v1/contributors        public list of named contributors
//	GET  /api/admin/credits          every name, including hidden ones
//	POST /api/admin/credits/hide     {"ref": "...", "hidden": true} hides a name
//
// A name appears publicly once its phone has listened for an hour, so the list
// shows people who actually contributed. Names never enter the recordings.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const listedAfterSec = 3600

// RefCode is a device's invite code (stable, not reversible to the device).
func (s *Server) RefCode(device string) string {
	m := hmac.New(sha256.New, s.keys.Derive("invite"))
	m.Write([]byte(device))
	return strings.ToLower(base32.StdEncoding.EncodeToString(m.Sum(nil))[:7])
}

// normName cleans a display name: letters (any script), spaces, - . '
// 2 to 40 characters, at least two letters. ok=false if it is not a name.
func normName(in string) (string, bool) {
	in = strings.Join(strings.Fields(in), " ")
	if in == "" {
		return "", true
	}
	letters := 0
	for _, r := range in {
		switch {
		case unicode.IsLetter(r):
			letters++
		case r == ' ' || r == '-' || r == '.' || r == '\'' || r == '’':
		default:
			return "", false
		}
	}
	n := utf8.RuneCountInString(in)
	return in, letters >= 2 && n <= 40
}

func hoursOf(sec float64) float64 { return math.Round(sec/360) / 10 }

func (s *Server) handleMyCredit(w http.ResponseWriter, r *http.Request) {
	d, err := s.authDevice(r)
	if err != nil {
		errJSON(w, 401, "unauthorized")
		return
	}
	ctx := r.Context()
	if r.Method == http.MethodPost {
		var req struct{ Name string }
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil {
			errJSON(w, 400, "invalid request")
			return
		}
		name, ok := normName(req.Name)
		if !ok {
			errJSON(w, 400, "use letters only, 2 to 40 characters")
			return
		}
		if err := s.st.SetCredit(ctx, d.ID, name, s.now()); err != nil {
			errJSON(w, 503, "could not save; retry")
			return
		}
	}
	ref := s.RefCode(d.ID)
	if err := s.st.PutRef(ctx, ref, d.ID); err != nil {
		errJSON(w, 503, "unavailable")
		return
	}
	name, hidden, err := s.st.Credit(ctx, d.ID)
	if err != nil {
		errJSON(w, 503, "unavailable")
		return
	}
	var still float64
	full := 0
	if rows, err := s.st.DeviceNights(ctx, d.ID); err == nil {
		for _, n := range rows {
			still += n.StillSec
			if n.StillSec >= s.minHours()*3600 {
				full++
			}
		}
	}
	active, joined, _ := s.st.InvitedCount(ctx, d.ID)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"name": name, "hidden": hidden, "listed": name != "" && !hidden && still >= listedAfterSec,
		"hours": hoursOf(still), "fullNights": full, "minHours": s.minHours(), "listedAfterHours": listedAfterSec / 3600,
		"ref": ref, "invited": active, "invitedJoined": joined})
}

type contributorOut struct {
	Name    string  `json:"name"`
	Hours   float64 `json:"hours"`
	Nights  int     `json:"nights"`
	Invited int     `json:"invited"`
	Since   float64 `json:"since"`
	Ref     string  `json:"ref,omitempty"`    // admin
	Hidden  bool    `json:"hidden,omitempty"` // admin
	Cohort  string  `json:"cohort,omitempty"` // admin
}

func (s *Server) contributors(r *http.Request, admin bool) ([]contributorOut, error) {
	list, err := s.st.Contributors(r.Context(), s.minHours()*3600)
	if err != nil {
		return nil, err
	}
	out := []contributorOut{}
	for _, c := range list {
		if !admin && (c.Hidden || c.StillSec < listedAfterSec) {
			continue
		}
		o := contributorOut{Name: c.Name, Hours: hoursOf(c.StillSec), Nights: c.FullNights, Invited: c.Invited, Since: math.Round(c.Created)}
		if admin {
			o.Ref, o.Hidden, o.Cohort = s.RefCode(c.DeviceID), c.Hidden, c.Cohort
		}
		out = append(out, o)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Hours != out[j].Hours {
			return out[i].Hours > out[j].Hours
		}
		return out[i].Since < out[j].Since
	})
	return out, nil
}

func (s *Server) handleContributors(w http.ResponseWriter, r *http.Request) {
	list, err := s.contributors(r, false)
	if err != nil {
		errJSON(w, 503, "unavailable")
		return
	}
	phones, still, _ := s.st.ListeningTotals(r.Context())
	w.Header().Set("Cache-Control", "public, max-age=60")
	writeJSON(w, 200, map[string]any{"people": list, "phones": phones, "hours": hoursOf(still),
		"listedAfterHours": listedAfterSec / 3600, "updated": time.Now().Unix()})
}

func (s *Server) handleAdminCredits(w http.ResponseWriter, r *http.Request) {
	list, err := s.contributors(r, true)
	if err != nil {
		errJSON(w, 503, "unavailable")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"people": list})
}

func (s *Server) handleAdminHideCredit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Ref    string
		Hidden bool
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil || req.Ref == "" {
		errJSON(w, 400, "invalid request")
		return
	}
	id, err := s.st.RefOwner(r.Context(), strings.ToLower(req.Ref))
	if err != nil || id == "" {
		errJSON(w, 404, "unknown")
		return
	}
	if err := s.st.SetCreditHidden(r.Context(), id, req.Hidden); err != nil {
		errJSON(w, 503, "could not save")
		return
	}
	writeJSON(w, 200, map[string]any{"ref": req.Ref, "hidden": req.Hidden})
}
