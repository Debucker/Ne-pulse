// Package security holds NE-PULSE's anti-abuse primitives: proof-of-work
// enrollment challenges, device credentials, privacy-preserving network
// source hashes, and trusted-proxy-aware client IP extraction.
package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/bits"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Keys derives purpose-specific keys from one server secret.
type Keys struct{ master []byte }

// NewKeys builds key material. An empty secret generates a random one
// (fine for development; production must set a stable secret so challenges
// and source hashes survive restarts).
func NewKeys(secret string) (*Keys, bool) {
	if secret == "" {
		b := make([]byte, 32)
		_, _ = rand.Read(b)
		return &Keys{master: b}, false
	}
	return &Keys{master: []byte(secret)}, true
}

// Derive returns a purpose-specific key (for components outside this package).
func (k *Keys) Derive(purpose string) []byte { return k.derive(purpose) }

func (k *Keys) derive(purpose string) []byte {
	m := hmac.New(sha256.New, k.master)
	m.Write([]byte(purpose))
	return m.Sum(nil)
}

// ---- Proof of work ---------------------------------------------------------

// PoW issues and verifies hashcash-style enrollment challenges. A client
// must find a nonce such that SHA-256(challenge ":" nonce) has at least
// Difficulty leading zero bits: ~2^Difficulty hashes (about a second in a
// phone browser at the default 18), which makes registering thousands of
// fake devices expensive without bothering a real user.
type PoW struct {
	key        []byte
	Difficulty int
	TTL        time.Duration
	mu         sync.Mutex
	used       map[string]time.Time
}

// NewPoW builds a challenge issuer.
func NewPoW(k *Keys, difficulty int, ttl time.Duration) *PoW {
	return &PoW{key: k.derive("pow"), Difficulty: difficulty, TTL: ttl, used: map[string]time.Time{}}
}

// Challenge is sent to a client.
type Challenge struct {
	Challenge  string `json:"challenge"`
	Difficulty int    `json:"difficulty"`
	ExpiresAt  int64  `json:"expiresAt"`
}

// Issue creates a new signed challenge.
func (p *PoW) Issue(now time.Time) Challenge {
	nonce := make([]byte, 12)
	_, _ = rand.Read(nonce)
	exp := now.Add(p.TTL).Unix()
	payload := fmt.Sprintf("%d.%d.%s", exp, p.Difficulty, base64.RawURLEncoding.EncodeToString(nonce))
	m := hmac.New(sha256.New, p.key)
	m.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(m.Sum(nil)[:16])
	return Challenge{Challenge: payload + "." + sig, Difficulty: p.Difficulty, ExpiresAt: exp}
}

// Errors returned by Verify.
var (
	ErrChallenge = errors.New("invalid challenge")
	ErrExpired   = errors.New("challenge expired")
	ErrReused    = errors.New("challenge already used")
	ErrWork      = errors.New("insufficient proof of work")
)

// LeadingZeroBits counts leading zero bits of h.
func LeadingZeroBits(h []byte) int {
	n := 0
	for _, b := range h {
		if b == 0 {
			n += 8
			continue
		}
		return n + bits.LeadingZeros8(b)
	}
	return n
}

// Verify checks a solved challenge and marks it used.
func (p *PoW) Verify(challenge, nonce string, now time.Time) error {
	parts := strings.Split(challenge, ".")
	if len(parts) != 4 || len(nonce) == 0 || len(nonce) > 32 {
		return ErrChallenge
	}
	payload := strings.Join(parts[:3], ".")
	m := hmac.New(sha256.New, p.key)
	m.Write([]byte(payload))
	want := base64.RawURLEncoding.EncodeToString(m.Sum(nil)[:16])
	if subtle.ConstantTimeCompare([]byte(want), []byte(parts[3])) != 1 {
		return ErrChallenge
	}
	exp, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return ErrChallenge
	}
	if now.Unix() > exp {
		return ErrExpired
	}
	diff, err := strconv.Atoi(parts[1])
	if err != nil || diff < p.Difficulty {
		return ErrChallenge
	}
	h := sha256.Sum256([]byte(challenge + ":" + nonce))
	if LeadingZeroBits(h[:]) < diff {
		return ErrWork
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for c, t := range p.used {
		if now.After(t) {
			delete(p.used, c)
		}
	}
	if _, ok := p.used[challenge]; ok {
		return ErrReused
	}
	p.used[challenge] = time.Unix(exp, 0)
	return nil
}

// Solve brute-forces a challenge (tests, simulator and load generator).
func Solve(challenge string, difficulty int) string {
	for i := 0; ; i++ {
		n := strconv.Itoa(i)
		h := sha256.Sum256([]byte(challenge + ":" + n))
		if LeadingZeroBits(h[:]) >= difficulty {
			return n
		}
	}
}

// ---- Device credentials ------------------------------------------------------

// NewDeviceCredentials returns a random pseudonymous device ID and a secret.
// Only SHA-256(secret) is stored; the secret has 256 bits of entropy, so a
// fast hash is sufficient.
func NewDeviceCredentials() (id, secret string, hash []byte) {
	a := make([]byte, 16)
	b := make([]byte, 32)
	_, _ = rand.Read(a)
	_, _ = rand.Read(b)
	id = "dv_" + base64.RawURLEncoding.EncodeToString(a)
	secret = base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(secret))
	return id, secret, h[:]
}

// CheckSecret compares a presented secret with a stored hash in constant time.
func CheckSecret(secret string, hash []byte) bool {
	h := sha256.Sum256([]byte(secret))
	return len(hash) == len(h) && subtle.ConstantTimeCompare(h[:], hash) == 1
}

// ValidDeviceID checks the ID format before touching the database.
func ValidDeviceID(id string) bool {
	if !strings.HasPrefix(id, "dv_") || len(id) != 25 {
		return false
	}
	_, err := base64.RawURLEncoding.DecodeString(id[3:])
	return err == nil
}

// ---- Network sources -------------------------------------------------------

// SourceHasher maps a client IP to a short keyed hash of its network prefix
// (/24 for IPv4, /48 for IPv6). The key rotates daily, so hashes cannot be
// linked across days or reversed to an address; within an event they tell
// the detector how many *different networks* its evidence came from, which
// bounds what one attacker on one network can fabricate.
type SourceHasher struct{ key []byte }

// NewSourceHasher builds a hasher.
func NewSourceHasher(k *Keys) *SourceHasher { return &SourceHasher{key: k.derive("source")} }

// Hash returns the source hash for ip on the UTC day of now.
func (s *SourceHasher) Hash(ip net.IP, now time.Time) string {
	var prefix []byte
	if v4 := ip.To4(); v4 != nil {
		prefix = append([]byte{4}, v4[:3]...)
	} else if ip != nil {
		prefix = append([]byte{6}, ip.To16()[:6]...)
	} else {
		prefix = []byte{0}
	}
	day := make([]byte, 8)
	binary.BigEndian.PutUint64(day, uint64(now.UTC().Unix()/86400))
	m := hmac.New(sha256.New, s.key)
	m.Write(day)
	m.Write(prefix)
	return hex.EncodeToString(m.Sum(nil)[:6])
}

// ---- Client IP -------------------------------------------------------------

// ClientIP extracts the client address. header names a header set by a
// trusted reverse proxy ("CF-Connecting-IP", "X-Real-IP", or
// "X-Forwarded-For"); for X-Forwarded-For, hops is how many trusted proxies
// append to it, and the address that many entries from the right is used —
// never the left-most, client-controlled one. With header == "" only the
// TCP peer address is trusted.
type ClientIP struct {
	Header string
	Hops   int
}

// Get returns the client IP for r.
func (c ClientIP) Get(r *http.Request) net.IP {
	if c.Header != "" {
		v := r.Header.Get(c.Header)
		if v != "" {
			if strings.EqualFold(c.Header, "X-Forwarded-For") {
				parts := strings.Split(v, ",")
				hops := c.Hops
				if hops < 1 {
					hops = 1
				}
				if i := len(parts) - hops; i >= 0 {
					if ip := net.ParseIP(strings.TrimSpace(parts[i])); ip != nil {
						return ip
					}
				}
			} else if ip := net.ParseIP(strings.TrimSpace(v)); ip != nil {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return net.ParseIP(host)
}

// Key returns a rate-limit key for r (the IP as a string).
func (c ClientIP) Key(r *http.Request) string {
	ip := c.Get(r)
	if ip == nil {
		return "unknown"
	}
	return ip.String()
}

// ConstantTimeEqual compares two tokens.
func ConstantTimeEqual(a, b string) bool {
	return len(a) > 0 && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
