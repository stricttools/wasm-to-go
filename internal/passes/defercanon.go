package passes

import (
	"go/ast"
	"go/token"
	"math"
	"strconv"
	"strings"
)

// The canonicalizing helpers, by the domain they canonicalize: a float
// type, or the lane shape of a vector (SIMDType) whose lanes they
// canonicalize.
var canonHelpers = map[string]string{
	"f32_canon": "float32", "f64_canon": "float64",
	SIMDPrefix + "f32x4_canon": "f32x4", SIMDPrefix + "f64x2_canon": "f64x2",
}

// CanonHelper is the canonicalizing helper of each float type.
var CanonHelper = map[string]string{"float32": "f32_canon", "float64": "f64_canon"}

// The canonicalizing helper of each domain.
var canonOfDomain = map[string]string{
	"float32": "f32_canon", "float64": "f64_canon",
	"f32x4": SIMDPrefix + "f32x4_canon", "f64x2": SIMDPrefix + "f64x2_canon",
}

// NaNBlind reports whether helper's result is the same for every NaN
// operand: min and max, which return the canonical NaN for any NaN, and
// the conversions to integer, which trap or saturate alike for any NaN.
func NaNBlind(helper string) bool { return nanBlindHelpers.has(helper) }

var nanBlindHelpers = set[string]{
	"f32_min": {}, "f32_max": {}, "f64_min": {}, "f64_max": {},
	"i32_trunc_f32_s": {}, "i32_trunc_f32_u": {}, "i32_trunc_f64_s": {}, "i32_trunc_f64_u": {},
	"i64_trunc_f32_s": {}, "i64_trunc_f32_u": {}, "i64_trunc_f64_s": {}, "i64_trunc_f64_u": {},
	"i32_trunc_sat_f32_s": {}, "i32_trunc_sat_f32_u": {},
	"i32_trunc_sat_f64_s": {}, "i32_trunc_sat_f64_u": {},
	"i64_trunc_sat_f32_s": {}, "i64_trunc_sat_f32_u": {},
	"i64_trunc_sat_f64_s": {}, "i64_trunc_sat_f64_u": {},
}

// The helpers whose result is canonical: a NaN result is the canonical NaN.
var canonResultHelpers = set[string]{
	"f32_min": {}, "f32_max": {}, "f64_min": {}, "f64_max": {},
}

// The helpers that convert an integer to a float, which is never a NaN.
var intToFloatHelpers = set[string]{"f32_convert_i64_s": {}, "f32_convert_i64_u": {}}

// The math functions whose result is a NaN exactly when their operand is,
// and otherwise a function of the operand's value: under a
// canonicalization, the NaN their operand is cannot be told apart.
var nanPropagatingMath = set[string]{"Sqrt": {}, "Floor": {}, "Ceil": {}, "Trunc": {}, "RoundToEven": {}}

// DeferCanon defers the canonicalization of float variables to their uses,
// and returns the number of variables it deferred.
//
// The translator canonicalizes each float operation's result where it is
// computed, f32_canon(float32(x + y)), and each float lane of a vector
// operation's, simd_f32x4_canon(simd_f32x4_add(x, y)). A variable
// assigned such a result can hold the raw result instead, x + y, when
// every value assigned to it is a canonicalized result (of its float type,
// or for a vector of one lane shape), a constant that is not a NaN other
// than the canonical one (in each lane), a min or max (canonical already),
// an integer converted to a float (never a NaN), the zero of its
// declaration, or another such variable. Its uses then take it raw where
// the NaN they see cannot change their result, and canonicalized
// everywhere else:
//
//   - raw: the operands of arithmetic (+, -, *, /, the NaN-propagating
//     math functions, conversions between float types, and the vector
//     operations of the lane shape that canonicalize their result) whose
//     own result is taken raw or canonicalized; operands of comparisons;
//     operands of the canonicalizing, min and max, and conversion-to-integer
//     helpers and vector operations of the lane shape; values assigned to
//     another such variable or discarded (_ = x);
//   - canonicalized: everything else, among them stores, reinterpretations
//     (math.Float32bits), calls, returns, globals, neg, abs, copysign, and
//     the vector operations that move lanes or bits (shuffles, bitwise
//     operations, extract_lane, replace_lane, pmin and pmax).
//
// A variable is deferred only if some raw result reaches it: one holding
// only constants and helper results gains nothing.
//
// Why: in a loop body, a float computed into a local and used by the next
// operation was canonicalized at every step (a compare and a branch per
// operation, a compare and a blend per vector operation); deferred, a chain
// of operations through locals canonicalizes once, where its value leaves
// the chain. basic-pitch's onnxruntime build, with the locals split (see
// the translator's splitLocals), went from 1,098 ms to 781 ms per
// inference on linux/amd64 (medians of interleaved runs).
//
// # Why this preserves behavior
//
// Every value assigned to a deferred variable, raw, canonicalizes to the
// value the variable held before: a canonicalized result's raw form does
// by definition, and the other values are their own canonical form. So
// the variable's raw value (each lane of it) is a NaN exactly when its
// value was, and otherwise the same bits. A raw use is one whose result is
// the same for every NaN and for equal non-NaN values: arithmetic gives a
// NaN for a NaN operand, which the canonicalization above it (on every
// path the use reaches) makes the canonical NaN; a comparison is false (or
// true for !=) for every NaN; min and max return the canonical NaN, and
// the conversions to integer trap or saturate the same way for every NaN;
// an assignment to another deferred variable keeps the invariant. A vector
// is raw only where its lanes are read in the lane shape it was deferred
// in. Every other use canonicalizes first, and gets the bits the variable
// held.
//
// The pass works on the function's own variables, never shadowed in
// generated code, and leaves a function alone when one of its float
// variables appears inside a function literal.
func DeferCanon(fn *ast.FuncDecl) int {
	if fn.Body == nil {
		return 0
	}
	d := &deferrer{types: map[string]string{}, defs: map[string][]ast.Expr{}, params: set[string]{}}
	if fn.Type.Params != nil {
		for _, f := range fn.Type.Params.List {
			if id, ok := f.Type.(*ast.Ident); ok {
				for _, n := range f.Names {
					d.types[n.Name] = id.Name
					d.params.add(n.Name)
				}
			}
		}
	}
	if !d.collect(fn.Body) {
		return 0
	}
	d.solve()
	if len(d.deferred) == 0 {
		return 0
	}
	for _, s := range fn.Body.List {
		d.stmt(s)
	}
	return len(d.deferred)
}

type deferrer struct {
	types    map[string]string     // the Go type of each variable whose type the pass knows
	defs     map[string][]ast.Expr // the values assigned to each variable; nil for a declaration's zero
	unsafe   set[string]           // variables assigned something the pass cannot follow
	params   set[string]           // parameters, whose values come from the caller
	inLit    set[string]           // variables appearing in function literals
	domain   map[string]string     // each candidate's domain: its float type, or a vector's lane shape
	deferred map[string]string     // the deferred variables and their domains
}

// collect records every variable's type and assigned values, and reports
// whether the function holds only statements the pass knows.
func (d *deferrer) collect(body *ast.BlockStmt) bool {
	d.unsafe = set[string]{}
	d.inLit = set[string]{}
	known := true
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			ast.Inspect(n, func(m ast.Node) bool {
				if id, ok := m.(*ast.Ident); ok {
					d.inLit.add(id.Name)
				}
				return true
			})
			return false
		case *ast.ValueSpec:
			typ := ""
			if id, ok := n.Type.(*ast.Ident); ok {
				typ = id.Name
			}
			for i, id := range n.Names {
				if typ != "" {
					d.types[id.Name] = typ
				}
				if i < len(n.Values) {
					d.defs[id.Name] = append(d.defs[id.Name], n.Values[i])
				} else if len(n.Values) > 0 {
					d.unsafe.add(id.Name)
				} else {
					d.defs[id.Name] = append(d.defs[id.Name], nil)
				}
			}
		case *ast.AssignStmt:
			if n.Tok != token.ASSIGN && n.Tok != token.DEFINE {
				for _, l := range n.Lhs {
					if id, ok := l.(*ast.Ident); ok {
						d.unsafe.add(id.Name)
					}
				}
				break
			}
			for i, l := range n.Lhs {
				id, ok := l.(*ast.Ident)
				if !ok || id.Name == "_" {
					continue
				}
				if len(n.Lhs) != len(n.Rhs) {
					d.unsafe.add(id.Name)
					continue
				}
				if n.Tok == token.DEFINE {
					if t := d.exprType(n.Rhs[i]); t != "" {
						d.types[id.Name] = t
					}
				}
				d.defs[id.Name] = append(d.defs[id.Name], n.Rhs[i])
			}
		case *ast.IncDecStmt:
			if id, ok := n.X.(*ast.Ident); ok {
				d.unsafe.add(id.Name)
			}
		case *ast.UnaryExpr:
			if id, ok := n.X.(*ast.Ident); ok && n.Op == token.AND {
				d.unsafe.add(id.Name)
			}
		case *ast.RangeStmt, *ast.TypeSwitchStmt, *ast.SelectStmt, *ast.GoStmt, *ast.DeferStmt, *ast.SendStmt:
			known = false
		}
		return known
	})
	return known
}

// The Go type of e, where the pass can tell it: a known variable's, a
// conversion's, the float helpers' and math functions' results, and a
// vector operation's vector.
func (d *deferrer) exprType(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return d.exprType(e.X)
	case *ast.Ident:
		return d.types[e.Name]
	case *ast.CallExpr:
		if SIMDVector(e) {
			return SIMDType
		}
		switch f := e.Fun.(type) {
		case *ast.Ident:
			if _, ok := intRanges[f.Name]; ok || f.Name == "float32" || f.Name == "float64" {
				return f.Name
			}
			if t, ok := canonHelpers[f.Name]; ok && isFloat(t) {
				return t
			}
			switch f.Name {
			case "f32_min", "f32_max", "f32_abs", "f32_neg", "f32_copysign", "f32_convert_i64_s", "f32_convert_i64_u":
				return "float32"
			case "f64_min", "f64_max", "f64_abs", "f64_neg":
				return "float64"
			}
		case *ast.SelectorExpr:
			if pkg, ok := f.X.(*ast.Ident); ok && pkg.Name == "math" {
				switch f.Sel.Name {
				case "Float32frombits":
					return "float32"
				case "Float64frombits", "Copysign", "Sqrt", "Floor", "Ceil", "Trunc", "RoundToEven":
					return "float64"
				case "Float32bits":
					return "uint32"
				case "Float64bits":
					return "uint64"
				}
			}
		}
	case *ast.BinaryExpr:
		switch e.Op {
		case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ, token.LAND, token.LOR:
			return "bool"
		case token.SHL, token.SHR:
			return d.exprType(e.X)
		}
		if t := d.exprType(e.X); t != "" {
			return t
		}
		return d.exprType(e.Y)
	case *ast.UnaryExpr:
		if e.Op != token.AND && e.Op != token.ARROW {
			return d.exprType(e.X)
		}
	}
	return ""
}

func isFloat(t string) bool { return t == "float32" || t == "float64" }

func isInt(t string) bool {
	_, ok := intRanges[t]
	return ok
}

// The domain of a canonicalization call e (its helper's), or "".
func canonCall(e ast.Expr) string {
	if c, ok := unparen(e).(*ast.CallExpr); ok && len(c.Args) == 1 {
		if id, ok := c.Fun.(*ast.Ident); ok {
			return canonHelpers[id.Name]
		}
	}
	return ""
}

// The name of the vector operation e calls (after SIMDPrefix), or "".
func simdOpName(e ast.Expr) string {
	if c, ok := unparen(e).(*ast.CallExpr); ok {
		if id, ok := c.Fun.(*ast.Ident); ok && IsSIMDOp(id.Name) {
			return id.Name[len(SIMDPrefix):]
		}
	}
	return ""
}

// The vector operations whose result is a vector of shape with no NaN lane
// other than the canonical NaN: min and max, and conversions from integers.
var simdCanonResults = map[string]string{
	"f32x4_min": "f32x4", "f32x4_max": "f32x4", "f64x2_min": "f64x2", "f64x2_max": "f64x2",
	"f32x4_convert_i32x4_s": "f32x4", "f32x4_convert_i32x4_u": "f32x4",
	"f64x2_convert_low_i32x4_s": "f64x2", "f64x2_convert_low_i32x4_u": "f64x2",
}

// solve finds the deferred variables: the float variables whose every
// value is safe (see DeferCanon), assuming the others deferred, as the
// largest such set; then those some raw value reaches.
func (d *deferrer) solve() {
	safe := set[string]{}
	d.domain = map[string]string{}
	for v, t := range d.types {
		if (isFloat(t) || t == SIMDType) && !d.params.has(v) && !d.unsafe.has(v) && !d.inLit.has(v) && len(d.defs[v]) > 0 {
			safe.add(v)
			if isFloat(t) {
				d.domain[v] = t
			}
		}
	}
	// A vector's domain is the lane shape of its canonicalized values and
	// of the vectors assigned to it; a vector of two shapes is not safe.
	for changed := true; changed; {
		changed = false
		for v := range safe {
			if d.types[v] != SIMDType {
				continue
			}
			shapes := set[string]{}
			if s := d.domain[v]; s != "" {
				shapes.add(s)
			}
			for _, e := range d.defs[v] {
				if c := canonCall(e); c != "" {
					shapes.add(c)
				} else if id, ok := unparen(e).(*ast.Ident); ok && safe.has(id.Name) && d.domain[id.Name] != "" {
					shapes.add(d.domain[id.Name])
				} else if s := simdCanonResults[simdOpName(e)]; s != "" {
					shapes.add(s)
				}
			}
			switch {
			case len(shapes) > 1:
				delete(safe, v)
				changed = true
			case len(shapes) == 1 && d.domain[v] == "":
				for s := range shapes {
					d.domain[v] = s
				}
				changed = true
			}
		}
	}
	for v := range safe {
		if d.domain[v] == "" {
			delete(safe, v)
		}
	}
	for changed := true; changed; {
		changed = false
		for v := range safe {
			for _, e := range d.defs[v] {
				if !d.safeValue(e, d.domain[v], safe) {
					delete(safe, v)
					changed = true
					break
				}
			}
		}
	}
	raw := set[string]{}
	for changed := true; changed; {
		changed = false
		for v := range safe {
			if raw.has(v) {
				continue
			}
			for _, e := range d.defs[v] {
				if canonCall(e) != "" {
					raw.add(v)
				} else if id, ok := unparen(e).(*ast.Ident); ok && raw.has(id.Name) {
					raw.add(v)
				}
				if raw.has(v) {
					changed = true
					break
				}
			}
		}
	}
	d.deferred = map[string]string{}
	for v := range raw {
		d.deferred[v] = d.domain[v]
	}
}

func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

// Reports whether e, assigned to a variable of domain dom, is a value
// whose canonical form is itself or, raw, canonicalizes to the variable's
// value: see DeferCanon.
func (d *deferrer) safeValue(e ast.Expr, dom string, safe set[string]) bool {
	if e == nil {
		return true
	}
	e = unparen(e)
	if c := canonCall(e); c != "" {
		return c == dom
	}
	if id, ok := e.(*ast.Ident); ok {
		return safe.has(id.Name) && d.domain[id.Name] == dom
	}
	if !isFloat(dom) {
		op := simdOpName(e)
		if simdCanonResults[op] == dom {
			return true
		}
		if op == "v128_const" {
			for i, a := range e.(*ast.CallExpr).Args {
				v, err := strconv.ParseUint(exprString(a), 0, 32)
				if err != nil {
					return false
				}
				switch dom {
				case "f32x4":
					f := math.Float32frombits(uint32(v))
					if f != f && v != 0x7fc00000 {
						return false
					}
				case "f64x2":
					if i%2 == 1 {
						lo, err := strconv.ParseUint(exprString(e.(*ast.CallExpr).Args[i-1]), 0, 32)
						if err != nil {
							return false
						}
						b := v<<32 | lo
						if f := math.Float64frombits(b); f != f && b != 0x7ff8000000000000 {
							return false
						}
					}
				}
			}
			return true
		}
		return false
	}
	typ := dom
	switch e := e.(type) {
	case *ast.CallExpr:
		switch f := e.Fun.(type) {
		case *ast.Ident:
			if canonResultHelpers.has(f.Name) || intToFloatHelpers.has(f.Name) {
				return d.exprType(e) == typ
			}
			if f.Name == typ && len(e.Args) == 1 {
				return isInt(d.exprType(e.Args[0]))
			}
		case *ast.SelectorExpr:
			pkg, ok := f.X.(*ast.Ident)
			if !ok || pkg.Name != "math" || len(e.Args) != 1 {
				return false
			}
			lit, ok := e.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.INT {
				return false
			}
			v, err := strconv.ParseUint(lit.Value, 0, 64)
			if err != nil {
				return false
			}
			switch {
			case f.Sel.Name == "Float32frombits" && typ == "float32":
				f := math.Float32frombits(uint32(v))
				return f == f || uint32(v) == 0x7fc00000
			case f.Sel.Name == "Float64frombits" && typ == "float64":
				f := math.Float64frombits(v)
				return f == f || v == 0x7ff8000000000000
			}
		}
	}
	return false
}

// The blindness of a position: where a value is raw (see DeferCanon).
// "scalar" makes deferred float variables raw, a lane shape deferred
// vectors of that shape, "all" every deferred variable, and "" none.
func rawIn(dom, blind string) bool {
	return blind == "all" || blind == dom || blind == "scalar" && isFloat(dom)
}

// The blindness of an assignment to a deferred variable of domain dom.
func assignBlind(dom string) string {
	if isFloat(dom) {
		return "scalar"
	}
	return dom
}

func (d *deferrer) stmts(list []ast.Stmt) {
	for _, s := range list {
		d.stmt(s)
	}
}

func (d *deferrer) stmt(s ast.Stmt) {
	switch s := s.(type) {
	case *ast.AssignStmt:
		paired := len(s.Lhs) == len(s.Rhs)
		for i := range s.Rhs {
			blind := ""
			if paired {
				if id, ok := s.Lhs[i].(*ast.Ident); ok {
					if dom, deferred := d.deferred[id.Name]; deferred {
						blind = assignBlind(dom)
						if canonCall(s.Rhs[i]) == dom {
							s.Rhs[i] = unparen(s.Rhs[i]).(*ast.CallExpr).Args[0]
						}
					} else if id.Name == "_" {
						blind = "all"
					}
				}
			}
			s.Rhs[i] = d.expr(s.Rhs[i], blind)
		}
		for i, l := range s.Lhs {
			if _, ok := l.(*ast.Ident); !ok {
				s.Lhs[i] = d.expr(l, "")
			}
		}
	case *ast.ExprStmt:
		s.X = d.expr(s.X, "")
	case *ast.ReturnStmt:
		for i := range s.Results {
			s.Results[i] = d.expr(s.Results[i], "")
		}
	case *ast.BlockStmt:
		d.stmts(s.List)
	case *ast.LabeledStmt:
		d.stmt(s.Stmt)
	case *ast.IfStmt:
		if s.Init != nil {
			d.stmt(s.Init)
		}
		s.Cond = d.expr(s.Cond, "")
		d.stmts(s.Body.List)
		if s.Else != nil {
			d.stmt(s.Else)
		}
	case *ast.SwitchStmt:
		if s.Init != nil {
			d.stmt(s.Init)
		}
		if s.Tag != nil {
			s.Tag = d.expr(s.Tag, "")
		}
		for _, c := range s.Body.List {
			cc := c.(*ast.CaseClause)
			for i := range cc.List {
				cc.List[i] = d.expr(cc.List[i], "")
			}
			d.stmts(cc.Body)
		}
	case *ast.ForStmt:
		if s.Init != nil {
			d.stmt(s.Init)
		}
		if s.Cond != nil {
			s.Cond = d.expr(s.Cond, "")
		}
		if s.Post != nil {
			d.stmt(s.Post)
		}
		d.stmts(s.Body.List)
	case *ast.DeclStmt:
		gd := s.Decl.(*ast.GenDecl)
		for _, sp := range gd.Specs {
			if vs, ok := sp.(*ast.ValueSpec); ok {
				for i := range vs.Values {
					blind := ""
					if len(vs.Values) == len(vs.Names) {
						if dom, deferred := d.deferred[vs.Names[i].Name]; deferred {
							blind = assignBlind(dom)
							if canonCall(vs.Values[i]) == dom {
								vs.Values[i] = unparen(vs.Values[i]).(*ast.CallExpr).Args[0]
							}
						}
					}
					vs.Values[i] = d.expr(vs.Values[i], blind)
				}
			}
		}
	case *ast.IncDecStmt:
		s.X = d.expr(s.X, "")
	}
	// BranchStmt and EmptyStmt hold no expressions; collect refused the
	// statements this switch does not list.
}

// The blindness of the operands of the vector operation op in a position
// of blindness blind.
func simdOperandBlind(op, blind string) string {
	shape, name, _ := strings.Cut(op, "_")
	switch name {
	case "canon":
		return shape
	case "eq", "ne", "lt", "gt", "le", "ge", "min", "max":
		if shape[0] == 'f' {
			return shape
		}
	case "add", "sub", "mul", "div", "sqrt", "ceil", "floor", "trunc", "nearest":
		if shape[0] == 'f' && blind == shape {
			return shape
		}
	case "demote_f64x2_zero":
		if blind == shape {
			return "f64x2"
		}
	case "promote_low_f32x4":
		if blind == shape {
			return "f32x4"
		}
	case "trunc_sat_f32x4_s", "trunc_sat_f32x4_u":
		return "f32x4"
	case "trunc_sat_f64x2_s_zero", "trunc_sat_f64x2_u_zero":
		return "f64x2"
	}
	return ""
}

// e with each use of a deferred variable canonicalized unless e's value
// is taken where the NaN it is cannot be told apart (blind: see rawIn).
func (d *deferrer) expr(e ast.Expr, blind string) ast.Expr {
	switch x := e.(type) {
	case *ast.Ident:
		if dom, ok := d.deferred[x.Name]; ok && !rawIn(dom, blind) {
			return &ast.CallExpr{Fun: ast.NewIdent(canonOfDomain[dom]), Args: []ast.Expr{x}}
		}
	case *ast.ParenExpr:
		x.X = d.expr(x.X, blind)
	case *ast.BinaryExpr:
		opBlind := ""
		switch x.Op {
		case token.ADD, token.SUB, token.MUL, token.QUO:
			if blind == "scalar" && isFloat(d.exprType(x)) {
				opBlind = "scalar"
			}
		case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
			if isFloat(d.exprType(x.X)) || isFloat(d.exprType(x.Y)) {
				opBlind = "scalar"
			}
		}
		x.X = d.expr(x.X, opBlind)
		x.Y = d.expr(x.Y, opBlind)
	case *ast.CallExpr:
		argBlind := ""
		switch f := x.Fun.(type) {
		case *ast.Ident:
			switch {
			case IsSIMDOp(f.Name):
				argBlind = simdOperandBlind(f.Name[len(SIMDPrefix):], blind)
			case isFloat(canonHelpers[f.Name]), nanBlindHelpers.has(f.Name):
				argBlind = "scalar"
			case (f.Name == "float32" || f.Name == "float64") && len(x.Args) == 1 && isFloat(d.exprType(x.Args[0])):
				if blind == "scalar" {
					argBlind = "scalar"
				}
			}
		case *ast.SelectorExpr:
			if pkg, ok := f.X.(*ast.Ident); ok && pkg.Name == "math" && nanPropagatingMath.has(f.Sel.Name) {
				if blind == "scalar" {
					argBlind = "scalar"
				}
			} else {
				x.Fun = d.expr(x.Fun, "")
			}
		default:
			x.Fun = d.expr(x.Fun, "")
		}
		for i := range x.Args {
			x.Args[i] = d.expr(x.Args[i], argBlind)
		}
	case *ast.IndexExpr:
		x.X = d.expr(x.X, "")
		x.Index = d.expr(x.Index, "")
	case *ast.SliceExpr:
		x.X = d.expr(x.X, "")
		if x.Low != nil {
			x.Low = d.expr(x.Low, "")
		}
		if x.High != nil {
			x.High = d.expr(x.High, "")
		}
		if x.Max != nil {
			x.Max = d.expr(x.Max, "")
		}
	case *ast.UnaryExpr:
		x.X = d.expr(x.X, "")
	case *ast.StarExpr:
		x.X = d.expr(x.X, "")
	case *ast.TypeAssertExpr:
		x.X = d.expr(x.X, "")
	case *ast.SelectorExpr:
		x.X = d.expr(x.X, "")
	case *ast.CompositeLit:
		for i := range x.Elts {
			x.Elts[i] = d.expr(x.Elts[i], "")
		}
	case *ast.KeyValueExpr:
		x.Value = d.expr(x.Value, "")
	}
	return e
}
