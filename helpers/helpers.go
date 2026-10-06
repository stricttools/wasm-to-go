package helpers

import (
	"encoding/binary"
	"math"
	"math/bits"
	"sync/atomic"
)

// Prevent constant folding/propagation,
// ensuring code using them compiles
// and overflows/panics at runtime.

//go:nosplit
func i32(x int32) int32 { return x }

//go:nosplit
func i64(x int64) int64 { return x }

// Detect signed integer overflow.
// Folded away for constant y.
// They generate sub-optimal code on Intel.

//go:nosplit
func i32_div_s(x, y int32) int32 {
	if y == -1 && x == math.MinInt32 {
		panic("integer overflow")
	}
	return x / y
}

//go:nosplit
func i64_div_s(x, y int64) int64 {
	if y == -1 && x == math.MinInt64 {
		panic("integer overflow")
	}
	return x / y
}

//go:nosplit
func i32_neg_s(x int32) int32 {
	if x == math.MinInt32 {
		panic("integer overflow")
	}
	return -x
}

//go:nosplit
func i64_neg_s(x int64) int64 {
	if x == math.MinInt64 {
		panic("integer overflow")
	}
	return -x
}

// Needed for correct y wrap around behavior.
// Folded away for constant y.

//go:nosplit
func i32_shl(x, y int32) int32 {
	return x << (y & 31)
}

//go:nosplit
func i32_shr_s(x, y int32) int32 {
	return x >> (y & 31)
}

//go:nosplit
func i32_shr_u(x, y int32) int32 {
	return int32(uint32(x) >> (y & 31))
}

//go:nosplit
func i64_shl(x, y int64) int64 {
	return x << (y & 63)
}

//go:nosplit
func i64_shr_s(x, y int64) int64 {
	return x >> (y & 63)
}

//go:nosplit
func i64_shr_u(x, y int64) int64 {
	return int64(uint64(x) >> (y & 63))
}

//go:nosplit
func i32_rotl(x, y int32) int32 {
	return int32(bits.RotateLeft32(uint32(x), int(y)))
}

//go:nosplit
func i32_rotr(x, y int32) int32 {
	return int32(bits.RotateLeft32(uint32(x), -int(y)))
}

//go:nosplit
func i64_rotl(x, y int64) int64 {
	return int64(bits.RotateLeft64(uint64(x), int(y)))
}

//go:nosplit
func i64_rotr(x, y int64) int64 {
	return int64(bits.RotateLeft64(uint64(x), -int(y)))
}

// Must be implemented as bitwise operations: abs, neg, and copysign
// never change a NaN's payload (not even on CPUs whose own absolute value
// and negation instructions are arithmetic, like legacy MIPS).

//go:nosplit
func f32_abs(x float32) float32 {
	return math.Float32frombits(math.Float32bits(x) &^ (1 << 31))
}

//go:nosplit
func f64_abs(x float64) float64 {
	return math.Float64frombits(math.Float64bits(x) &^ (1 << 63))
}

//go:nosplit
func f32_neg(x float32) float32 {
	return math.Float32frombits(math.Float32bits(x) ^ (1 << 31))
}

//go:nosplit
func f64_neg(x float64) float64 {
	return math.Float64frombits(math.Float64bits(x) ^ (1 << 63))
}

//go:nosplit
func f32_copysign(x, y float32) float32 {
	return math.Float32frombits(math.Float32bits(x)&^(1<<31) | math.Float32bits(y)&(1<<31))
}

// The WebAssembly deterministic profile: every operation other than abs,
// neg, copysign, and the reinterpretations returns the positive canonical
// NaN whenever its result is a NaN, whatever NaN the CPU produced.

//go:nosplit
func f32_canon(x float32) float32 {
	if x != x {
		return math.Float32frombits(0x7fc00000)
	}
	return x
}

//go:nosplit
func f64_canon(x float64) float64 {
	if x != x {
		return math.Float64frombits(0x7ff8000000000000)
	}
	return x
}

// Return the positive canonical NaN when either operand is a NaN
// (the builtins return an operand or a CPU-chosen NaN);
// they order -0 below +0, as the builtins and WebAssembly do.

//go:nosplit
func f32_min(x, y float32) float32 {
	if m := min(x, y); m == m {
		return m
	}
	return math.Float32frombits(0x7fc00000)
}

//go:nosplit
func f32_max(x, y float32) float32 {
	if m := max(x, y); m == m {
		return m
	}
	return math.Float32frombits(0x7fc00000)
}

//go:nosplit
func f64_min(x, y float64) float64 {
	if m := min(x, y); m == m {
		return m
	}
	return math.Float64frombits(0x7ff8000000000000)
}

//go:nosplit
func f64_max(x, y float64) float64 {
	if m := max(x, y); m == m {
		return m
	}
	return math.Float64frombits(0x7ff8000000000000)
}

// Int to float32 conversions of 64-bit integers round once, to nearest
// even. Go's float32(int64) and float32(uint64) round twice (through
// float64) on some 32-bit platforms, which is off by one ulp for inputs
// like 2^47+2^23+1. Here float64 of an integer below 2^53 is exact, and so
// is scaling by a power of two, so float32() is the only rounding; the bits
// shifted out are kept as a sticky bit in y's lowest bit, below float32's
// rounding position.

//go:nosplit
func f32_convert_i64_u(x int64) float32 {
	u := uint64(x)
	if u < 1<<53 {
		return float32(float64(int64(u)))
	}
	s := uint(bits.Len64(u) - 53)
	y := u >> s
	if u&(1<<s-1) != 0 {
		y |= 1
	}
	return float32(float64(int64(y)) * math.Float64frombits(uint64(1023+s)<<52))
}

//go:nosplit
func f32_convert_i64_s(x int64) float32 {
	if x < 0 {
		// -x wraps for math.MinInt64, whose magnitude as a uint64 is right.
		return -f32_convert_i64_u(-x)
	}
	return f32_convert_i64_u(x)
}

// Float to int conversions.

// None converts a float to an integer with Go's conversion, whose result
// where the Go specification leaves it undefined (NaN, an infinity, a value
// out of the integer type's range) differs by CPU and by compiler: each
// checks or clamps its operand and then assembles the integer from the
// operand's bits (trunc_small for the i32 results, trunc_u64 for the i64
// ones), which strictgo accepts as it is.

// All i64 conversions use >= because both MaxInt64 and MaxUint64
// round up when converted to a float64.

// Returns the truncation of f toward zero as a 64-bit two's complement
// integer, for every f whose magnitude is below 2^64 (the caller has
// checked it). The significand, its implicit one restored, is aligned to
// the top of a uint64, where it stands for the magnitude scaled into
// [2^63, 2^64), and shifted right by 63 less the exponent; a magnitude below
// one (zero and the subnormals included) shifts by 64 or more, which Go
// defines to yield zero. It is how strictgo's reproducible.Trunc computes.
//
//go:nosplit
func trunc_u64(f float64) uint64 {
	b := math.Float64bits(f)
	u := (b<<11 | 1<<63) >> (1086 - b>>52&0x7ff)
	sign := uint64(int64(b) >> 63) // all ones for a negative f
	return u ^ sign - sign
}

// Returns x, an integer of magnitude below 2^51 (the caller has checked
// it), as an int64. Adding 1.5 * 2^52 is exact for such an x, and leaves it
// in the significand's low bits, offset by the constant's own bits: two
// instructions beside the addition, fewer than a shift by the exponent.
//
//go:nosplit
func trunc_small(x float64) int64 {
	return int64(math.Float64bits(x+0x1.8p52)) - 0x4338000000000000
}

//go:nosplit
func i32_trunc_f64_s(f float64) int32 {
	x := math.Trunc(f)
	switch {
	case f != f:
		panic("invalid conversion to integer")
	case x < math.MinInt32 || x > math.MaxInt32:
		panic("integer overflow")
	}
	return int32(trunc_small(x))
}

//go:nosplit
func i32_trunc_f32_s(f float32) int32 {
	x := math.Trunc(float64(f))
	switch {
	case f != f:
		panic("invalid conversion to integer")
	case x < math.MinInt32 || x > math.MaxInt32:
		panic("integer overflow")
	}
	return int32(trunc_small(x))
}

//go:nosplit
func i32_trunc_f64_u(f float64) int32 {
	x := math.Trunc(f)
	switch {
	case f != f:
		panic("invalid conversion to integer")
	case x < 0 || x > math.MaxUint32:
		panic("integer overflow")
	}
	return int32(trunc_small(x))
}

//go:nosplit
func i32_trunc_f32_u(f float32) int32 {
	x := math.Trunc(float64(f))
	switch {
	case f != f:
		panic("invalid conversion to integer")
	case x < 0 || x > math.MaxUint32:
		panic("integer overflow")
	}
	return int32(trunc_small(x))
}

//go:nosplit
func i64_trunc_f64_s(f float64) int64 {
	x := math.Trunc(f)
	switch {
	case f != f:
		panic("invalid conversion to integer")
	case x < math.MinInt64 || x >= math.MaxInt64:
		panic("integer overflow")
	}
	return int64(trunc_u64(f))
}

//go:nosplit
func i64_trunc_f32_s(f float32) int64 {
	x := math.Trunc(float64(f))
	switch {
	case f != f:
		panic("invalid conversion to integer")
	case x < math.MinInt64 || x >= math.MaxInt64:
		panic("integer overflow")
	}
	return int64(trunc_u64(float64(f)))
}

//go:nosplit
func i64_trunc_f64_u(f float64) int64 {
	x := math.Trunc(f)
	switch {
	case f != f:
		panic("invalid conversion to integer")
	case x < 0 || x >= math.MaxUint64:
		panic("integer overflow")
	}
	return int64(trunc_u64(f))
}

//go:nosplit
func i64_trunc_f32_u(f float32) int64 {
	x := math.Trunc(float64(f))
	switch {
	case f != f:
		panic("invalid conversion to integer")
	case x < 0 || x >= math.MaxUint64:
		panic("integer overflow")
	}
	return int64(trunc_u64(float64(f)))
}

//go:nosplit
func i32_trunc_sat_f64_s(f float64) int32 {
	switch {
	case f <= math.MinInt32:
		return math.MinInt32
	case f >= math.MaxInt32:
		return math.MaxInt32
	case f != f:
		return 0
	}
	return int32(trunc_small(math.Trunc(f)))
}

//go:nosplit
func i32_trunc_sat_f32_s(f float32) int32 {
	switch {
	case f <= math.MinInt32:
		return math.MinInt32
	case f >= math.MaxInt32:
		return math.MaxInt32
	case f != f:
		return 0
	}
	return int32(trunc_small(math.Trunc(float64(f))))
}

//go:nosplit
func i32_trunc_sat_f64_u(f float64) int32 {
	switch {
	case f <= 0 || f != f:
		return 0
	case f >= math.MaxUint32:
		return -1
	}
	return int32(trunc_small(math.Trunc(f)))
}

//go:nosplit
func i32_trunc_sat_f32_u(f float32) int32 {
	switch {
	case f <= 0 || f != f:
		return 0
	case f >= math.MaxUint32:
		return -1
	}
	return int32(trunc_small(math.Trunc(float64(f))))
}

//go:nosplit
func i64_trunc_sat_f64_s(f float64) int64 {
	switch {
	case f < math.MinInt64:
		return math.MinInt64
	case f >= math.MaxInt64:
		return math.MaxInt64
	case f != f:
		return 0
	}
	return int64(trunc_u64(f))
}

//go:nosplit
func i64_trunc_sat_f32_s(f float32) int64 {
	switch {
	case f < math.MinInt64:
		return math.MinInt64
	case f >= math.MaxInt64:
		return math.MaxInt64
	case f != f:
		return 0
	}
	return int64(trunc_u64(float64(f)))
}

//go:nosplit
func i64_trunc_sat_f64_u(f float64) int64 {
	switch {
	case f <= 0 || f != f:
		return 0
	case f >= math.MaxUint64:
		return -1
	}
	return int64(trunc_u64(f))
}

//go:nosplit
func i64_trunc_sat_f32_u(f float32) int64 {
	switch {
	case f <= 0 || f != f:
		return 0
	case f >= math.MaxUint64:
		return -1
	}
	return int64(trunc_u64(float64(f)))
}

// Wide Arithmetic.

//go:nosplit
func i64_add128(xl, xh, yl, yh int64) (int64, int64) {
	lo, carry := bits.Add64(uint64(xl), uint64(yl), 0)
	hi, _ := bits.Add64(uint64(xh), uint64(yh), carry)
	return int64(lo), int64(hi)
}

//go:nosplit
func i64_sub128(xl, xh, yl, yh int64) (int64, int64) {
	lo, borrow := bits.Sub64(uint64(xl), uint64(yl), 0)
	hi, _ := bits.Sub64(uint64(xh), uint64(yh), borrow)
	return int64(lo), int64(hi)
}

//go:nosplit
func i64_add_wide(x, y int64) (int64, int64) {
	lo, carry := bits.Add64(uint64(x), uint64(y), 0)
	return int64(lo), int64(carry)
}

//go:nosplit
func i64_sub_wide(x, y int64) (int64, int64) {
	lo, borrow := bits.Sub64(uint64(x), uint64(y), 0)
	return int64(lo), int64(-borrow)
}

//go:nosplit
func i64_mul_wide_u(x, y int64) (int64, int64) {
	hi, lo := bits.Mul64(uint64(x), uint64(y))
	return int64(lo), int64(hi)
}

//go:nosplit
func i64_mul_wide_s(x, y int64) (int64, int64) {
	hi, lo := bits.Mul64(uint64(x), uint64(y))
	if x < 0 {
		hi -= uint64(y)
	}
	if y < 0 {
		hi -= uint64(x)
	}
	return int64(lo), int64(hi)
}

// Multi-byte loads/stores.

//go:nosplit
func load16[T uint32 | uint64](mem []byte, addr T) uint16 {
	return binary.LittleEndian.Uint16(mem[addr:])
}

//go:nosplit
func store16[T uint32 | uint64](mem []byte, addr T, val uint16) {
	binary.LittleEndian.PutUint16(mem[addr:], val)
}

//go:nosplit
func load32[T uint32 | uint64](mem []byte, addr T) uint32 {
	return binary.LittleEndian.Uint32(mem[addr:])
}

//go:nosplit
func store32[T uint32 | uint64](mem []byte, addr T, val uint32) {
	binary.LittleEndian.PutUint32(mem[addr:], val)
}

//go:nosplit
func load64[T uint32 | uint64](mem []byte, addr T) uint64 {
	return binary.LittleEndian.Uint64(mem[addr:])
}

//go:nosplit
func store64[T uint32 | uint64](mem []byte, addr T, val uint64) {
	binary.LittleEndian.PutUint64(mem[addr:], val)
}

// Bulk memory operations.

// Grows the memory *mem by delta pages, up to max, and returns its old size
// in pages, or -1. *back is the memory's backing array: *mem is its prefix,
// with its capacity cut to its length, so no slice of the memory reaches
// past its end, and back's bytes past the memory's end are zero. Growth
// within back is in place: no copy, no allocation. Past it, the memory moves
// to a new backing array of twice the size, at least what the growth needs,
// at most max pages.
func memory_grow(mem, back *[]byte, delta, max int64) int64 {
	buf := *mem
	len := len(buf)
	old := len >> 16
	if delta == 0 {
		return int64(old)
	}
	max = int64(min(uint64(max), math.MaxInt>>16))
	new, c := bits.Add64(uint64(old), uint64(delta), 0)
	if c != 0 || new > uint64(max) {
		return -1
	}
	size := int(new << 16)
	if size > cap(*back) {
		n := int(min(uint64(cap(*back))>>15, uint64(max)) << 16)
		if n < size {
			n = size
		}
		b := make([]byte, n)
		copy(b, buf)
		*back = b
	}
	*mem = (*back)[:size:size]
	return int64(old)
}

// The bulk memory operations slice the memory up to its length, not its
// capacity, so an operation past the end traps even where the slice's
// capacity is larger (an imported memory's, a shared memory's).

func memory_init[T1, T2 int | uint32 | uint64](mem []byte, data string, dest T1, src, n T2) {
	mem = mem[:len(mem):len(mem)]
	x := uint64(dest)
	z := uint64(src)
	y := x + uint64(n)
	w := z + uint64(n)
	copy(mem[x:y], data[z:w])
}

func memory_copy[T uint32 | uint64](mem []byte, dest, src, n T) {
	mem = mem[:len(mem):len(mem)]
	x := uint64(dest)
	z := uint64(src)
	y := x + uint64(n)
	w := z + uint64(n)
	copy(mem[x:y], mem[z:w])
}

func memory_fill[T uint32 | uint64](mem []byte, dest T, val int32, n T) {
	mem = mem[:len(mem):len(mem)]
	x := uint64(dest)
	y := x + uint64(n)
	buf := mem[x:y]
	if len(buf) > 0 {
		buf[0] = byte(val)
		for i := 1; i < len(buf); {
			chunk := min(i, 8192)
			i += copy(buf[i:], buf[:chunk])
		}
	}
}

func memory_zero[T uint32 | uint64](mem []byte, dest, n T) {
	mem = mem[:len(mem):len(mem)]
	x := uint64(dest)
	y := x + uint64(n)
	clear(mem[x:y])
}

func table_init[T1, T2, T3 int | int32 | int64](tab, elems []any, dest T1, src T2, n T3) {
	x := uint64(dest)
	z := uint64(src)
	y := x + uint64(n)
	w := z + uint64(n)
	copy(tab[x:y], elems[z:w])
}

func table_copy[T1, T2, T3 int32 | int64](dst, tab []any, dest T1, src T2, n T3) {
	x := uint64(dest)
	z := uint64(src)
	y := x + uint64(n)
	w := z + uint64(n)
	copy(dst[x:y], tab[z:w])
}

func table_fill[T int32 | int64](tab []any, dest T, val any, n T) {
	x := uint64(dest)
	y := x + uint64(n)
	buf := tab[x:y]
	if val == nil {
		clear(buf)
		return
	}
	for i := range buf {
		buf[i] = val
	}
}

func table_grow[T int32 | int64](tab *[]any, val any, delta, max T) T {
	buf := *tab
	old := len(buf)
	if delta == 0 {
		return T(old)
	}
	new, c := bits.Add64(uint64(old), uint64(delta), 0)
	if c != 0 || new > uint64(max) {
		return -1
	}
	buf = append(buf, make([]any, delta)...)
	if val != nil {
		cpy := buf[old:]
		for i := range cpy {
			cpy[i] = val
		}
	}
	*tab = buf
	return T(old)
}

//go:nosplit
func atomic_fence() {
	var b atomic.Bool
	b.Swap(true)
}

// The lanes of SIMD operations written lane by lane (the passes package's
// portable SIMD code): a comparison's mask, a test's result, saturation,
// canonicalization of a float lane's bits, pseudo-minimum and -maximum,
// and a byte of a vector's words by a dynamic index.

// Prevents constant folding, as i32 does.
//
//go:nosplit
func lane_u32(x uint32) uint32 { return x }

//go:nosplit
func lane_mask8(c bool) uint8 {
	if c {
		return 0xff
	}
	return 0
}

//go:nosplit
func lane_mask16(c bool) uint16 {
	if c {
		return 0xffff
	}
	return 0
}

//go:nosplit
func lane_mask32(c bool) uint32 {
	if c {
		return 0xffffffff
	}
	return 0
}

//go:nosplit
func lane_mask64(c bool) uint64 {
	if c {
		return 0xffffffffffffffff
	}
	return 0
}

//go:nosplit
func lane_bool(c bool) int32 {
	if c {
		return 1
	}
	return 0
}

//go:nosplit
func lane_sat_s8(v int64) uint8 { return uint8(int8(min(max(v, -128), 127))) }

//go:nosplit
func lane_sat_u8(v int64) uint8 { return uint8(min(max(v, 0), 255)) }

//go:nosplit
func lane_sat_s16(v int64) uint16 { return uint16(int16(min(max(v, -32768), 32767))) }

//go:nosplit
func lane_sat_u16(v int64) uint16 { return uint16(min(max(v, 0), 65535)) }

// The bits of a float32 lane, with a NaN made the positive canonical NaN,
// without a branch: the mask is all ones exactly when the magnitude is
// above the infinity's.
//
//go:nosplit
func lane_canon32(b uint32) uint32 {
	return b ^ (b^0x7fc00000)&-(((b&0x7fffffff)+0x7fffff)>>31)
}

//go:nosplit
func lane_canon64(b uint64) uint64 {
	return b ^ (b^0x7ff8000000000000)&-(((b&0x7fffffffffffffff)+0xfffffffffffff)>>63)
}

// pmin and pmax return one of their operands, bit for bit:
// b < a ? b : a, and a < b ? b : a.
//
//go:nosplit
func lane_pmin32(a, b uint32) uint32 {
	if math.Float32frombits(b) < math.Float32frombits(a) {
		return b
	}
	return a
}

//go:nosplit
func lane_pmax32(a, b uint32) uint32 {
	if math.Float32frombits(a) < math.Float32frombits(b) {
		return b
	}
	return a
}

//go:nosplit
func lane_pmin64(a, b uint64) uint64 {
	if math.Float64frombits(b) < math.Float64frombits(a) {
		return b
	}
	return a
}

//go:nosplit
func lane_pmax64(a, b uint64) uint64 {
	if math.Float64frombits(a) < math.Float64frombits(b) {
		return b
	}
	return a
}

// The same, of lanes held as floats: moves keep their bits.
//
//go:nosplit
func lane_pminf32(a, b float32) float32 {
	if b < a {
		return b
	}
	return a
}

//go:nosplit
func lane_pmaxf32(a, b float32) float32 {
	if a < b {
		return b
	}
	return a
}

//go:nosplit
func lane_pminf64(a, b float64) float64 {
	if b < a {
		return b
	}
	return a
}

//go:nosplit
func lane_pmaxf64(a, b float64) float64 {
	if a < b {
		return b
	}
	return a
}

// The byte i of the vector of words w0 to w3, or 0 if i is 16 or more
// (i8x16.swizzle).
//
//go:nosplit
func lane_byte(w0, w1, w2, w3 uint32, i uint8) uint8 {
	w := w0
	switch i >> 2 {
	case 0:
	case 1:
		w = w1
	case 2:
		w = w2
	case 3:
		w = w3
	default:
		return 0
	}
	return uint8(w >> (8 * (i & 3)))
}
