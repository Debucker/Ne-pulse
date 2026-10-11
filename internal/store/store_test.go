package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "np.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func flush(s *Store) {
	// the async writer flushes every 200 ms
	time.Sleep(350 * time.Millisecond)
}

func TestDeviceLifecycleAndDeletion(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	if err := s.CreateDevice(ctx, "dv_x", []byte{1, 2}, "ios", 100); err != nil {
		t.Fatal(err)
	}
	s.UpdateDeviceSession("dv_x", 200, 1, 10, 20, 500, 3, 1, 60)
	s.InsertPick(PickRow{DeviceID: "dv_x", Unit: "1:2", Onset: 150, Sigma: 0.2, Class: "seismic", Accepted: true, Features: json.RawMessage(`{}`), ArrivedAt: 153})
	s.InsertSnippet(Snippet{EventID: "ev", DeviceID: "dv_x", Unit: "1:2", T0: 140, Fs: 50, XYZ: [][3]float32{{1, 2, 3}, {4, 5, 6}}}, 200)
	flush(s)
	d, err := s.GetDevice(ctx, "dv_x")
	if err != nil || d.Samples != 500 || d.Row != 10 || d.ConnectedSec != 60 {
		t.Fatalf("device %+v err %v", d, err)
	}
	sn, err := s.Snippets(ctx, "ev")
	if err != nil || len(sn) != 1 || sn[0].XYZ[1][2] != 6 {
		t.Fatalf("snippets %+v err %v", sn, err)
	}
	if err := s.DeleteDevice(ctx, "dv_x"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetDevice(ctx, "dv_x"); err != ErrNotFound {
		t.Fatalf("device survived deletion: %v", err)
	}
	if sn, _ := s.Snippets(ctx, "ev"); len(sn) != 0 {
		t.Fatal("snippet survived device deletion")
	}
	if p, _ := s.RecentPicks(ctx, 10); len(p) != 0 {
		t.Fatal("pick survived device deletion")
	}
}

func TestEventsAndAlertsExactlyOnce(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	s.UpsertEvent("NP-1", "candidate", true, "", 1000, 41, 69, 1005, 1005, []byte(`{"a":1}`))
	s.UpsertEvent("NP-1", "confirmed", false, "closed", 1000, 41, 69, 1005, 1100, []byte(`{"a":2}`))
	s.UpsertEvent("NP-2", "candidate", false, "dismissed", 2000, 41, 69, 2005, 2100, []byte(`{}`))
	flush(s)
	e, err := s.GetEvent(ctx, "NP-1")
	if err != nil || e.Level != "confirmed" || string(e.Doc) != `{"a":2}` || e.CreatedAt != 1005 {
		t.Fatalf("%+v %v", e, err)
	}
	pub, _ := s.ListEvents(ctx, 10, false, 0)
	all, _ := s.ListEvents(ctx, 10, true, 0)
	if len(pub) != 1 || len(all) != 2 {
		t.Fatalf("list public=%d all=%d", len(pub), len(all))
	}
	ok1, _ := s.ClaimAlert(ctx, "NP-1", "confirmed", "telegram", 1)
	ok2, _ := s.ClaimAlert(ctx, "NP-1", "confirmed", "telegram", 2)
	if !ok1 || ok2 {
		t.Fatalf("claims %v %v", ok1, ok2)
	}
	if err := s.RetractEvent(ctx, "NP-1", "test", "admin", 5); err != nil {
		t.Fatal(err)
	}
	if e, _ := s.GetEvent(ctx, "NP-1"); !e.Retracted {
		t.Fatal("not retracted")
	}
}

func TestUnitsRoundTrip(t *testing.T) {
	s := open(t)
	s.SaveUnits([]UnitRow{{Key: 1<<40 | 7, Votes: 3, Exposure: 7200}}, 1)
	flush(s)
	u, err := s.LoadUnits(context.Background())
	if err != nil || len(u) != 1 || u[0].Key != 1<<40|7 || u[0].Exposure != 7200 {
		t.Fatalf("%+v %v", u, err)
	}
}
