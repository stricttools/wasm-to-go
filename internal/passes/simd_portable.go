package passes

import (
	"fmt"
	"go/ast"
	"strconv"
	"strings"
)

// The portable code of the SIMD operations: Go expressions over the four
// uint32 words of a v128, each a little-endian quarter of its 16 bytes,
// written lane by lane. With the portable target the words are the fields
// of SIMDType's struct; with an archsimd target, which has no direct code
// for an operation, they are the vector's elements (GetElem), and the
// result is set element by element (SetElem): the same code, slower.

type simdPortable struct {
	target SIMDTarget
	// The domain of each vector variable (portableDomains): "f32" for a
	// SIMDTypeF32, "f64" for a SIMDTypeF64, and "bits" (or missing) for a
	// SIMDType.
	domains map[string]string
}

func (l *simdLowerer) portable() simdPortable { return simdPortable{l.target, l.domains} }

// The domain of the vector expression e: a variable's, or a composite
// literal's by its type.
func (p simdPortable) domain(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		if d := p.domains[x.Name]; d != "" {
			return d
		}
	case *ast.CompositeLit:
		if id, ok := x.Type.(*ast.Ident); ok {
			switch id.Name {
			case SIMDTypeF32:
				return "f32"
			case SIMDTypeF64:
				return "f64"
			}
		}
	case *ast.ParenExpr:
		return p.domain(x.X)
	}
	return "bits"
}

// The float lanes of width bits of the vector operand e, as expressions.
func (p simdPortable) floats(e ast.Expr, width int) []string {
	n := 128 / width
	out := make([]string, n)
	field := map[int]string{32: ".F", 64: ".D"}[width]
	switch d := p.domain(e); {
	case d == "f32" && width == 32, d == "f64" && width == 64:
		if lit, ok := e.(*ast.CompositeLit); ok && len(lit.Elts) == n {
			for i := range out {
				out[i] = "(" + exprString(lit.Elts[i]) + ")"
			}
			return out
		}
		s := exprString(e)
		for i := range out {
			out[i] = s + field + strconv.Itoa(i)
		}
		return out
	}
	for i, l := range lanesOf(p.words(e), width) {
		out[i] = float(l, width)
	}
	return out
}

// The vector of float lanes l of width bits: of the float domain in the
// portable code, of words elsewhere.
func (p simdPortable) packFloats(l []string, width int) string {
	if p.target == SIMDPortable {
		t := map[int]string{32: SIMDTypeF32, 64: SIMDTypeF64}[width]
		return t + "{" + strings.Join(l, ", ") + "}"
	}
	bits := make([]string, len(l))
	for i, f := range l {
		bits[i] = "math.Float" + strconv.Itoa(width) + "bits(" + f + ")"
	}
	return p.pack(packLanes(bits, width))
}

// The four words of the vector operand e, as expressions.
func (p simdPortable) words(e ast.Expr) [4]string {
	var w [4]string
	switch x := e.(type) {
	case *ast.CallExpr:
		if id, ok := x.Fun.(*ast.Ident); ok && id.Name == SIMDPrefix+"v128_const" && len(x.Args) == 4 {
			// Through lane_u32, which keeps the words out of Go's
			// constant evaluator, as i32 does a scalar constant: a
			// conversion of a constant that overflows does not compile.
			for i := range w {
				w[i] = "lane_u32(" + exprString(x.Args[i]) + ")"
			}
			return w
		}
	case *ast.CompositeLit:
		if id, ok := x.Type.(*ast.Ident); ok && id.Name == SIMDType && len(x.Elts) == 4 {
			for i := range w {
				w[i] = "(" + exprString(x.Elts[i]) + ")"
			}
			return w
		}
	}
	switch p.domain(e) {
	case "f32":
		var bits []string
		for _, f := range p.floats(e, 32) {
			bits = append(bits, "math.Float32bits("+f+")")
		}
		return packLanes(bits, 32)
	case "f64":
		var bits []string
		for _, f := range p.floats(e, 64) {
			bits = append(bits, "math.Float64bits("+f+")")
		}
		return packLanes(bits, 64)
	}
	s := exprString(e)
	if _, ok := e.(*ast.Ident); !ok {
		s = "(" + s + ")"
	}
	for i := range w {
		if p.target == SIMDPortable {
			w[i] = s + ".L" + strconv.Itoa(i)
		} else {
			w[i] = s + ".GetElem(" + strconv.Itoa(i) + ")"
		}
	}
	return w
}

// The vector of words w.
func (p simdPortable) pack(w [4]string) string {
	if p.target == SIMDPortable {
		return SIMDType + "{" + strings.Join(w[:], ", ") + "}"
	}
	s := SIMDType + "{}"
	for i, x := range w {
		s += ".SetElem(" + strconv.Itoa(i) + ", " + x + ")"
	}
	return s
}

func shifted(x string, n int) string {
	if n == 0 {
		return x
	}
	return "(" + x + ">>" + strconv.Itoa(n) + ")"
}

// Lanes of words, as unsigned expressions of their width.
func lanes8(w [4]string) []string {
	l := make([]string, 16)
	for j := range l {
		l[j] = "uint8(" + shifted(w[j/4], 8*(j%4)) + ")"
	}
	return l
}

func lanes16(w [4]string) []string {
	l := make([]string, 8)
	for j := range l {
		l[j] = "uint16(" + shifted(w[j/2], 16*(j%2)) + ")"
	}
	return l
}

func lanes32(w [4]string) []string { return w[:] }

func lanes64(w [4]string) []string {
	return []string{
		"(uint64(" + w[0] + ")|uint64(" + w[1] + ")<<32)",
		"(uint64(" + w[2] + ")|uint64(" + w[3] + ")<<32)",
	}
}

func lanesOf(w [4]string, width int) []string {
	switch width {
	case 8:
		return lanes8(w)
	case 16:
		return lanes16(w)
	case 32:
		return lanes32(w)
	}
	return lanes64(w)
}

// The words of lanes of width bits, unsigned expressions of that width.
func packLanes(l []string, width int) [4]string {
	var w [4]string
	switch width {
	case 8:
		for i := range w {
			w[i] = "uint32(" + l[4*i] + ")|uint32(" + l[4*i+1] + ")<<8|uint32(" + l[4*i+2] + ")<<16|uint32(" + l[4*i+3] + ")<<24"
		}
	case 16:
		for i := range w {
			w[i] = "uint32(" + l[2*i] + ")|uint32(" + l[2*i+1] + ")<<16"
		}
	case 32:
		copy(w[:], l)
	case 64:
		for i := range 2 {
			w[2*i] = "uint32(" + l[i] + ")"
			w[2*i+1] = "uint32((" + l[i] + ")>>32)"
		}
	}
	return w
}

// The width of the lanes of a shape: i8x16 is 8, f64x2 64.
func laneWidth(shape string) int {
	n, _ := strconv.Atoi(shape[1:strings.IndexByte(shape, 'x')])
	return n
}

// The unsigned Go type of a lane of width bits.
func laneType(width int) string { return "uint" + strconv.Itoa(width) }

// The signed lane: int8(a) for a lane of width 8.
func signed(a string, width int) string { return "int" + strconv.Itoa(width) + "(" + a + ")" }

// The float of a lane of width 32 or 64.
func float(a string, width int) string {
	return "math.Float" + strconv.Itoa(width) + "frombits(" + a + ")"
}

func floatBits(f string, width int) string {
	return "math.Float" + strconv.Itoa(width) + "bits(float" + strconv.Itoa(width) + "(" + f + "))"
}

// The lane-wise result of applying f to the lanes of each operand.
func (p simdPortable) lanewise(width int, f func(a ...string) string, operands ...ast.Expr) string {
	ls := make([][]string, len(operands))
	for i, o := range operands {
		ls[i] = lanesOf(p.words(o), width)
	}
	out := make([]string, len(ls[0]))
	for j := range out {
		args := make([]string, len(ls))
		for i := range ls {
			args[i] = ls[i][j]
		}
		out[j] = laneType(width) + "(" + f(args...) + ")"
	}
	return p.pack(packLanes(out, width))
}

// The scalar operand e, as an expression.
func scalar(e ast.Expr) string { return "(" + exprString(e) + ")" }

// The constant lane operand e.
func laneIndex(e ast.Expr) int {
	n, err := strconv.Atoi(exprString(e))
	if err != nil {
		panic("a SIMD lane is not a constant: " + exprString(e))
	}
	return n
}

// The portable code of op, a SIMD operation other than a store, with its
// operands (the pointer variable first for a memory access).
func (p simdPortable) op(op string, args []ast.Expr) string {
	shape, name, _ := strings.Cut(op, "_")
	if shape == "v128" {
		return p.v128(name, args)
	}
	width := laneWidth(shape)
	isFloat := shape[0] == 'f'
	cmp := map[string]string{"eq": "==", "ne": "!=", "lt": "<", "gt": ">", "le": "<=", "ge": ">="}
	mask := func(c string) string { return "lane_mask" + strconv.Itoa(width) + "(" + c + ")" }
	switch {
	case name == "splat":
		v := scalar(args[0])
		n := 128 / width
		l := make([]string, n)
		for j := range l {
			l[j] = v
		}
		if isFloat {
			return p.packFloats(l, width)
		}
		for j := range l {
			l[j] = laneType(width) + "(" + v + ")"
		}
		return p.pack(packLanes(l, width))
	case strings.HasPrefix(name, "extract_lane"):
		if isFloat {
			return p.floats(args[0], width)[laneIndex(args[1])]
		}
		lanes := lanesOf(p.words(args[0]), width)
		a := lanes[laneIndex(args[1])]
		switch {
		case width == 64:
			return "int64(" + a + ")"
		case name == "extract_lane_s":
			return "int32(" + signed(a, width) + ")"
		default:
			return "int32(" + a + ")"
		}
	case name == "replace_lane":
		if isFloat {
			lanes := p.floats(args[0], width)
			lanes[laneIndex(args[1])] = scalar(args[2])
			return p.packFloats(lanes, width)
		}
		lanes := lanesOf(p.words(args[0]), width)
		lanes[laneIndex(args[1])] = laneType(width) + "(" + scalar(args[2]) + ")"
		return p.pack(packLanes(lanes, width))
	case name == "all_true":
		var cs []string
		for _, a := range lanesOf(p.words(args[0]), width) {
			cs = append(cs, a+" != 0")
		}
		return "lane_bool(" + strings.Join(cs, " && ") + ")"
	case name == "bitmask":
		var bs []string
		for j, a := range lanesOf(p.words(args[0]), width) {
			bs = append(bs, "int32("+a+">>"+strconv.Itoa(width-1)+")<<"+strconv.Itoa(j))
		}
		return strings.Join(bs, " | ")
	case name == "shl", name == "shr_s", name == "shr_u":
		s := "(uint32(" + scalar(args[1]) + ")&" + strconv.Itoa(width-1) + ")"
		return p.lanewise(width, func(a ...string) string {
			switch name {
			case "shl":
				return a[0] + "<<" + s
			case "shr_s":
				return signed(a[0], width) + ">>" + s
			}
			return a[0] + ">>" + s
		}, args[0])
	case name == "canon":
		var out []string
		for _, f := range p.floats(args[0], width) {
			w := strconv.Itoa(width)
			out = append(out, "math.Float"+w+"frombits(lane_canon"+w+"(math.Float"+w+"bits("+f+")))")
		}
		return p.packFloats(out, width)
	case isFloat:
		return p.float(shape, width, name, args)
	}
	if c, ok := cmp[strings.TrimSuffix(strings.TrimSuffix(name, "_s"), "_u")]; ok {
		return p.lanewise(width, func(a ...string) string {
			if strings.HasSuffix(name, "_s") || width == 64 && name != "eq" && name != "ne" {
				return mask(signed(a[0], width) + c + signed(a[1], width))
			}
			return mask(a[0] + c + a[1])
		}, args[0], args[1])
	}
	sat := func(sign string, w int, v string) string {
		return "lane_sat_" + sign + strconv.Itoa(w) + "(int64(" + v + "))"
	}
	wide := func(a string, w int, signedLane bool) string { // the lane as an int32 (or an int64 for 32-bit lanes)
		t := "int32"
		if w == 32 {
			t = "int64"
		}
		if signedLane {
			return t + "(" + signed(a, w) + ")"
		}
		return t + "(" + a + ")"
	}
	switch name {
	case "add", "sub", "mul":
		op := map[string]string{"add": "+", "sub": "-", "mul": "*"}[name]
		return p.lanewise(width, func(a ...string) string { return a[0] + op + a[1] }, args[0], args[1])
	case "neg":
		return p.lanewise(width, func(a ...string) string { return "-" + a[0] }, args[0])
	case "abs":
		return p.lanewise(width, func(a ...string) string {
			m := laneType(width) + "(" + signed(a[0], width) + ">>" + strconv.Itoa(width-1) + ")"
			return "(" + a[0] + "^" + m + ")-" + m
		}, args[0])
	case "add_sat_s", "add_sat_u", "sub_sat_s", "sub_sat_u":
		sign := name[len(name)-1:]
		op := map[byte]string{'a': "+", 's': "-"}[name[0]]
		return p.lanewise(width, func(a ...string) string {
			return sat(sign, width, wide(a[0], width, sign == "s")+op+wide(a[1], width, sign == "s"))
		}, args[0], args[1])
	case "min_s", "max_s":
		return p.lanewise(width, func(a ...string) string {
			return laneType(width) + "(" + name[:3] + "(" + signed(a[0], width) + ", " + signed(a[1], width) + "))"
		}, args[0], args[1])
	case "min_u", "max_u":
		return p.lanewise(width, func(a ...string) string { return name[:3] + "(" + a[0] + ", " + a[1] + ")" }, args[0], args[1])
	case "avgr_u":
		return p.lanewise(width, func(a ...string) string {
			return "(uint32(" + a[0] + ")+uint32(" + a[1] + ")+1)>>1"
		}, args[0], args[1])
	case "popcnt":
		return p.lanewise(width, func(a ...string) string { return "bits.OnesCount8(" + a[0] + ")" }, args[0])
	case "q15mulr_sat_s":
		return p.lanewise(width, func(a ...string) string {
			return sat("s", 16, "(int32("+signed(a[0], 16)+")*int32("+signed(a[1], 16)+")+0x4000)>>15")
		}, args[0], args[1])
	case "swizzle":
		x, y := p.words(args[0]), lanes8(p.words(args[1]))
		out := make([]string, 16)
		for j := range out {
			out[j] = "lane_byte(" + strings.Join(x[:], ", ") + ", " + y[j] + ")"
		}
		return p.pack(packLanes(out, 8))
	case "shuffle":
		x, y := lanes8(p.words(args[0])), lanes8(p.words(args[1]))
		out := make([]string, 16)
		idx := make([]int, 16)
		for j := range out {
			idx[j] = laneIndex(args[2+j])
			if idx[j] < 16 {
				out[j] = x[idx[j]]
			} else {
				out[j] = y[idx[j]-16]
			}
		}
		w := packLanes(out, 8)
		// A word of four bytes in order from one word is that word.
		xw, yw := p.words(args[0]), p.words(args[1])
		for i := range w {
			k := idx[4*i]
			if k%4 == 0 && idx[4*i+1] == k+1 && idx[4*i+2] == k+2 && idx[4*i+3] == k+3 {
				if k < 16 {
					w[i] = xw[k/4]
				} else {
					w[i] = yw[(k-16)/4]
				}
			}
		}
		return p.pack(w)
	case "narrow_i16x8_s", "narrow_i16x8_u", "narrow_i32x4_s", "narrow_i32x4_u":
		from := width * 2
		sign := name[len(name)-1:]
		var out []string
		for _, o := range args[:2] {
			for _, a := range lanesOf(p.words(o), from) {
				out = append(out, sat(sign, width, signed(a, from)))
			}
		}
		return p.pack(packLanes(out, width))
	case "dot_i16x8_s":
		x, y := lanes16(p.words(args[0])), lanes16(p.words(args[1]))
		out := make([]string, 4)
		for j := range out {
			out[j] = "uint32(int32(" + signed(x[2*j], 16) + ")*int32(" + signed(y[2*j], 16) + ")+int32(" + signed(x[2*j+1], 16) + ")*int32(" + signed(y[2*j+1], 16) + "))"
		}
		return p.pack(packLanes(out, 32))
	}
	// extend, extadd_pairwise, extmul, trunc_sat, and convert: from lanes
	// of another width.
	if from, high, sign, ok := extOp(name); ok {
		switch {
		case strings.HasPrefix(name, "extend_"):
			src := lanesOf(p.words(args[0]), from)
			n := 128 / width
			out := make([]string, n)
			for j := range out {
				a := src[j+n*btoi(high)]
				if sign {
					out[j] = laneType(width) + "(" + signed(signed(a, from), width) + ")"
				} else {
					out[j] = laneType(width) + "(" + a + ")"
				}
			}
			return p.pack(packLanes(out, width))
		case strings.HasPrefix(name, "extadd_pairwise_"):
			src := lanesOf(p.words(args[0]), from)
			out := make([]string, 128/width)
			for j := range out {
				a, b := src[2*j], src[2*j+1]
				if sign {
					out[j] = laneType(width) + "(" + signed(signed(a, from), width) + "+" + signed(signed(b, from), width) + ")"
				} else {
					out[j] = laneType(width) + "(" + a + ")+" + laneType(width) + "(" + b + ")"
				}
			}
			return p.pack(packLanes(out, width))
		case strings.HasPrefix(name, "extmul_"):
			x, y := lanesOf(p.words(args[0]), from), lanesOf(p.words(args[1]), from)
			n := 128 / width
			out := make([]string, n)
			for j := range out {
				a, b := x[j+n*btoi(high)], y[j+n*btoi(high)]
				if sign {
					out[j] = laneType(width) + "(" + signed(signed(a, from), width) + "*" + signed(signed(b, from), width) + ")"
				} else {
					out[j] = laneType(width) + "(" + a + ")*" + laneType(width) + "(" + b + ")"
				}
			}
			return p.pack(packLanes(out, width))
		}
	}
	switch op {
	case "i32x4_trunc_sat_f32x4_s", "i32x4_trunc_sat_f32x4_u":
		helper := "i32_trunc_sat_f32_" + op[len(op)-1:]
		var out [4]string
		for j, f := range p.floats(args[0], 32) {
			out[j] = "uint32(" + helper + "(" + f + "))"
		}
		return p.pack(out)
	case "i32x4_trunc_sat_f64x2_s_zero", "i32x4_trunc_sat_f64x2_u_zero":
		helper := "i32_trunc_sat_f64_" + op[len(op)-6:len(op)-5]
		src := p.floats(args[0], 64)
		return p.pack([4]string{"uint32(" + helper + "(" + src[0] + "))", "uint32(" + helper + "(" + src[1] + "))", "0", "0"})
	}
	panic("no portable code for SIMD operation " + op)
}

// Reports, for the extend, extadd_pairwise, and extmul operations, the
// width of the lanes they read, whether they read the high half, and
// whether the lanes are signed.
func extOp(name string) (from int, high, sign, ok bool) {
	i := strings.LastIndex(name, "_i")
	if i < 0 {
		return 0, false, false, false
	}
	shape := name[i+1:]
	shape, s, _ := strings.Cut(shape, "_")
	if len(shape) < 4 || !strings.Contains(shape, "x") {
		return 0, false, false, false
	}
	return laneWidth(shape), strings.Contains(name, "_high_"), s == "s", true
}

// The code of a float operation of shape: on the lanes as floats, whose
// result is in the float domain (packFloats) but for the comparisons.
func (p simdPortable) float(shape string, width int, name string, args []ast.Expr) string {
	w := strconv.Itoa(width)
	ft := "float" + w
	lanewise := func(f func(a ...string) string, operands ...ast.Expr) string {
		ls := make([][]string, len(operands))
		for i, o := range operands {
			ls[i] = p.floats(o, width)
		}
		out := make([]string, 128/width)
		for j := range out {
			a := make([]string, len(ls))
			for i := range ls {
				a[i] = ls[i][j]
			}
			out[j] = f(a...)
		}
		return p.packFloats(out, width)
	}
	bits := func(f string) string { return "math.Float" + w + "bits(" + f + ")" }
	frombits := func(b string) string { return "math.Float" + w + "frombits(" + b + ")" }
	cmp := map[string]string{"eq": "==", "ne": "!=", "lt": "<", "gt": ">", "le": "<=", "ge": ">="}
	if c, ok := cmp[name]; ok {
		x, y := p.floats(args[0], width), p.floats(args[1], width)
		out := make([]string, len(x))
		for j := range out {
			out[j] = "lane_mask" + w + "(" + x[j] + c + y[j] + ")"
		}
		return p.pack(packLanes(out, width))
	}
	switch name {
	case "add", "sub", "mul", "div":
		op := map[string]string{"add": "+", "sub": "-", "mul": "*", "div": "/"}[name]
		return lanewise(func(a ...string) string { return ft + "(" + a[0] + op + a[1] + ")" }, args[0], args[1])
	case "sqrt", "ceil", "floor", "trunc", "nearest":
		fn := map[string]string{"sqrt": "Sqrt", "ceil": "Ceil", "floor": "Floor", "trunc": "Trunc", "nearest": "RoundToEven"}[name]
		return lanewise(func(a ...string) string {
			if width == 32 {
				return "float32(math." + fn + "(float64(" + a[0] + ")))"
			}
			return "math." + fn + "(" + a[0] + ")"
		}, args[0])
	case "min", "max":
		return lanewise(func(a ...string) string {
			return frombits("lane_canon" + w + "(" + bits(name+"("+a[0]+", "+a[1]+")") + ")")
		}, args[0], args[1])
	case "pmin", "pmax":
		return lanewise(func(a ...string) string { return "lane_" + name + "f" + w + "(" + a[0] + ", " + a[1] + ")" }, args[0], args[1])
	case "abs":
		return lanewise(func(a ...string) string { return frombits(bits(a[0]) + "&^(1<<" + strconv.Itoa(width-1) + ")") }, args[0])
	case "neg":
		return lanewise(func(a ...string) string { return frombits(bits(a[0]) + "^(1<<" + strconv.Itoa(width-1) + ")") }, args[0])
	}
	switch shape + "_" + name {
	case "f32x4_convert_i32x4_s", "f32x4_convert_i32x4_u":
		src := lanes32(p.words(args[0]))
		out := make([]string, 4)
		for j := range out {
			if strings.HasSuffix(name, "_s") {
				out[j] = "float32(int32(" + src[j] + "))"
			} else {
				out[j] = "float32(" + src[j] + ")"
			}
		}
		return p.packFloats(out, 32)
	case "f64x2_convert_low_i32x4_s", "f64x2_convert_low_i32x4_u":
		src := lanes32(p.words(args[0]))
		out := make([]string, 2)
		for j := range out {
			if strings.HasSuffix(name, "_s") {
				out[j] = "float64(int32(" + src[j] + "))"
			} else {
				out[j] = "float64(" + src[j] + ")"
			}
		}
		return p.packFloats(out, 64)
	case "f32x4_demote_f64x2_zero":
		src := p.floats(args[0], 64)
		return p.packFloats([]string{"float32(" + src[0] + ")", "float32(" + src[1] + ")", "0", "0"}, 32)
	case "f64x2_promote_low_f32x4":
		src := p.floats(args[0], 32)
		return p.packFloats([]string{"float64(" + src[0] + ")", "float64(" + src[1] + ")"}, 64)
	}
	panic(fmt.Sprintf("no portable code for SIMD operation %s_%s", shape, name))
}

// The code of a v128 operation (other than a store); a memory access's
// first operand is its pointer variable.
func (p simdPortable) v128(name string, args []ast.Expr) string {
	bitwise := func(f func(a ...string) string, operands ...ast.Expr) string {
		ws := make([][4]string, len(operands))
		for i, o := range operands {
			ws[i] = p.words(o)
		}
		var out [4]string
		for j := range out {
			a := make([]string, len(ws))
			for i := range ws {
				a[i] = ws[i][j]
			}
			out[j] = f(a...)
		}
		return p.pack(out)
	}
	ptr := func() string {
		if _, ok := args[0].(*ast.Ident); ok {
			return exprString(args[0])
		}
		return "(" + exprString(args[0]) + ")"
	}
	le := func(off, size int) string { // the little-endian unsigned integer of size bytes at off of the pointer
		var parts []string
		for i := range size {
			parts = append(parts, laneType(size*8)+"("+ptr()+"["+strconv.Itoa(off+i)+"])<<"+strconv.Itoa(8*i))
		}
		return "(" + strings.Join(parts, "|") + ")"
	}
	switch name {
	case "not":
		return bitwise(func(a ...string) string { return "^" + a[0] }, args[0])
	case "and":
		return bitwise(func(a ...string) string { return a[0] + "&" + a[1] }, args[0], args[1])
	case "andnot":
		return bitwise(func(a ...string) string { return a[0] + "&^" + a[1] }, args[0], args[1])
	case "or":
		return bitwise(func(a ...string) string { return a[0] + "|" + a[1] }, args[0], args[1])
	case "xor":
		return bitwise(func(a ...string) string { return a[0] + "^" + a[1] }, args[0], args[1])
	case "bitselect":
		return bitwise(func(a ...string) string { return a[0] + "&" + a[2] + "|" + a[1] + "&^" + a[2] }, args[0], args[1], args[2])
	case "any_true":
		w := p.words(args[0])
		return "lane_bool(" + w[0] + "|" + w[1] + "|" + w[2] + "|" + w[3] + " != 0)"
	case "const":
		var w [4]string
		for i := range w {
			w[i] = "lane_u32(" + exprString(args[i]) + ")"
		}
		return p.pack(w)
	case "load":
		return p.pack([4]string{le(0, 4), le(4, 4), le(8, 4), le(12, 4)})
	case "from_bytes": // the vector of the bytes of an addressable [16]byte
		return p.pack([4]string{le(0, 4), le(4, 4), le(8, 4), le(12, 4)})
	case "bytes": // the [16]byte of a vector
		w := p.words(args[0])
		var b []string
		for i := range 16 {
			b = append(b, "byte("+shifted(w[i/4], 8*(i%4))+")")
		}
		return "[16]byte{" + strings.Join(b, ", ") + "}"
	case "load8x8_s", "load8x8_u", "load16x4_s", "load16x4_u", "load32x2_s", "load32x2_u":
		from, _ := strconv.Atoi(name[4:strings.IndexByte(name, 'x')])
		width := from * 2
		var out []string
		for j := range 64 / from {
			a := le(j*from/8, from/8)
			if name[len(name)-1] == 's' {
				out = append(out, laneType(width)+"("+signed(signed(a, from), width)+")")
			} else {
				out = append(out, laneType(width)+"("+a+")")
			}
		}
		return p.pack(packLanes(out, width))
	case "load8_splat", "load16_splat", "load32_splat", "load64_splat":
		size, _ := strconv.Atoi(name[4:strings.IndexByte(name, '_')])
		l := make([]string, 128/size)
		for j := range l {
			l[j] = le(0, size/8)
		}
		return p.pack(packLanes(l, size))
	case "load32_zero":
		return p.pack([4]string{le(0, 4), "0", "0", "0"})
	case "load64_zero":
		return p.pack([4]string{le(0, 4), le(4, 4), "0", "0"})
	case "load8_lane", "load16_lane", "load32_lane", "load64_lane":
		size, _ := strconv.Atoi(name[4:strings.IndexByte(name, '_')])
		lanes := lanesOf(p.words(args[1]), size)
		lanes[laneIndex(args[2])] = le(0, size/8)
		return p.pack(packLanes(lanes, size))
	}
	panic("no portable code for SIMD operation v128_" + name)
}

// The statements of a store, through the pointer variable args[0].
func (p simdPortable) store(op string, args []ast.Expr) []string {
	ptr := exprString(args[0])
	put := func(off, size int, v string) []string {
		var s []string
		for i := range size {
			s = append(s, ptr+"["+strconv.Itoa(off+i)+"] = byte("+shifted(v, 8*i)+")")
		}
		return s
	}
	w := p.words(args[1])
	if op == "v128_store" {
		var s []string
		for i := range w {
			s = append(s, put(4*i, 4, w[i])...)
		}
		return s
	}
	size, _ := strconv.Atoi(op[len("v128_store") : len(op)-len("_lane")])
	return put(0, size/8, lanesOf(w, size)[laneIndex(args[2])])
}
