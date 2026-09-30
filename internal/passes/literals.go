package passes

import (
	"go/ast"
	"go/token"
	"math"
	"strconv"
	"strings"
)

// The integer types a constant may be converted to, with their ranges.
var intRanges = map[string][2]float64{
	"int8": {math.MinInt8, math.MaxInt8}, "int16": {math.MinInt16, math.MaxInt16},
	"int32": {math.MinInt32, math.MaxInt32}, "int64": {math.MinInt64, math.MaxInt64},
	"uint8": {0, math.MaxUint8}, "byte": {0, math.MaxUint8}, "uint16": {0, math.MaxUint16},
	"uint32": {0, math.MaxUint32}, "uint64": {0, math.MaxUint64},
}

// Literals writes the integer constants the translator wraps in the i32
// and i64 helpers, i32(5), as Go constants, int32(5), wherever the
// constant cannot become an operand of a Go constant expression, and
// returns the number it wrote.
//
// Why: i32 and i64 keep constants away from Go's constant evaluator
// (see const.go), but each is an inlined call, which gives the Go
// compiler another named variable and inline mark to track, and a
// module has tens of thousands of constants. Most are operands of an
// operation whose other operand is a variable, where no constant
// expression can form.
//
// # Why this preserves behavior
//
// A constant is written as int32(5) only if, after any chain of
// conversions to integer types that can represent its value, it is an
// operand of a binary operation whose other operand is not a constant (a
// variable, a field, or the result of a function call), not the divisor
// of a division or remainder when it is zero, and not a negative shift
// count; or an argument of a function call that is not a conversion or a
// builtin, other than the address or value of a memory access, which Lower
// and Expand turn into an index or shifted bytes; or the value of an assignment, a return, or a variable
// declaration. In each of these a Go constant is converted to the type
// the operation or the destination needs, int32 or int64 as with the
// helper, and the operation is evaluated at run time, as the helper's
// result would be: the same value, the same wrapping, and the same traps.
// The conversions of the chain are of a constant the type can represent,
// so Go's constant conversion gives the value the run-time conversion
// would. Anything else keeps the helper.
func Literals(fn *ast.FuncDecl) int {
	if fn.Body == nil {
		return 0
	}
	locals := declaredNames(fn)
	var sites int
	var stack []ast.Node
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		stack = append(stack, n)
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		typ, v, ok := helperConst(call)
		if !ok || !literalSafe(stack, v, locals) {
			return true
		}
		call.Fun = ast.NewIdent(typ)
		sites++
		return true
	})
	return sites
}

// Returns the type and value of i32(L) or i64(L).
func helperConst(call *ast.CallExpr) (typ string, v float64, ok bool) {
	id, ok := call.Fun.(*ast.Ident)
	if !ok || len(call.Args) != 1 {
		return "", 0, false
	}
	switch id.Name {
	case "i32":
		typ = "int32"
	case "i64":
		typ = "int64"
	default:
		return "", 0, false
	}
	s, neg := "", false
	switch a := call.Args[0].(type) {
	case *ast.BasicLit:
		s = a.Value
	case *ast.UnaryExpr:
		lit, ok := a.X.(*ast.BasicLit)
		if !ok || a.Op != token.SUB {
			return "", 0, false
		}
		s, neg = lit.Value, true
	default:
		return "", 0, false
	}
	if rest, ok := strings.CutPrefix(s, "-"); ok {
		s, neg = rest, !neg
	}
	u, err := strconv.ParseUint(s, 0, 64)
	if err != nil {
		return "", 0, false
	}
	v = float64(u)
	if neg {
		v = -v
	}
	return typ, v, true
}

// Reports whether the constant ending stack (its ancestors first), of
// value v, can be written as a Go constant: see Literals.
func literalSafe(stack []ast.Node, v float64, locals set[string]) bool {
	i := len(stack) - 1
	// Climb the conversions to integer types that can represent v.
	for i > 0 {
		conv, ok := stack[i-1].(*ast.CallExpr)
		if !ok || len(conv.Args) != 1 || conv.Args[0] != stack[i] {
			break
		}
		id, ok := conv.Fun.(*ast.Ident)
		if !ok {
			return false
		}
		r, ok := intRanges[id.Name]
		if !ok || v < r[0] || v > r[1] {
			return false
		}
		i--
	}
	if i == 0 {
		return false
	}
	self := stack[i].(ast.Expr)
	switch p := stack[i-1].(type) {
	case *ast.BinaryExpr:
		other := p.X
		if p.X == self {
			other = p.Y
		}
		if possiblyConst(other, locals) {
			return false
		}
		if p.Y == self {
			switch p.Op {
			case token.QUO, token.REM:
				return v != 0
			case token.SHL, token.SHR:
				return v >= 0
			}
		}
		return true
	case *ast.CallExpr:
		if p.Fun == self {
			return false
		}
		switch fun := p.Fun.(type) {
		case *ast.Ident:
			// Lower and Expand make the address of a memory access an
			// index, where a constant must fit in an int, and Lower may
			// shift a stored value and convert it to byte.
			if lowerFuncs[fun.Name] != "" && p.Fun != self {
				return false
			}
			_, conversion := intRanges[fun.Name]
			return !conversion && !builtins.has(fun.Name) && !floatTypes.has(fun.Name)
		case *ast.SelectorExpr:
			// Not unsafe.Sizeof and the like, which are constants.
			pkg, ok := fun.X.(*ast.Ident)
			return !ok || pkg.Name != "unsafe"
		}
		return true
	case *ast.AssignStmt:
		for _, lhs := range p.Lhs {
			if lhs == self {
				return false
			}
		}
		return true
	case *ast.ReturnStmt, *ast.ValueSpec:
		return true
	}
	return false
}

var (
	builtins   = set[string]{"len": {}, "cap": {}, "min": {}, "max": {}, "real": {}, "imag": {}, "complex": {}, "uintptr": {}, "int": {}, "uint": {}}
	floatTypes = set[string]{"float32": {}, "float64": {}}
)

// Reports whether e is, or could be, a Go constant: anything but a
// variable declared in the function (or its receiver), a field or element
// of one, or a call of a function that is not a conversion or a builtin.
func possiblyConst(e ast.Expr, locals set[string]) bool {
	switch e := e.(type) {
	case *ast.Ident:
		return !locals.has(e.Name)
	case *ast.ParenExpr:
		return possiblyConst(e.X, locals)
	case *ast.UnaryExpr:
		return possiblyConst(e.X, locals)
	case *ast.BinaryExpr:
		return possiblyConst(e.X, locals) && possiblyConst(e.Y, locals)
	case *ast.SelectorExpr:
		return possiblyConst(e.X, locals)
	case *ast.IndexExpr, *ast.StarExpr, *ast.TypeAssertExpr, *ast.SliceExpr:
		return false
	case *ast.CallExpr:
		switch fun := e.Fun.(type) {
		case *ast.Ident:
			if _, ok := intRanges[fun.Name]; ok || builtins.has(fun.Name) || floatTypes.has(fun.Name) {
				for _, a := range e.Args {
					if possiblyConst(a, locals) {
						return true
					}
				}
				return false
			}
			return fun.Name == "i32" || fun.Name == "i64"
		case *ast.SelectorExpr:
			pkg, ok := fun.X.(*ast.Ident)
			return ok && pkg.Name == "unsafe"
		}
		return false
	}
	return true
}

// The names fn declares as variables: its receiver, parameters, and
// results, and every variable its body declares.
func declaredNames(fn *ast.FuncDecl) set[string] {
	names := set[string]{}
	for _, fl := range []*ast.FieldList{fn.Recv, fn.Type.Params, fn.Type.Results} {
		if fl == nil {
			continue
		}
		for _, f := range fl.List {
			for _, id := range f.Names {
				names.add(id.Name)
			}
		}
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.ValueSpec:
			for _, id := range n.Names {
				names.add(id.Name)
			}
		case *ast.AssignStmt:
			if n.Tok == token.DEFINE {
				for _, l := range n.Lhs {
					if id, ok := l.(*ast.Ident); ok {
						names.add(id.Name)
					}
				}
			}
		}
		return true
	})
	return names
}
