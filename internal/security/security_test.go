package security

import (
	"errors"
	"net"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPoWRoundTrip(t *testing.T) {
	k, _ := NewKeys("test-secret")
	p := NewPoW(k, 12, time.Minute)
	now := time.Unix(1_800_000_000, 0)
	c := p.Issue(now)
	nonce := Solve(c.Challenge, c.Difficulty)
	if err := p.Verify(c.Challenge, nonce, now); err != nil {
		t.Fatal(err)
	}
	if err := p.Verify(c.Challenge, nonce, now); !errors.Is(err, ErrReused) {
		t.Fatalf("reuse: %v", err)
	}
	c2 := p.Issue(now)
	if err := p.Verify(c2.Challenge, "0", now); !errors.Is(err, ErrWork) && err != nil {
		t.Fatalf("weak nonce: %v", err)
	}
	if err := p.Verify(c2.Challenge, Solve(c2.Challenge, 12), now.Add(2*time.Minute)); !errors.Is(err, ErrExpired) {
		t.Fatalf("expiry: %v", err)
	}
	// Tampering with the difficulty invalidates the signature.
	forged := "9999999999.1." + c2.Challenge[len("0000000000.12."):]
	if err := p.Verify(forged, Solve(forged, 1), now); !errors.Is(err, ErrChallenge) {
		t.Fatalf("forgery: %v", err)
	}
}

func TestCredentials(t *testing.T) {
	id, secret, hash := NewDeviceCredentials()
	if !ValidDeviceID(id) {
		t.Fatalf("invalid id %q", id)
	}
	if !CheckSecret(secret, hash) || CheckSecret(secret+"x", hash) {
		t.Fatal("secret check")
	}
	if ValidDeviceID("dv_../../etc/passwd") {
		t.Fatal("bad id accepted")
	}
}

func TestSourceHashGroupsPrefixesAndRotates(t *testing.T) {
	k, _ := NewKeys("s")
	h := NewSourceHasher(k)
	day := time.Unix(1_800_000_000, 0)
	a := h.Hash(net.ParseIP("203.0.113.7"), day)
	b := h.Hash(net.ParseIP("203.0.113.200"), day)
	c := h.Hash(net.ParseIP("203.0.114.7"), day)
	if a != b || a == c {
		t.Fatalf("prefix grouping wrong: %s %s %s", a, b, c)
	}
	if h.Hash(net.ParseIP("203.0.113.7"), day.Add(24*time.Hour)) == a {
		t.Fatal("hash did not rotate")
	}
}

func TestClientIPIgnoresSpoofedForwardedFor(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.5:1234"
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 198.51.100.9")
	// One trusted proxy appends the real client: use the right-most entry.
	if ip := (ClientIP{Header: "X-Forwarded-For", Hops: 1}).Get(r); ip.String() != "198.51.100.9" {
		t.Fatalf("got %v", ip)
	}
	// No trusted header: peer address only.
	if ip := (ClientIP{}).Get(r); ip.String() != "10.0.0.5" {
		t.Fatalf("got %v", ip)
	}
	r.Header.Set("CF-Connecting-IP", "192.0.2.44")
	if ip := (ClientIP{Header: "CF-Connecting-IP"}).Get(r); ip.String() != "192.0.2.44" {
		t.Fatalf("got %v", ip)
	}
}
