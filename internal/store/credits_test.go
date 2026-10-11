package store

import (
	"context"
	"testing"
)

func TestCreditsInvitesAndDeletion(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	for _, id := range []string{"a", "b", "c"} {
		if err := s.CreateDevice(ctx, id, []byte("h"), "ios", 1); err != nil {
			t.Fatal(err)
		}
		s.SetDeviceCohort(ctx, id, "8A11UZ")
	}
	s.CreateDevice(ctx, "loadtest", []byte("h"), "android", 1) // not in a class: not in the totals
	s.AddNight(Night{DeviceID: "loadtest", Night: "2026-10-11", StillSec: 9999})
	s.SetCredit(ctx, "a", "Alisher T.", 10)
	s.PutRef(ctx, "refa", "a")
	if id, _ := s.RefOwner(ctx, "refa"); id != "a" {
		t.Fatalf("ref owner %q", id)
	}
	s.AddInvite(ctx, "b", "a", 11)
	s.AddInvite(ctx, "c", "a", 12)
	s.AddInvite(ctx, "b", "c", 13) // first invite wins
	s.AddNight(Night{DeviceID: "a", Night: "2026-10-11", StillSec: 5 * 3600})
	s.AddNight(Night{DeviceID: "b", Night: "2026-10-11", StillSec: 2 * 3600})
	s.AddNight(Night{DeviceID: "c", Night: "2026-10-11", StillSec: 600}) // under an hour: not counted
	flush(s)

	list, err := s.Contributors(ctx, 4*3600)
	if err != nil || len(list) != 1 {
		t.Fatalf("contributors %v %+v", err, list)
	}
	c := list[0]
	if c.Name != "Alisher T." || c.StillSec != 5*3600 || c.FullNights != 1 || c.Invited != 1 {
		t.Fatalf("contributor %+v", c)
	}
	active, joined, _ := s.InvitedCount(ctx, "a")
	if active != 1 || joined != 2 {
		t.Fatalf("invited %d/%d", active, joined)
	}
	s.SetCreditHidden(ctx, "a", true)
	if _, hidden, _ := s.Credit(ctx, "a"); !hidden {
		t.Fatal("not hidden")
	}
	if phones, still, _ := s.ListeningTotals(ctx); phones != 3 || still != 5*3600+2*3600+600 {
		t.Fatalf("totals %d %v", phones, still)
	}
	// Deleting the phone removes the name with it.
	if err := s.DeleteDevice(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if name, _, _ := s.Credit(ctx, "a"); name != "" {
		t.Fatal("name survived deletion")
	}
	if id, _ := s.RefOwner(ctx, "refa"); id != "" {
		t.Fatal("ref survived deletion")
	}
}
