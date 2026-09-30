package helpers

import (
	"math"
	"math/rand/v2"
	"testing"
)

// trunc_u64 is the conversion wherever Go defines it: every float whose
// truncation int64 or uint64 holds.
func Test_trunc_u64(t *testing.T) {
	check := func(f float64) {
		t.Helper()
		got := trunc_u64(f)
		var want uint64
		if f < 0 {
			want = uint64(int64(f))
		} else {
			want = uint64(f)
		}
		if got != want {
			t.Errorf("trunc_u64(%v [%#x]) = %#x, want %#x", f, math.Float64bits(f), got, want)
		}
	}
	for _, f := range []float64{
		0, math.Copysign(0, -1), 0.5, -0.5, 0.9999999999999999, -0.9999999999999999,
		1, -1, 1.5, -1.5, 2, -2, math.SmallestNonzeroFloat64, -math.SmallestNonzeroFloat64,
		0x1p52, 0x1p52 + 1, 0x1p53, -0x1p53, 0x1p63 - 1024, 0x1p63, 0x1p64 - 2048,
		-0x1p63, -0x1p63 + 1024, math.MaxInt32, math.MinInt32, math.MaxUint32,
		math.Nextafter(math.MaxInt32+1, 0), math.Nextafter(math.MinInt32-1, 0),
	} {
		check(f)
	}
	r := rand.New(rand.NewPCG(3, 4))
	for range 1 << 20 {
		// Every exponent where the truncation fits, with random bits.
		b := r.Uint64()&^(0x7ff<<52) | uint64(r.IntN(1023+64))<<52
		f := math.Float64frombits(b)
		if f <= -0x1p63 || f >= 0x1p64 {
			continue
		}
		check(f)
	}
}

// trunc_small is the conversion for every integer of magnitude below 2^51,
// which holds every value an i32 conversion yields.
func Test_trunc_small(t *testing.T) {
	check := func(x float64) {
		t.Helper()
		if got, want := trunc_small(x), int64(x); got != want {
			t.Errorf("trunc_small(%v) = %d, want %d", x, got, want)
		}
	}
	for _, x := range []float64{0, math.Copysign(0, -1), 1, -1, math.MaxInt32, math.MinInt32, math.MaxUint32,
		-math.MaxUint32, 0x1p51 - 1, -0x1p51 + 1} {
		check(x)
	}
	r := rand.New(rand.NewPCG(5, 6))
	for range 1 << 20 {
		check(float64(r.Int64N(1<<52) - 1<<51 + 1))
	}
}
