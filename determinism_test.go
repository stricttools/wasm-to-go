//go:build !generator

package main

import (
	"math"
	"testing"

	constfold_test "github.com/stricttools/wasm-to-go/testdata/regression/constfold"
	f32convert_test "github.com/stricttools/wasm-to-go/testdata/regression/f32convert"
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
		{"min64(-NaN with payload, 1)", m.Xmin64(u64(0xfff8000000000123), one), canon64},
		{"min64(1, sNaN)", m.Xmin64(one, u64(0x7ff4000000000000)), canon64},
		{"max64(-NaN with payload, 1)", m.Xmax64(u64(0xfff8000000000123), one), canon64},
		{"min64(+0, -0)", m.Xmin64(0, u64(0x8000000000000000)), u64(0x8000000000000000)},
		{"max64(-0, +0)", m.Xmax64(u64(0x8000000000000000), 0), 0},
		{"min32(-NaN with payload, 1)", int64(m.Xmin32(u32(0xffc00123), u32(0x3f800000))), canon32},
		{"max32(1, sNaN)", int64(m.Xmax32(u32(0x3f800000), u32(0x7fa00000))), canon32},
		{"min32(+0, -0)", int64(m.Xmin32(0, u32(0x80000000))), int64(u32(0x80000000))},
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

// f32.convert_i64_s and f32.convert_i64_u round once, to nearest even, on
// every architecture (Go's own conversion rounds twice, through float64,
// on 386, arm, and mipsle).
func Test_regression_f32convert(t *testing.T) {
	m := f32convert_test.New()
	for _, tt := range []struct {
		x    uint64
		s, u uint32 // float32 bits of the signed and the unsigned conversion
	}{
		{0, 0, 0},
		{1, 0x3f800000, 0x3f800000},
		{0x800000800001, 0x57000001, 0x57000001},     // 2^47+2^23+1: above the tie, rounds up
		{0x800000800000, 0x57000000, 0x57000000},     // the tie itself, to even
		{0x20000000000003, 0x5a000000, 0x5a000000},   // 2^53+3
		{0x7fffffffffffffff, 0x5f000000, 0x5f000000}, // 2^63-1
		{0x8000008000000001, 0xdeffffff, 0x5f000001}, // -(2^63-2^39-1); 2^63+2^39+1
		{0x8000000000000000, 0xdf000000, 0x5f000000}, // -2^63; 2^63
		{0xffffff7fffffffff, 0xd3000000, 0x5f7fffff}, // -(2^39+1); 2^64-2^39-1
		{0xffffffffffffffff, 0xbf800000, 0x5f800000}, // -1; 2^64-1
		{0xffff7fffff7fffff, 0xd7000001, 0x5f7fff80}, // -(2^47+2^23+1); 2^64-2^47-2^23-1
	} {
		if got := uint32(m.Xs(int64(tt.x))); got != tt.s {
			t.Errorf("f32.convert_i64_s(%#x) = %#x, want %#x", tt.x, got, tt.s)
		}
		if got := uint32(m.Xu(int64(tt.x))); got != tt.u {
			t.Errorf("f32.convert_i64_u(%#x) = %#x, want %#x", tt.x, got, tt.u)
		}
	}
}

// Operations on float constants run with IEEE 754 arithmetic at run time
// (or in the compiler's IEEE folding), never in Go's constant evaluator,
// which computes exactly, has no negative zero, and refuses to compile an
// overflow or a division by zero.
func Test_regression_constfold(t *testing.T) {
	m := constfold_test.New()
	b64 := math.Float64bits
	b32 := math.Float32bits
	for _, tt := range []struct {
		name      string
		got, want uint64
	}{
		{"0 * -1", b64(m.Xzero_times_minus_one()), 0x8000000000000000},
		{"1e-300 * -1e-300", b64(m.Xtiny_times_minus_tiny()), 0x8000000000000000},
		{"1 / 0 (operand)", b64(m.Xdiv_by_zero(1)), 0x7ff0000000000000},
		{"-1 / 0 (operand)", b64(m.Xdiv_by_zero(-1)), 0xfff0000000000000},
		{"0 / 0 (operand)", b64(m.Xdiv_by_zero(0)), canon64},
		{"1 / 0", b64(m.Xone_div_zero()), 0x7ff0000000000000},
		{"0 / 0", b64(m.Xzero_div_zero()), canon64},
		{"1e308 * 10", b64(m.Xoverflow()), 0x7ff0000000000000},
		{"(1+2^-30) * (1-2^-30) + -1", b64(m.Xrounded_product()), 0},
		{"-0 + -0", b64(m.Xminus_zero_sum()), 0x8000000000000000},
		{"float32 0 * -1", uint64(b32(m.Xzero_times_minus_one32())), 0x80000000},
		{"float32 3e38 * 10", uint64(b32(m.Xoverflow32())), 0x7f800000},
		{"float32 1 / 0 (operand)", uint64(b32(m.Xdiv_by_zero32(1))), 0x7f800000},
		{"float32 1 / 3", uint64(b32(m.Xthird32())), 0x3eaaaaab},
	} {
		if tt.got != tt.want {
			t.Errorf("%s = %#x, want %#x", tt.name, tt.got, tt.want)
		}
	}
}
