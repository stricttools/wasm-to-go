package libc

import (
	"math"
	"testing"
)

// fma rounds once, and returns the positive canonical NaN for every NaN,
// whatever NaN its operands hold.
func Test_fma(t *testing.T) {
	const canon = 0x7ff8000000000000
	f := math.Float64frombits
	negZero := math.Copysign(0, -1)
	tests := []struct {
		x, y, z float64
		want    uint64
	}{
		{2, 3, 4, 0x4024000000000000},
		// (1+2^-52)*(1-2^-53) - 1 is 2^-53-2^-105 exactly; a multiply
		// then an add gives 0.
		{f(0x3ff0000000000001), f(0x3fefffffffffffff), -1, 0x3c9ffffffffffffe},
		{1, 0, negZero, 0},
		{-1, 0, negZero, 0x8000000000000000},
		{f(0x7fefffffffffffff), 2, f(0xffefffffffffffff), 0x7fefffffffffffff},
		{1, 1, f(0xfff8000000000123), canon},
		{1, 1, f(0x7ff4000000000001), canon},
		{f(0xfff8000000000123), 1, 1, canon},
		{1, f(0x7ff4000000000001), 1, canon},
		{math.Inf(1), 0, 1, canon},
		{math.Inf(1), 1, math.Inf(-1), canon},
	}
	for _, tc := range tests {
		if got := math.Float64bits(fma(tc.x, tc.y, tc.z)); got != tc.want {
			t.Errorf("fma(%#x, %#x, %#x) = %#x, want %#x", math.Float64bits(tc.x),
				math.Float64bits(tc.y), math.Float64bits(tc.z), got, tc.want)
		}
	}
}
