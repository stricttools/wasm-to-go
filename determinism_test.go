//go:build !generator

package main

import (
	"testing"

	nancanon_test "github.com/stricttools/wasm-to-go/testdata/regression/nancanon"
)

const (
	canon64 = 0x7ff8000000000000 // the positive canonical NaNs
	canon32 = 0x7fc00000
)

// Every float operation other than abs, neg, copysign, and the
// reinterpretations returns the positive canonical NaN whenever its result
// is a NaN (the WebAssembly deterministic profile), whatever NaN the CPU
// produced; abs, neg, copysign, loads, and stores keep a NaN's bits.
func Test_regression_nancanon(t *testing.T) {
	m := nancanon_test.New()
	u32 := func(x uint32) int32 { return int32(x) }
	u64 := func(x uint64) int64 { return int64(x) }
	one := u64(0x3ff0000000000000)

	for _, tt := range []struct {
		name      string
		got, want int64
	}{
		{"div64(0, 0)", m.Xdiv64(0, 0), canon64},
		{"div64(-NaN with payload, 1)", m.Xdiv64(u64(0xfff8000000000123), one), canon64},
		{"add64(-NaN with payload, 0)", m.Xadd64(u64(0xfff8000000000123), 0), canon64},
		{"add64(sNaN, 0)", m.Xadd64(u64(0x7ff4000000000001), 0), canon64},
		{"sqrt64(-1)", m.Xsqrt64(u64(0xbff0000000000000)), canon64},
		{"floor64(sNaN)", m.Xfloor64(u64(0x7ff4000000000000)), canon64},
		{"promote(-NaN with payload)", m.Xpromote(u32(0xffc00001)), canon64},
		{"demote(-NaN with payload)", int64(m.Xdemote(u64(0xfff8000000000123))), canon32},
		{"sub32(sNaN, 0)", int64(m.Xsub32(u32(0x7fa00000), 0)), canon32},
		{"nearest32(-sNaN)", int64(m.Xnearest32(u32(0xff800001))), canon32},
		{"neg64(sNaN)", m.Xneg64(u64(0x7ff4000000000001)), u64(0xfff4000000000001)},
		{"abs64(-NaN with payload)", m.Xabs64(u64(0xfff8000000000123)), u64(0x7ff8000000000123)},
		{"neg32(sNaN)", int64(m.Xneg32(u32(0x7fa00001))), int64(u32(0xffa00001))},
		{"copysign64(sNaN, -1)", m.Xcopysign64(u64(0x7ff4000000000001), u64(0xbff0000000000000)), u64(0xfff4000000000001)},
		{"storeload64(-sNaN)", m.Xstoreload64(u64(0xfff4000000000001)), u64(0xfff4000000000001)},
		{"negsum64(NaN with payload, 0)", m.Xnegsum64(u64(0x7ff8000000000123), 0), u64(0xfff8000000000000)},
		{"muladd64(-NaN with payload, 1, 1)", m.Xmuladd64(u64(0xfff8000000000123), one, one), canon64},
		{"muladd64(0, inf, 1)", m.Xmuladd64(0, u64(0x7ff0000000000000), one), canon64},
	} {
		if tt.got != tt.want {
			t.Errorf("%s = %#x, want %#x", tt.name, uint64(tt.got), uint64(tt.want))
		}
	}
}
