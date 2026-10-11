package protocol

import (
	"errors"
	"testing"
)

func good(now float64) *Batch {
	b := &Batch{Type: "batch", Seq: 1, T0: now - 1000, Clock: Clock{OffsetMS: 0, RTTMS: 80}}
	for i := 0; i < 60; i++ {
		b.DT = append(b.DT, int64(i)*167)
		b.X = append(b.X, 10)
		b.Y = append(b.Y, -20)
		b.Z = append(b.Z, 9807)
	}
	return b
}

func TestValidateAcceptsGoodBatch(t *testing.T) {
	now := 1.8e12
	first, last, err := good(now).Validate(now)
	if err != nil {
		t.Fatal(err)
	}
	if last <= first || last > now/1000 {
		t.Fatalf("times %f %f", first, last)
	}
}

func TestValidateRejections(t *testing.T) {
	now := 1.8e12
	cases := map[string]struct {
		mut  func(*Batch)
		want error
	}{
		"empty":        {func(b *Batch) { b.DT, b.X, b.Y, b.Z = nil, nil, nil, nil }, ErrEmpty},
		"shape":        {func(b *Batch) { b.X = b.X[:10] }, ErrShape},
		"order":        {func(b *Batch) { b.DT[5] = 1 }, ErrOrder},
		"range":        {func(b *Batch) { b.Z[3] = 200_000 }, ErrRange},
		"future":       {func(b *Batch) { b.T0 = now + 10_000 }, ErrFuture},
		"stale":        {func(b *Batch) { b.T0 = now - 200_000 }, ErrStale},
		"clock offset": {func(b *Batch) { b.Clock.OffsetMS = 1e10 }, ErrClock},
		"span":         {func(b *Batch) { b.DT[59] = 40_000 }, ErrSpan},
		"too large": {func(b *Batch) {
			for len(b.DT) <= MaxBatchSamples {
				b.DT = append(b.DT, b.DT[len(b.DT)-1])
				b.X, b.Y, b.Z = append(b.X, 0), append(b.Y, 0), append(b.Z, 0)
			}
		}, ErrTooLarge},
	}
	for name, c := range cases {
		b := good(now)
		c.mut(b)
		if _, _, err := b.Validate(now); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v want %v", name, err, c.want)
		}
	}
}

func TestDecodeRejectsUnknownType(t *testing.T) {
	if _, err := Decode([]byte(`{"type":"shake"}`)); err == nil {
		t.Fatal("unknown type accepted")
	}
	v, err := Decode([]byte(`{"type":"sync","c0":123.5}`))
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := v.(*Sync); !ok || s.C0 != 123.5 {
		t.Fatalf("decoded %#v", v)
	}
}
