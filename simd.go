package main

import (
	"encoding/binary"
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
	"strings"

	"github.com/stricttools/wasm-to-go/internal/passes"
)

// The fixed-width SIMD instructions (the 0xFD prefix) translate to calls of
// the operations of the passes package's SIMD form, simd_f32x4_add(x, y)
// for f32x4.add: names no Go code declares, which passes.LowerSIMD rewrites
// into each target's code when a file is written (inline scalar code, or
// direct calls of simd/archsimd's methods). Every operand of one is a
// variable or a constant, and every result a variable of its own, so the
// rewritten code can repeat an operand at each lane.

// The form of a SIMD instruction: its immediates, operands, and result.
type simdForm int

const (
	simdUnary     simdForm = iota // v128 → v128
	simdBinary                    // v128 v128 → v128
	simdTernary                   // v128 v128 v128 → v128 (bitselect)
	simdTest                      // v128 → i32
	simdShift                     // v128 i32 → v128
	simdSplat                     // scalar → v128
	simdExtract                   // v128 → scalar, with a lane
	simdReplace                   // v128 scalar → v128, with a lane
	simdLoad                      // address → v128, with a memarg
	simdStore                     // address v128 →, with a memarg
	simdLoadLane                  // address v128 → v128, with a memarg and a lane
	simdStoreLane                 // address v128 →, with a memarg and a lane
	simdConst                     // → v128, with 16 bytes
	simdShuffle                   // v128 v128 → v128, with 16 lane indexes
)

type simdOp struct {
	name   string
	form   simdForm
	scalar wasmType // the scalar operand or result of splat, extract, and replace
	canon  string   // the lane shape to canonicalize the result in (f32x4 or f64x2), or ""
}

var simdOps = map[uint64]simdOp{}

func init() {
	add := func(code uint64, name string, form simdForm) *simdOp {
		simdOps[code] = simdOp{name: name, form: form}
		op := simdOps[code]
		return &op
	}
	set := func(code uint64, op *simdOp) { simdOps[code] = *op }

	for code, name := range []string{"v128.load", "v128.load8x8_s", "v128.load8x8_u", "v128.load16x4_s", "v128.load16x4_u",
		"v128.load32x2_s", "v128.load32x2_u", "v128.load8_splat", "v128.load16_splat", "v128.load32_splat", "v128.load64_splat"} {
		add(uint64(code), name, simdLoad)
	}
	add(0x0b, "v128.store", simdStore)
	add(0x0c, "v128.const", simdConst)
	add(0x0d, "i8x16.shuffle", simdShuffle)
	add(0x0e, "i8x16.swizzle", simdBinary)
	for i, s := range []struct {
		shape string
		t     wasmType
	}{{"i8x16", i32}, {"i16x8", i32}, {"i32x4", i32}, {"i64x2", i64}, {"f32x4", f32}, {"f64x2", f64}} {
		op := add(uint64(0x0f+i), s.shape+".splat", simdSplat)
		op.scalar = s.t
		set(uint64(0x0f+i), op)
	}
	lanes := []struct {
		code      uint64
		name      string
		form      simdForm
		scalar    wasmType
	}{
		{0x15, "i8x16.extract_lane_s", simdExtract, i32}, {0x16, "i8x16.extract_lane_u", simdExtract, i32}, {0x17, "i8x16.replace_lane", simdReplace, i32},
		{0x18, "i16x8.extract_lane_s", simdExtract, i32}, {0x19, "i16x8.extract_lane_u", simdExtract, i32}, {0x1a, "i16x8.replace_lane", simdReplace, i32},
		{0x1b, "i32x4.extract_lane", simdExtract, i32}, {0x1c, "i32x4.replace_lane", simdReplace, i32},
		{0x1d, "i64x2.extract_lane", simdExtract, i64}, {0x1e, "i64x2.replace_lane", simdReplace, i64},
		{0x1f, "f32x4.extract_lane", simdExtract, f32}, {0x20, "f32x4.replace_lane", simdReplace, f32},
		{0x21, "f64x2.extract_lane", simdExtract, f64}, {0x22, "f64x2.replace_lane", simdReplace, f64},
	}
	for _, l := range lanes {
		op := add(l.code, l.name, l.form)
		op.scalar = l.scalar
		set(l.code, op)
	}
	code := uint64(0x23)
	for _, shape := range []string{"i8x16", "i16x8", "i32x4"} {
		for _, cmp := range []string{"eq", "ne", "lt_s", "lt_u", "gt_s", "gt_u", "le_s", "le_u", "ge_s", "ge_u"} {
			add(code, shape+"."+cmp, simdBinary)
			code++
		}
	}
	for _, shape := range []string{"f32x4", "f64x2"} {
		for _, cmp := range []string{"eq", "ne", "lt", "gt", "le", "ge"} {
			add(code, shape+"."+cmp, simdBinary)
			code++
		}
	}
	add(0x4d, "v128.not", simdUnary)
	add(0x4e, "v128.and", simdBinary)
	add(0x4f, "v128.andnot", simdBinary)
	add(0x50, "v128.or", simdBinary)
	add(0x51, "v128.xor", simdBinary)
	add(0x52, "v128.bitselect", simdTernary)
	add(0x53, "v128.any_true", simdTest)
	for i, n := range []string{"8", "16", "32", "64"} {
		add(uint64(0x54+i), "v128.load"+n+"_lane", simdLoadLane)
		add(uint64(0x58+i), "v128.store"+n+"_lane", simdStoreLane)
	}
	add(0x5c, "v128.load32_zero", simdLoad)
	add(0x5d, "v128.load64_zero", simdLoad)
	canon := func(code uint64, name string, form simdForm, shape string) {
		op := add(code, name, form)
		op.canon = shape
		set(code, op)
	}
	canon(0x5e, "f32x4.demote_f64x2_zero", simdUnary, "f32x4")
	canon(0x5f, "f64x2.promote_low_f32x4", simdUnary, "f64x2")
	for code, name := range map[uint64]string{
		0x60: "i8x16.abs", 0x61: "i8x16.neg", 0x62: "i8x16.popcnt",
		0x7c: "i16x8.extadd_pairwise_i8x16_s", 0x7d: "i16x8.extadd_pairwise_i8x16_u",
		0x7e: "i32x4.extadd_pairwise_i16x8_s", 0x7f: "i32x4.extadd_pairwise_i16x8_u",
		0x80: "i16x8.abs", 0x81: "i16x8.neg",
		0x87: "i16x8.extend_low_i8x16_s", 0x88: "i16x8.extend_high_i8x16_s",
		0x89: "i16x8.extend_low_i8x16_u", 0x8a: "i16x8.extend_high_i8x16_u",
		0xa0: "i32x4.abs", 0xa1: "i32x4.neg",
		0xa7: "i32x4.extend_low_i16x8_s", 0xa8: "i32x4.extend_high_i16x8_s",
		0xa9: "i32x4.extend_low_i16x8_u", 0xaa: "i32x4.extend_high_i16x8_u",
		0xc0: "i64x2.abs", 0xc1: "i64x2.neg",
		0xc7: "i64x2.extend_low_i32x4_s", 0xc8: "i64x2.extend_high_i32x4_s",
		0xc9: "i64x2.extend_low_i32x4_u", 0xca: "i64x2.extend_high_i32x4_u",
		0xe0: "f32x4.abs", 0xe1: "f32x4.neg", 0xec: "f64x2.abs", 0xed: "f64x2.neg",
		0xf8: "i32x4.trunc_sat_f32x4_s", 0xf9: "i32x4.trunc_sat_f32x4_u",
		0xfa: "f32x4.convert_i32x4_s", 0xfb: "f32x4.convert_i32x4_u",
		0xfc: "i32x4.trunc_sat_f64x2_s_zero", 0xfd: "i32x4.trunc_sat_f64x2_u_zero",
		0xfe: "f64x2.convert_low_i32x4_s", 0xff: "f64x2.convert_low_i32x4_u",
	} {
		add(code, name, simdUnary)
	}
	for code, name := range map[uint64]string{
		0x63: "i8x16.all_true", 0x64: "i8x16.bitmask", 0x83: "i16x8.all_true", 0x84: "i16x8.bitmask",
		0xa3: "i32x4.all_true", 0xa4: "i32x4.bitmask", 0xc3: "i64x2.all_true", 0xc4: "i64x2.bitmask",
	} {
		add(code, name, simdTest)
	}
	for code, name := range map[uint64]string{
		0x6b: "i8x16.shl", 0x6c: "i8x16.shr_s", 0x6d: "i8x16.shr_u",
		0x8b: "i16x8.shl", 0x8c: "i16x8.shr_s", 0x8d: "i16x8.shr_u",
		0xab: "i32x4.shl", 0xac: "i32x4.shr_s", 0xad: "i32x4.shr_u",
		0xcb: "i64x2.shl", 0xcc: "i64x2.shr_s", 0xcd: "i64x2.shr_u",
	} {
		add(code, name, simdShift)
	}
	for code, name := range map[uint64]string{
		0x65: "i8x16.narrow_i16x8_s", 0x66: "i8x16.narrow_i16x8_u",
		0x6e: "i8x16.add", 0x6f: "i8x16.add_sat_s", 0x70: "i8x16.add_sat_u",
		0x71: "i8x16.sub", 0x72: "i8x16.sub_sat_s", 0x73: "i8x16.sub_sat_u",
		0x76: "i8x16.min_s", 0x77: "i8x16.min_u", 0x78: "i8x16.max_s", 0x79: "i8x16.max_u", 0x7b: "i8x16.avgr_u",
		0x82: "i16x8.q15mulr_sat_s", 0x85: "i16x8.narrow_i32x4_s", 0x86: "i16x8.narrow_i32x4_u",
		0x8e: "i16x8.add", 0x8f: "i16x8.add_sat_s", 0x90: "i16x8.add_sat_u",
		0x91: "i16x8.sub", 0x92: "i16x8.sub_sat_s", 0x93: "i16x8.sub_sat_u",
		0x95: "i16x8.mul", 0x96: "i16x8.min_s", 0x97: "i16x8.min_u", 0x98: "i16x8.max_s", 0x99: "i16x8.max_u", 0x9b: "i16x8.avgr_u",
		0x9c: "i16x8.extmul_low_i8x16_s", 0x9d: "i16x8.extmul_high_i8x16_s",
		0x9e: "i16x8.extmul_low_i8x16_u", 0x9f: "i16x8.extmul_high_i8x16_u",
		0xae: "i32x4.add", 0xb1: "i32x4.sub", 0xb5: "i32x4.mul",
		0xb6: "i32x4.min_s", 0xb7: "i32x4.min_u", 0xb8: "i32x4.max_s", 0xb9: "i32x4.max_u", 0xba: "i32x4.dot_i16x8_s",
		0xbc: "i32x4.extmul_low_i16x8_s", 0xbd: "i32x4.extmul_high_i16x8_s",
		0xbe: "i32x4.extmul_low_i16x8_u", 0xbf: "i32x4.extmul_high_i16x8_u",
		0xce: "i64x2.add", 0xd1: "i64x2.sub", 0xd5: "i64x2.mul",
		0xd6: "i64x2.eq", 0xd7: "i64x2.ne", 0xd8: "i64x2.lt_s", 0xd9: "i64x2.gt_s", 0xda: "i64x2.le_s", 0xdb: "i64x2.ge_s",
		0xdc: "i64x2.extmul_low_i32x4_s", 0xdd: "i64x2.extmul_high_i32x4_s",
		0xde: "i64x2.extmul_low_i32x4_u", 0xdf: "i64x2.extmul_high_i32x4_u",
		0xe8: "f32x4.min", 0xe9: "f32x4.max", 0xea: "f32x4.pmin", 0xeb: "f32x4.pmax",
		0xf4: "f64x2.min", 0xf5: "f64x2.max", 0xf6: "f64x2.pmin", 0xf7: "f64x2.pmax",
	} {
		add(code, name, simdBinary)
	}
	for code, name := range map[uint64]string{
		0x67: "f32x4.ceil", 0x68: "f32x4.floor", 0x69: "f32x4.trunc", 0x6a: "f32x4.nearest", 0xe3: "f32x4.sqrt",
		0x74: "f64x2.ceil", 0x75: "f64x2.floor", 0x7a: "f64x2.trunc", 0x94: "f64x2.nearest", 0xef: "f64x2.sqrt",
	} {
		canon(code, name, simdUnary, name[:5])
	}
	for code, name := range map[uint64]string{
		0xe4: "f32x4.add", 0xe5: "f32x4.sub", 0xe6: "f32x4.mul", 0xe7: "f32x4.div",
		0xf0: "f64x2.add", 0xf1: "f64x2.sub", 0xf2: "f64x2.mul", 0xf3: "f64x2.div",
	} {
		canon(code, name, simdBinary, name[:5])
	}
}

// The helpers the SIMD code of every target may call (passes.LowerSIMD).
var simdHelpers = []string{
	"lane_u32", "lane_mask8", "lane_mask16", "lane_mask32", "lane_mask64", "lane_bool",
	"lane_sat_s8", "lane_sat_u8", "lane_sat_s16", "lane_sat_u16", "lane_canon32", "lane_canon64",
	"lane_pmin32", "lane_pmax32", "lane_pmin64", "lane_pmax64", "lane_byte",
	"lane_pminf32", "lane_pmaxf32", "lane_pminf64", "lane_pmaxf64",
	"i32_trunc_sat_f32_s", "i32_trunc_sat_f32_u", "i32_trunc_sat_f64_s", "i32_trunc_sat_f64_u",
}

func (t *translator) addSIMDHelpers() {
	for _, h := range simdHelpers {
		t.helpers.add(h)
	}
}

// The conversion name (v128.bytes or v128.from_bytes) of x, between a
// vector and its bytes, where the Module meets its host
// (wasmType.apiType); x is a variable, or a call of the host, whose
// result the conversion's code reads once.
func simdConversion(name string, x ast.Expr) ast.Expr {
	return &ast.CallExpr{Fun: newID(simdName(name)), Args: []ast.Expr{x}}
}

// The name of the operation of instruction name in the SIMD form.
func simdName(name string) string {
	return passes.SIMDPrefix + strings.ReplaceAll(name, ".", "_")
}

func (fn *funcCompiler) simdCall(name string, args ...ast.Expr) *ast.CallExpr {
	return &ast.CallExpr{Fun: newID(simdName(name)), Args: args}
}

func intLit(v int) ast.Expr { return &ast.BasicLit{Kind: token.INT, Value: strconv.Itoa(v)} }

// Translates the SIMD instruction after its 0xFD prefix.
func (t *translator) readOpcodeSIMD(fn *funcCompiler) error {
	code, err := readLEB128(t.in)
	if err != nil {
		return err
	}
	t.addSIMDHelpers()
	op, ok := simdOps[code]
	if !ok {
		if code >= 0x100 && code <= 0x113 {
			return fmt.Errorf("unsupported opcode (relaxed SIMD): 0xFD 0x%02X: build without -mrelaxed-simd", code)
		}
		return fmt.Errorf("unsupported opcode (SIMD): 0xFD 0x%02X", code)
	}
	memarg := func() (uint64, error) {
		if _, err := readLEB128(t.in); err != nil { // align
			return 0, err
		}
		return readLEB128(t.in)
	}
	lane := func() (ast.Expr, error) {
		b, err := t.in.ReadByte()
		return intLit(int(b)), err
	}
	// Operands are variables or constants: a pending expression is
	// written to a variable first.
	if !fn.blocks.top().unreachable {
		fn.flush()
	}
	result := func(e ast.Expr) {
		if op.canon != "" {
			e = fn.simdCall(op.canon+".canon", e)
		}
		fn.push(e)
	}
	mem := func() ast.Expr { return fn.memory.selector }
	switch op.form {
	case simdUnary, simdTest:
		x := fn.pop()
		result(fn.simdCall(op.name, x))
	case simdBinary, simdShift:
		y := fn.pop()
		x := fn.pop()
		result(fn.simdCall(op.name, x, y))
	case simdTernary:
		c := fn.pop()
		y := fn.pop()
		x := fn.pop()
		result(fn.simdCall(op.name, x, y, c))
	case simdSplat:
		result(fn.simdCall(op.name, fn.pop()))
	case simdExtract:
		l, err := lane()
		if err != nil {
			return err
		}
		result(fn.simdCall(op.name, fn.pop(), l))
	case simdReplace:
		l, err := lane()
		if err != nil {
			return err
		}
		v := fn.pop()
		x := fn.pop()
		result(fn.simdCall(op.name, x, l, v))
	case simdLoad:
		offset, err := memarg()
		if err != nil {
			return err
		}
		result(fn.simdCall(op.name, mem(), fn.popAddr(offset)))
	case simdStore:
		offset, err := memarg()
		if err != nil {
			return err
		}
		v := fn.pop()
		fn.emit(&ast.ExprStmt{X: fn.simdCall(op.name, mem(), fn.popAddr(offset), v)})
	case simdLoadLane:
		offset, err := memarg()
		if err != nil {
			return err
		}
		l, err := lane()
		if err != nil {
			return err
		}
		v := fn.pop()
		result(fn.simdCall(op.name, mem(), fn.popAddr(offset), v, l))
	case simdStoreLane:
		offset, err := memarg()
		if err != nil {
			return err
		}
		l, err := lane()
		if err != nil {
			return err
		}
		v := fn.pop()
		fn.emit(&ast.ExprStmt{X: fn.simdCall(op.name, mem(), fn.popAddr(offset), v, l)})
	case simdConst:
		var b [16]byte
		if _, err := ioReadFull(t, b[:]); err != nil {
			return err
		}
		fn.pushConst(simdConstExpr(b))
	case simdShuffle:
		var b [16]byte
		if _, err := ioReadFull(t, b[:]); err != nil {
			return err
		}
		y := fn.pop()
		x := fn.pop()
		args := []ast.Expr{x, y}
		for _, i := range b {
			args = append(args, intLit(int(i)))
		}
		result(fn.simdCall(op.name, args...))
	}
	return nil
}

func ioReadFull(t *translator, b []byte) (int, error) {
	for i := range b {
		c, err := t.in.ReadByte()
		if err != nil {
			return i, err
		}
		b[i] = c
	}
	return len(b), nil
}

// The v128.const of bytes b: simd_v128_const(w0, w1, w2, w3) of its four
// little-endian 32-bit words.
func simdConstExpr(b [16]byte) ast.Expr {
	args := make([]ast.Expr, 4)
	for i := range args {
		args[i] = &ast.BasicLit{Kind: token.INT, Value: "0x" + strconv.FormatUint(uint64(binary.LittleEndian.Uint32(b[4*i:])), 16)}
	}
	return &ast.CallExpr{Fun: newID(simdName("v128.const")), Args: args}
}
