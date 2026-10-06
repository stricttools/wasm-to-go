package wasm2go

// The cases of the determinism test (determinism.wat translated to
// determinism.go, and expected.txt): every operation is an export opN
// taking two raw-bit operands and returning raw bits, so no value passes
// through a Go float operation outside the translated code.

import (
	"fmt"
	"math"
	"reflect"
)

// ops lists the exports in order: ops[n] is export opN. The prefix names
// the operand type: a float type, or i32/i64 for conversions from integers.
var ops = []struct{ name, operands string }{
	{"f64.nearest", "f64"}, {"f64.floor", "f64"}, {"f64.ceil", "f64"}, {"f64.trunc", "f64"},
	{"f64.sqrt", "f64"}, {"f64.abs", "f64"}, {"f64.neg", "f64"},
	{"f32.nearest", "f32"}, {"f32.floor", "f32"}, {"f32.ceil", "f32"}, {"f32.trunc", "f32"},
	{"f32.sqrt", "f32"}, {"f32.abs", "f32"}, {"f32.neg", "f32"},
	{"f32.demote_f64", "f64"}, {"f64.promote_f32", "f32"},
	{"f64.add", "f64 f64"}, {"f64.sub", "f64 f64"}, {"f64.mul", "f64 f64"}, {"f64.div", "f64 f64"},
	{"f64.min", "f64 f64"}, {"f64.max", "f64 f64"}, {"f64.copysign", "f64 f64"},
	{"f32.add", "f32 f32"}, {"f32.sub", "f32 f32"}, {"f32.mul", "f32 f32"}, {"f32.div", "f32 f32"},
	{"f32.min", "f32 f32"}, {"f32.max", "f32 f32"}, {"f32.copysign", "f32 f32"},
	{"i32.trunc_f32_s", "f32"}, {"i32.trunc_f32_u", "f32"}, {"i32.trunc_f64_s", "f64"}, {"i32.trunc_f64_u", "f64"},
	{"i64.trunc_f32_s", "f32"}, {"i64.trunc_f32_u", "f32"}, {"i64.trunc_f64_s", "f64"}, {"i64.trunc_f64_u", "f64"},
	{"i32.trunc_sat_f32_s", "f32"}, {"i32.trunc_sat_f32_u", "f32"}, {"i32.trunc_sat_f64_s", "f64"}, {"i32.trunc_sat_f64_u", "f64"},
	{"i64.trunc_sat_f32_s", "f32"}, {"i64.trunc_sat_f32_u", "f32"}, {"i64.trunc_sat_f64_s", "f64"}, {"i64.trunc_sat_f64_u", "f64"},
	{"f32.convert_i32_s", "int"}, {"f32.convert_i32_u", "int"}, {"f32.convert_i64_s", "int"}, {"f32.convert_i64_u", "int"},
	{"f64.convert_i32_s", "int"}, {"f64.convert_i32_u", "int"}, {"f64.convert_i64_s", "int"}, {"f64.convert_i64_u", "int"},
	{"f64.chain_mul_add", "f64 f64"}, {"f64.chain_mul_sub", "f64 f64"}, {"f64.chain_neg_mul_add", "f64 f64"},
	{"f64.chain_div_sqrt", "f64 f64"}, {"f64.chain_min_add", "f64 f64"}, {"f64.chain_add_neg", "f64 f64"},
	{"f64.chain_add_abs", "f64 f64"}, {"f64.chain_add_copysign", "f64 f64"}, {"f64.chain_add_floor", "f64 f64"},
	{"f32.chain_mul_add", "f32 f32"}, {"f32.chain_mul_sub", "f32 f32"}, {"f32.chain_add_neg", "f32 f32"},
	{"f32.chain_add_abs", "f32 f32"}, {"f32.chain_demote_add", "f64 f64"}, {"f64.chain_promote_add", "f32 f32"},
	{"f64.local_add_neg", "f64 f64"},
	{"f64.local_add_abs", "f64 f64"},
	{"f64.local_add_copysign", "f64 f64"},
	{"f64.local_add_reinterpret", "f64 f64"},
	{"f64.local_add_store", "f64 f64"},
	{"f64.local_add_call", "f64 f64"},
	{"f64.local_add_global", "f64 f64"},
	{"f64.local_add_return", "f64 f64"},
	{"f64.local_mul_local_add", "f64 f64"},
	{"f64.local_add_lt", "f64 f64"},
	{"f64.local_add_ne_self", "f64 f64"},
	{"f64.local_add_min", "f64 f64"},
	{"f64.local_add_trunc_sat", "f64 f64"},
	{"f64.local_add_sqrt_neg", "f64 f64"},
	{"f64.local_select_neg", "f64 f64"},
	{"f64.local_loop_neg", "f64 f64"},
	{"f32.local_add_neg", "f32 f32"},
	{"f32.local_add_copysign", "f32 f32"},
	{"f32.local_add_store", "f32 f32"},
	{"f32.local_add_reinterpret", "f32 f32"},
	{"f32.local_add_promote_neg", "f32 f32"},
	{"f32.local_mul_local_add_neg", "f32 f32"},
}

// Unary operands: signed zeros, halves and rounding ties, integer and
// float32 range boundaries, extremes, subnormals, infinities, and NaNs of
// every kind (canonical, arithmetic with a payload, signaling), both signs.
var f64s = []uint64{
	0x0000000000000000, 0x8000000000000000, // +0, -0
	0x3fe0000000000000, 0xbfe0000000000000, // 0.5, -0.5
	0x3fdfffffffffffff, 0xbfeccccccccccccd, // 0.49999999999999994, -0.9
	0x3ff8000000000000, 0x4004000000000000, 0xc004000000000000, // 1.5, 2.5, -2.5
	0x4330000000000001, 0xc330000000000001, // 2^52+1, -(2^52+1)
	0x432fffffffffffff,                                         // 2^52-0.5
	0xbff0000000000000,                                         // -1
	0x41dfffffffe00000, 0x41dfffffffc00000, 0x41e0000000000000, // 2^31-0.5, 2^31-1, 2^31
	0xc1e0000000100000, 0xc1e0000000000000, 0xc1e0000000200000, // -2^31-0.5, -2^31, -2^31-1
	0x41efffffffe00000, 0x41efffffffffffff, 0x41f0000000000000, // 2^32-1, 2^32-epsilon, 2^32
	0x43dfffffffffffff, 0x43e0000000000000, 0xc3e0000000000000, 0xc3e0000000000001, // 2^63-1024, 2^63, -2^63, -2^63-2048
	0x43efffffffffffff, 0x43f0000000000000, // 2^64-2048, 2^64
	0x7fefffffffffffff, 0xffefffffffffffff, 0x0000000000000001, 0x8000000000000001, // max, -max, min subnormal, -min subnormal
	0x47efffffe0000000, 0x47efffffefffffff, 0x47efffff00000000, 0x36a0000000000000, 0x3690000000000001, // float32 demotion bounds
	0x7ff0000000000000, 0xfff0000000000000, // +inf, -inf
	0x7ff8000000000000, 0xfff8000000000000, // canonical NaN, -canonical NaN
	0x7ff8000000000001, 0xfffc000000000123, // arithmetic NaNs with a payload
	0x7ff0000000000001, 0xfff4000000000000, // signaling NaNs
}

var f32s = []uint32{
	0x00000000, 0x80000000, 0x3f000000, 0xbf000000, 0x3effffff, 0xbf666666,
	0x3fc00000, 0x40200000, 0xc0200000, 0x4b000001, 0xcb000001, 0xbf800000,
	0x4effffff, 0x4f000000, 0xcf000000, 0xcf000001, 0x4f7fffff, 0x4f800000,
	0x5effffff, 0x5f000000, 0xdf000000, 0xdf000001, 0x5f7fffff, 0x5f800000,
	0x7f7fffff, 0xff7fffff, 0x00000001, 0x80000001,
	0x7f800000, 0xff800000, 0x7fc00000, 0xffc00000, 0x7fc00001, 0xffe00123,
	0x7f800001, 0xffa00000,
}

// Binary operands (every pair is tested): signed zeros and ones,
// infinities, NaNs of every kind, and, for the chains, the factors
// 1+2^-30 and 1-2^-30 (1+2^-13 and 1-2^-13 in float32), whose product
// rounds to 1 but whose fused multiply-add with -1 does not give 0.
var f64bin = []uint64{0, 0x8000000000000000, 0x3ff0000000000000, 0xbff0000000000000, 0x7ff0000000000000, 0xfff0000000000000,
	0x7ff8000000000000, 0xfff8000000000000, 0x7ff8000000000001, 0xfffc000000000123, 0x7ff0000000000001, 0xfff4000000000000,
	math.Float64bits(1 + 0x1p-30), math.Float64bits(1 - 0x1p-30), 0x7fefffffffffffff, 0x0000000000000001}
var f32bin = []uint32{0, 0x80000000, 0x3f800000, 0xbf800000, 0x7f800000, 0xff800000,
	0x7fc00000, 0xffc00000, 0x7fc00001, 0xffe00123, 0x7f800001, 0xffa00000,
	math.Float32bits(1 + 0x1p-13), math.Float32bits(1 - 0x1p-13), 0x7f7fffff, 0x00000001}

// Integer operands for conversions to float: rounding ties and sticky bits
// at the float32 and float64 precisions, and the extremes.
var ints = []uint64{0, 1, 0xffffffffffffffff, 0x7fffffff, 0x80000000, 0xffffffff, 0x1000001, 0x1000003, 0x7fffffffffffffff,
	0x8000000000000000, 0x8000008000000001, 0x8000008000000000, 0x20000000000001, 0x20000000000003, 0xfffffffffffff801,
	0x7ffffffffffffdff, 0x7ffffffffffffe00, 0xffffff7fffffffff, 0xffffff8000000000, 0x800000800001, 0xc0000081}

// A Case is one call: export Export (named Name in expected.txt) applied
// to the raw bits A and B.
type Case struct {
	Name, Export string
	A, B         uint64
}

// Cases lists every call, in the order of expected.txt.
func Cases() []Case {
	var cs []Case
	for n, op := range ops {
		for _, c := range operands(op.operands) {
			cs = append(cs, Case{op.name, fmt.Sprintf("op%d", n), c[0], c[1]})
		}
	}
	return cs
}

// Results makes every call and returns one line per call,
// "name a b -> result" with the operands and the result in hex,
// or "trap: message" when the call trapped.
func Results() []string { return ResultsOf(New()) }

// ResultsOf is Results for module, a *Module of this translation of
// determinism.wasm or of another.
func ResultsOf(module any) []string {
	m := reflect.ValueOf(module)
	var lines []string
	for _, c := range Cases() {
		f := m.MethodByName("X" + c.Export)
		lines = append(lines, fmt.Sprintf("%s %#x %#x -> %s", c.Name, c.A, c.B, call(f, c.A, c.B)))
	}
	return lines
}

func operands(kind string) (cs [][2]uint64) {
	switch kind {
	case "f64":
		for _, a := range f64s {
			cs = append(cs, [2]uint64{a, 0})
		}
	case "f32":
		for _, a := range f32s {
			cs = append(cs, [2]uint64{uint64(a), 0})
		}
	case "int":
		for _, a := range ints {
			cs = append(cs, [2]uint64{a, 0})
		}
	case "f64 f64":
		for _, a := range f64bin {
			for _, b := range f64bin {
				cs = append(cs, [2]uint64{a, b})
			}
		}
	case "f32 f32":
		for _, a := range f32bin {
			for _, b := range f32bin {
				cs = append(cs, [2]uint64{uint64(a), uint64(b)})
			}
		}
	default:
		panic("unknown operand kind " + kind)
	}
	return cs
}

func call(f reflect.Value, a, b uint64) (s string) {
	defer func() {
		if r := recover(); r != nil {
			s = fmt.Sprint("trap: ", r)
		}
	}()
	r := f.Call([]reflect.Value{reflect.ValueOf(int64(a)), reflect.ValueOf(int64(b))})
	return fmt.Sprintf("%#x", uint64(r[0].Int()))
}
