package passes

import (
	"go/ast"
	"strconv"
	"strings"
)

// The direct code of the SIMD operations on the archsimd targets: calls of
// simd/archsimd's methods (Go 1.27) on SIMDType, archsimd.Uint32x4, viewed
// as the vector type of each operation's lanes. An operation a target has
// no direct code for, or whose instruction there differs from
// WebAssembly's (amd64's float-to-integer conversions do not saturate),
// is written lane by lane (simdPortable), through GetElem and SetElem.
//
// The methods used for amd64 need at most AVX2 (GOAMD64=v3): none of
// archsimd's AVX-512 methods.

// Views of a vector, from and to archsimd.Uint32x4, by lane type.
var archsimdViews = map[string][2]string{
	"u32": {"", ""},
	"i32": {".BitsToInt32()", ".ToBits()"},
	"f32": {".BitsToFloat32()", ".ToBits()"},
	"u8":  {".ReshapeToUint8s()", ".ReshapeToUint32s()"},
	"i8":  {".ReshapeToUint8s().BitsToInt8()", ".ToBits().ReshapeToUint32s()"},
	"u16": {".ReshapeToUint16s()", ".ReshapeToUint32s()"},
	"i16": {".ReshapeToUint16s().BitsToInt16()", ".ToBits().ReshapeToUint32s()"},
	"u64": {".ReshapeToUint64s()", ".ReshapeToUint32s()"},
	"i64": {".ReshapeToUint64s().BitsToInt64()", ".ToBits().ReshapeToUint32s()"},
	"f64": {".ReshapeToUint64s().BitsToFloat64()", ".ToBits().ReshapeToUint32s()"},
}

// The vector type of each lane type, and its mask conversion.
var archsimdTypes = map[string]string{
	"u8": "Uint8x16", "i8": "Int8x16", "u16": "Uint16x8", "i16": "Int16x8",
	"u32": "Uint32x4", "i32": "Int32x4", "u64": "Uint64x2", "i64": "Int64x2",
	"f32": "Float32x4", "f64": "Float64x2",
}

var archsimdMaskToInt = map[string]string{"8": ".ToInt8x16()", "16": ".ToInt16x8()", "32": ".ToInt32x4()", "64": ".ToInt64x2()"}

// The operand e as an expression: its target code (the translator's
// constants are not yet rewritten when a store reads them).
func archsimdOperand(e ast.Expr) string {
	if c, ok := e.(*ast.CallExpr); ok {
		if id, ok := c.Fun.(*ast.Ident); ok && id.Name == SIMDPrefix+"v128_const" {
			return archsimdConst(c.Args)
		}
	}
	s := exprString(e)
	if _, ok := e.(*ast.Ident); !ok {
		s = "(" + s + ")"
	}
	return s
}

func archsimdConst(args []ast.Expr) string {
	zero := true
	var w []string
	for _, a := range args {
		s := exprString(a)
		if v, err := strconv.ParseUint(s, 0, 32); err != nil || v != 0 {
			zero = false
		}
		w = append(w, s)
	}
	if zero {
		return SIMDType + "{}"
	}
	return "archsimd.LoadUint32x4Array(&[4]uint32{" + strings.Join(w, ", ") + "})"
}

// The lane type of a shape and signedness: i8x16 and "s" is i8.
func laneTypeOf(shape string, signed bool) string {
	w := strconv.Itoa(laneWidth(shape))
	switch {
	case shape[0] == 'f':
		return "f" + w
	case signed:
		return "i" + w
	}
	return "u" + w
}

// The little-endian unsigned integer of size bytes at the pointer p.
func leBytes(p string, size int) string {
	var parts []string
	for i := range size {
		parts = append(parts, laneType(size*8)+"("+p+"["+strconv.Itoa(i)+"])<<"+strconv.Itoa(8*i))
	}
	return "(" + strings.Join(parts, "|") + ")"
}

func archsimdOp(target SIMDTarget, op string, args []ast.Expr) string {
	shape, name, _ := strings.Cut(op, "_")
	if shape == "v128" {
		return archsimdV128(target, name, args)
	}
	width := laneWidth(shape)
	w := strconv.Itoa(width)
	isFloat := shape[0] == 'f'
	view := func(e ast.Expr, lt string) string { return archsimdOperand(e) + archsimdViews[lt][0] }
	back := func(e, lt string) string { return e + archsimdViews[lt][1] }
	unsignedLT := laneTypeOf(shape, false)
	signedLT := laneTypeOf(shape, true)
	if isFloat {
		unsignedLT = "u" + w
	}
	binary := func(lt, method string) string {
		return back(view(args[0], lt)+"."+method+"("+view(args[1], lt)+")", lt)
	}
	unary := func(lt, method string) string { return back(view(args[0], lt)+"."+method+"()", lt) }
	compare := func(lt, method string) string {
		mask := view(args[0], lt) + "." + method + "(" + view(args[1], lt) + ")" + archsimdMaskToInt[w]
		return back(mask, "i"+w)
	}
	cmps := map[string]string{"eq": "Equal", "ne": "NotEqual", "lt": "Less", "gt": "Greater", "le": "LessEqual", "ge": "GreaterEqual"}

	switch name {
	case "splat":
		v := scalar(args[0])
		switch {
		case shape == "f32x4":
			return "archsimd.BroadcastUint32x4(math.Float32bits(" + v + "))"
		case shape == "f64x2":
			return "archsimd.BroadcastUint64x2(math.Float64bits(" + v + "))" + archsimdViews["u64"][1]
		}
		return "archsimd.Broadcast" + archsimdTypes[unsignedLT] + "(" + laneType(width) + "(" + v + "))" + archsimdViews[unsignedLT][1]
	case "extract_lane", "extract_lane_s", "extract_lane_u":
		lane := exprString(args[1])
		e := view(args[0], unsignedLT) + ".GetElem(" + lane + ")"
		switch {
		case shape == "f32x4":
			return "math.Float32frombits(" + e + ")"
		case shape == "f64x2":
			return "math.Float64frombits(" + e + ")"
		case width == 64:
			return "int64(" + e + ")"
		case name == "extract_lane_s":
			return "int32(" + signed(e, width) + ")"
		}
		return "int32(" + e + ")"
	case "replace_lane":
		lane := exprString(args[1])
		v := scalar(args[2])
		switch {
		case shape == "f32x4":
			v = "math.Float32bits(" + v + ")"
		case shape == "f64x2":
			v = "math.Float64bits(" + v + ")"
		default:
			v = laneType(width) + "(" + v + ")"
		}
		return back(view(args[0], unsignedLT)+".SetElem("+lane+", "+v+")", unsignedLT)
	case "canon":
		f := view(args[0], "f"+w)
		return back("archsimd.Broadcast"+archsimdTypes["u"+w]+"("+map[string]string{"32": "0x7fc00000", "64": "0x7ff8000000000000"}[w]+").IfElse("+f+".NotEqual("+f+"), "+view(args[0], "u"+w)+")", "u"+w)
	}

	if isFloat {
		lt := "f" + w
		switch name {
		case "add", "sub", "mul", "div":
			return binary(lt, map[string]string{"add": "Add", "sub": "Sub", "mul": "Mul", "div": "Div"}[name])
		case "sqrt", "ceil", "floor", "trunc", "nearest":
			return unary(lt, map[string]string{"sqrt": "Sqrt", "ceil": "Ceil", "floor": "Floor", "trunc": "Trunc", "nearest": "Round"}[name])
		case "abs", "neg":
			sign := "archsimd.Broadcast" + archsimdTypes["u"+w] + "(1<<" + strconv.Itoa(width-1) + ")"
			method := map[string]string{"abs": "AndNot", "neg": "Xor"}[name]
			return back(view(args[0], "u"+w)+"."+method+"("+sign+")", "u"+w)
		case "min", "max":
			// WebAssembly's min and max return a NaN for a NaN lane, the
			// canonical NaN here, and order -0 below +0. amd64's MINPS
			// and MAXPS return their second operand for a NaN or for two
			// equal lanes, so of two zeros: where the operands compare
			// equal, the lane is the or (min) or the and (max) of their
			// bits, which is what both operand orders combined give, -0
			// and +0 for zeros. (a.Min(b) and b.Min(a) are not two
			// instructions: the compiler takes Min for commutative and
			// keeps one; see bugs/go-archsimd-min-commutative.md.)
			a, b := view(args[0], lt), view(args[1], lt)
			ua, ub := view(args[0], "u"+w), view(args[1], "u"+w)
			method := map[string]string{"min": "Min", "max": "Max"}[name]
			r := a + "." + method + "(" + b + ").ToBits()"
			if target == SIMDAMD64 {
				combine := map[string]string{"min": "Or", "max": "And"}[name]
				r = ua + "." + combine + "(" + ub + ").IfElse(" + a + ".Equal(" + b + "), " + r + ")"
			}
			nan := a + ".NotEqual(" + a + ").Or(" + b + ".NotEqual(" + b + "))"
			canon := "archsimd.Broadcast" + archsimdTypes["u"+w] + "(" + map[string]string{"32": "0x7fc00000", "64": "0x7ff8000000000000"}[w] + ")"
			return back(canon+".IfElse("+nan+", "+r+")", "u"+w)
		case "pmin", "pmax":
			// b < a ? b : a, and a < b ? b : a: an operand, bit for bit.
			a, b := view(args[0], lt), view(args[1], lt)
			ua, ub := view(args[0], "u"+w), view(args[1], "u"+w)
			less := b + ".Less(" + a + ")"
			if name == "pmax" {
				less = a + ".Less(" + b + ")"
			}
			return back(ub+".IfElse("+less+", "+ua+")", "u"+w)
		}
		if m, ok := cmps[name]; ok {
			return compare(lt, m)
		}
		switch shape + "_" + name {
		case "f32x4_convert_i32x4_s":
			return back(view(args[0], "i32")+".ConvertToFloat32()", "f32")
		case "f32x4_convert_i32x4_u":
			if target == SIMDAMD64 {
				return "" // AVX-512
			}
			return back(view(args[0], "u32")+".ConvertToFloat32()", "f32")
		case "f64x2_promote_low_f32x4":
			if target == SIMDARM64 {
				return back(view(args[0], "f32")+".ConvertLo2ToFloat64()", "f64")
			}
		case "f64x2_convert_low_i32x4_s", "f64x2_convert_low_i32x4_u":
			if target == SIMDWasm {
				lt := map[byte]string{'s': "i32", 'u': "u32"}[name[len(name)-1]]
				return back(view(args[0], lt)+".ConvertLo2ToFloat64()", "f64")
			}
		}
		return ""
	}

	// Integer operations.
	switch name {
	case "add", "sub":
		return binary(unsignedLT, map[string]string{"add": "Add", "sub": "Sub"}[name])
	case "mul":
		if width == 8 || width == 64 && target != SIMDWasm {
			return "" // amd64's 64-bit multiply is AVX-512; arm64 has none
		}
		return binary(unsignedLT, "Mul")
	case "neg":
		return unary(signedLT, "Neg")
	case "abs":
		if width == 64 && target != SIMDWasm {
			return ""
		}
		return unary(signedLT, "Abs")
	case "add_sat_s", "sub_sat_s", "add_sat_u", "sub_sat_u":
		lt := map[byte]string{'s': signedLT, 'u': unsignedLT}[name[len(name)-1]]
		return binary(lt, map[byte]string{'a': "AddSaturated", 's': "SubSaturated"}[name[0]])
	case "min_s", "max_s", "min_u", "max_u":
		if width == 64 {
			return ""
		}
		lt := map[byte]string{'s': signedLT, 'u': unsignedLT}[name[len(name)-1]]
		return binary(lt, map[string]string{"min": "Min", "max": "Max"}[name[:3]])
	case "avgr_u":
		return binary(unsignedLT, "Average")
	case "shl", "shr_s", "shr_u":
		if width == 8 && target == SIMDAMD64 {
			return "" // no byte shifts
		}
		if width == 64 && name == "shr_s" && target == SIMDAMD64 {
			return "" // AVX-512
		}
		lt := unsignedLT
		if name == "shr_s" {
			lt = signedLT
		}
		method := map[string]string{"shl": "ShiftAllLeft", "shr_s": "ShiftAllRight", "shr_u": "ShiftAllRight"}[name]
		s := "uint64(uint32(" + scalar(args[1]) + ")&" + strconv.Itoa(width-1) + ")"
		return back(view(args[0], lt)+"."+method+"("+s+")", lt)
	case "popcnt":
		if target == SIMDAMD64 {
			return ""
		}
		return unary("u8", "OnesCount")
	case "swizzle":
		switch target {
		case SIMDAMD64:
			// PSHUFB takes an index's low 4 bits unless its bit 7 is
			// set: adding 0x70 with unsigned saturation sets bit 7 of
			// every index of 16 and more, and keeps the low 4 bits of the
			// others.
			idx := view(args[1], "u8") + ".AddSaturated(archsimd.BroadcastUint8x16(0x70)).BitsToInt8()"
			return back(view(args[0], "i8")+".PermuteOrZero("+idx+")", "i8")
		case SIMDARM64:
			return back(view(args[0], "u8")+".LookupOrZero("+view(args[1], "u8")+")", "u8")
		case SIMDWasm:
			return back(view(args[0], "i8")+".LookupOrZero("+view(args[1], "i8")+")", "i8")
		}
	case "shuffle":
		return archsimdShuffle(target, args)
	case "dot_i16x8_s":
		if target != SIMDAMD64 {
			return ""
		}
		return back(view(args[0], "i16")+".DotProductPairs("+view(args[1], "i16")+")", "i32")
	case "narrow_i32x4_s", "narrow_i32x4_u":
		if target != SIMDAMD64 {
			return ""
		}
		method := map[byte]string{'s': "SaturateToInt16Concat", 'u': "SaturateToUint16Concat"}[name[len(name)-1]]
		lt := map[byte]string{'s': "i16", 'u': "u16"}[name[len(name)-1]]
		return back(view(args[0], "i32")+"."+method+"("+view(args[1], "i32")+")", lt)
	case "bitmask":
		// Masks to bits: VPMOVMSKB, VMOVMSKPS, VMOVMSKPD; amd64's
		// Mask16x8.ToBits is AVX-512.
		if target != SIMDAMD64 || width == 16 {
			return ""
		}
		lt := signedLT
		zero := "archsimd." + archsimdTypes[lt] + "{}"
		return "int32(" + view(args[0], lt) + ".Less(" + zero + ").ToBits())"
	case "all_true":
		if target != SIMDAMD64 || width == 16 {
			return ""
		}
		zero := "archsimd." + archsimdTypes[unsignedLT] + "{}"
		return "lane_bool(" + view(args[0], unsignedLT) + ".Equal(" + zero + ").ToBits() == 0)"
	}
	if m, ok := cmps[strings.TrimSuffix(strings.TrimSuffix(name, "_s"), "_u")]; ok {
		lt := unsignedLT
		if strings.HasSuffix(name, "_s") || width == 64 {
			lt = signedLT
		}
		return compare(lt, m)
	}
	if from, high, sign, ok := extOp(name); ok && strings.HasPrefix(name, "extend_") {
		src := laneTypeOf("i"+strconv.Itoa(from)+"x"+strconv.Itoa(128/from), sign)
		dst := laneTypeOf(shape, sign)
		t := archsimdTypes[dst][:len(archsimdTypes[dst])-len(strconv.Itoa(128/width))-1]
		method := "ExtendLo" + strconv.Itoa(128/width) + "To" + t
		v := view(args[0], src)
		if high {
			switch target {
			case SIMDWasm:
				method = "ExtendHi" + strconv.Itoa(128/width) + "To" + t
			case SIMDARM64:
				v += ".HiToLo()"
			default:
				return ""
			}
		}
		return back(v+"."+method+"()", dst)
	}
	switch op {
	case "i32x4_trunc_sat_f32x4_s", "i32x4_trunc_sat_f32x4_u":
		if target == SIMDAMD64 {
			return "" // CVTTPS2DQ does not saturate
		}
		method := map[byte]string{'s': "ConvertToInt32", 'u': "ConvertToUint32"}[op[len(op)-1]]
		lt := map[byte]string{'s': "i32", 'u': "u32"}[op[len(op)-1]]
		return back(view(args[0], "f32")+"."+method+"()", lt)
	}
	return ""
}

// i8x16.shuffle: the lanes of the first operand at the indexes below 16,
// of the second at the others, each picked by a lookup that gives 0 for
// the indexes of the other operand, and the two or'ed.
func archsimdShuffle(target SIMDTarget, args []ast.Expr) string {
	var ix, iy []string
	none := "-1"
	if target == SIMDARM64 {
		none = "255"
	}
	for _, a := range args[2:] {
		i := laneIndex(a)
		if i < 16 {
			ix, iy = append(ix, strconv.Itoa(i)), append(iy, none)
		} else {
			ix, iy = append(ix, none), append(iy, strconv.Itoa(i-16))
		}
	}
	lt, method := "i8", "PermuteOrZero"
	switch target {
	case SIMDARM64:
		lt, method = "u8", "LookupOrZero"
	case SIMDWasm:
		method = "LookupOrZero"
	}
	elt := map[string]string{"i8": "int8", "u8": "uint8"}[lt]
	load := func(idx []string) string {
		return "archsimd.Load" + archsimdTypes[lt] + "Array(&[16]" + elt + "{" + strings.Join(idx, ", ") + "})"
	}
	view := func(e ast.Expr) string { return archsimdOperand(e) + archsimdViews[lt][0] }
	return "(" + view(args[0]) + "." + method + "(" + load(ix) + ").Or(" + view(args[1]) + "." + method + "(" + load(iy) + ")))" + archsimdViews[lt][1]
}

// The direct code of a v128 operation; a memory access's first operand is
// its pointer variable.
func archsimdV128(target SIMDTarget, name string, args []ast.Expr) string {
	x := func(i int) string { return archsimdOperand(args[i]) }
	ptr := func() string { return exprString(args[0]) }
	switch name {
	case "not":
		return x(0) + ".Not()"
	case "and", "or", "xor", "andnot":
		return x(0) + "." + map[string]string{"and": "And", "or": "Or", "xor": "Xor", "andnot": "AndNot"}[name] + "(" + x(1) + ")"
	case "bitselect":
		return x(0) + ".And(" + x(2) + ").Or(" + x(1) + ".AndNot(" + x(2) + "))"
	case "any_true":
		if target == SIMDAMD64 {
			return "lane_bool(!" + x(0) + ".IsZero())"
		}
	case "const":
		return archsimdConst(args)
	case "load", "from_bytes":
		p := ptr()
		if name == "from_bytes" {
			p = "&" + exprString(args[0])
		}
		return "archsimd.LoadUint8x16Array(" + p + ").ReshapeToUint32s()"
	case "load8_splat", "load16_splat", "load32_splat", "load64_splat":
		size, _ := strconv.Atoi(name[4:strings.IndexByte(name, '_')])
		t := "Uint" + strconv.Itoa(size) + "x" + strconv.Itoa(128/size)
		v := leBytes(ptr(), size/8)
		return "archsimd.Broadcast" + t + "(" + v + ")" + archsimdViews["u"+strconv.Itoa(size)][1]
	case "load32_zero":
		return SIMDType + "{}.SetElem(0, " + leBytes(ptr(), 4) + ")"
	case "load64_zero":
		return "archsimd.Uint64x2{}.SetElem(0, " + leBytes(ptr(), 8) + ").ReshapeToUint32s()"
	case "load8_lane", "load16_lane", "load32_lane", "load64_lane":
		size, _ := strconv.Atoi(name[4:strings.IndexByte(name, '_')])
		lt := "u" + strconv.Itoa(size)
		return x(1) + archsimdViews[lt][0] + ".SetElem(" + exprString(args[2]) + ", " + leBytes(ptr(), size/8) + ")" + archsimdViews[lt][1]
	case "load8x8_s", "load8x8_u", "load16x4_s", "load16x4_u", "load32x2_s", "load32x2_u":
		from, _ := strconv.Atoi(name[4:strings.IndexByte(name, 'x')])
		sign := name[len(name)-1] == 's'
		src := laneTypeOf("i"+strconv.Itoa(from)+"x"+strconv.Itoa(128/from), sign)
		dst := laneTypeOf("i"+strconv.Itoa(2*from)+"x"+strconv.Itoa(64/from), sign)
		t := archsimdTypes[dst][:len(archsimdTypes[dst])-len(strconv.Itoa(64/from))-1]
		low := "archsimd.Uint64x2{}.SetElem(0, " + leBytes(ptr(), 8) + ").ReshapeToUint32s()"
		v := "(" + low + ")" + archsimdViews[src][0]
		return v + ".ExtendLo" + strconv.Itoa(64/from) + "To" + t + "()" + archsimdViews[dst][1]
	}
	return ""
}

// The statements of a store on an archsimd target, or nil.
func archsimdStore(target SIMDTarget, op string, args []ast.Expr) []string {
	if op == "v128_store" {
		return []string{archsimdOperand(args[1]) + ".ReshapeToUint8s().StoreArray(" + exprString(args[0]) + ")"}
	}
	return nil
}
