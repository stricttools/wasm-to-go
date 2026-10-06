package passes

import (
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
)

// The domains of the portable code's vectors: a vector of float lanes
// held as floats (SIMDTypeF32 or SIMDTypeF64), or as the words of its bits
// (SIMDType). A variable whose every value is the result of a float
// operation of one lane shape holds it as floats, so a chain of float
// operations through variables (a loop's accumulator) stays in float
// registers; the words of its bits are moved to and from integer registers
// at every lane of every operation. Every other use converts.
//
// Why: basic-pitch's inference took 1,896 ms in the portable code with
// every vector as words, and takes 1,033 ms with float vectors, where the
// build without SIMD takes 782 ms (linux/amd64, medians of interleaved
// runs).

// The domain of the result of the SIMD operation op, after SIMDPrefix:
// "f32" or "f64" for the float operations of the portable code, "bits"
// for the others.
func simdResultDomain(op string) string {
	shape, name, _ := strings.Cut(op, "_")
	switch name {
	case "add", "sub", "mul", "div", "sqrt", "ceil", "floor", "trunc", "nearest",
		"min", "max", "pmin", "pmax", "abs", "neg", "splat", "replace_lane", "canon":
		switch shape {
		case "f32x4":
			return "f32"
		case "f64x2":
			return "f64"
		}
	}
	switch op {
	case "f32x4_convert_i32x4_s", "f32x4_convert_i32x4_u", "f32x4_demote_f64x2_zero":
		return "f32"
	case "f64x2_convert_low_i32x4_s", "f64x2_convert_low_i32x4_u", "f64x2_promote_low_f32x4":
		return "f64"
	}
	return "bits"
}

// portableDomains returns the domain of each vector variable of fn: the
// domain of all its values, or bits where they differ, or where it is a
// parameter (the function's signature holds SIMDType). A constant or the
// zero of a declaration fits any domain.
func portableDomains(fn *ast.FuncDecl) map[string]string {
	defs := map[string][]ast.Expr{}
	fixed := map[string]bool{}
	vectors := map[string]bool{}
	if fn.Type.Params != nil {
		for _, f := range fn.Type.Params.List {
			if id, ok := f.Type.(*ast.Ident); ok && id.Name == SIMDType {
				for _, n := range f.Names {
					vectors[n.Name] = true
					fixed[n.Name] = true
				}
			}
		}
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ValueSpec:
			if id, ok := n.Type.(*ast.Ident); ok && id.Name == SIMDType {
				for i, name := range n.Names {
					vectors[name.Name] = true
					if i < len(n.Values) {
						defs[name.Name] = append(defs[name.Name], n.Values[i])
					} else {
						defs[name.Name] = append(defs[name.Name], nil)
					}
				}
			}
		case *ast.AssignStmt:
			if len(n.Lhs) != len(n.Rhs) {
				for _, l := range n.Lhs {
					if id, ok := l.(*ast.Ident); ok {
						fixed[id.Name] = true
					}
				}
				break
			}
			for i, l := range n.Lhs {
				id, ok := l.(*ast.Ident)
				if !ok || id.Name == "_" {
					continue
				}
				defs[id.Name] = append(defs[id.Name], n.Rhs[i])
				if n.Tok == token.DEFINE && SIMDVector(n.Rhs[i]) {
					vectors[id.Name] = true
				}
			}
		}
		return true
	})
	// Variables defined from other vector variables are vectors too.
	for changed := true; changed; {
		changed = false
		for v, es := range defs {
			if vectors[v] {
				continue
			}
			for _, e := range es {
				if id, ok := e.(*ast.Ident); ok && vectors[id.Name] {
					vectors[v] = true
					changed = true
					break
				}
			}
		}
	}
	domains := map[string]string{}
	for changed := true; changed; {
		changed = false
		for v := range vectors {
			d := ""
			if fixed[v] {
				d = "bits"
			}
			for _, e := range defs[v] {
				var de string
				switch {
				case e == nil:
				case simdOpName(e) == "v128_const":
				case simdOpName(e) != "":
					de = simdResultDomain(simdOpName(e))
				default:
					if id, ok := e.(*ast.Ident); ok && vectors[id.Name] {
						de = domains[id.Name]
					} else {
						de = "bits"
					}
				}
				switch {
				case de == "", de == d:
				case d == "":
					d = de
				default:
					d = "bits"
				}
			}
			if d != "" && d != domains[v] && domains[v] != "bits" {
				domains[v] = d
				changed = true
			}
		}
	}
	for v := range vectors {
		if domains[v] == "" {
			domains[v] = "bits"
		}
	}
	return domains
}

// The vector expression e converted to domain to.
func (p simdPortable) convert(e ast.Expr, to string) ast.Expr {
	switch to {
	case "f32":
		return mustParseExpr(p.packFloats(p.floats(e, 32), 32))
	case "f64":
		return mustParseExpr(p.packFloats(p.floats(e, 64), 64))
	}
	return mustParseExpr(p.pack(p.words(e)))
}

// convertUses writes the portable code's float vectors in their types
// (their declarations, as SIMDTypeF32 or SIMDTypeF64), and converts every
// value where the domain it is used in differs: an assignment to a
// variable of another domain, a call's operand, a result (the function's
// signature holds SIMDType).
func (p simdPortable) convertUses(fn *ast.FuncDecl) {
	results := map[int]bool{}
	if fn.Type.Results != nil {
		i := 0
		for _, f := range fn.Type.Results.List {
			isVec := false
			if id, ok := f.Type.(*ast.Ident); ok && id.Name == SIMDType {
				isVec = true
			}
			for range max(len(f.Names), 1) {
				results[i] = isVec
				i++
			}
		}
	}
	vector := func(e ast.Expr) bool {
		if id, ok := e.(*ast.Ident); ok {
			_, ok := p.domains[id.Name]
			return ok
		}
		if lit, ok := e.(*ast.CompositeLit); ok {
			if id, ok := lit.Type.(*ast.Ident); ok {
				return id.Name == SIMDType || id.Name == SIMDTypeF32 || id.Name == SIMDTypeF64
			}
		}
		return false
	}
	astutil.Apply(fn.Body, nil, func(c *astutil.Cursor) bool {
		switch n := c.Node().(type) {
		case *ast.AssignStmt:
			if len(n.Lhs) != len(n.Rhs) {
				break
			}
			for i, l := range n.Lhs {
				id, ok := l.(*ast.Ident)
				if !ok || id.Name == "_" || !vector(n.Rhs[i]) {
					continue
				}
				if want := p.domains[id.Name]; want != "" && p.domain(n.Rhs[i]) != want {
					n.Rhs[i] = p.convert(n.Rhs[i], want)
				}
			}
		case *ast.ReturnStmt:
			for i, r := range n.Results {
				if results[i] && vector(r) && p.domain(r) != "bits" {
					n.Results[i] = p.convert(r, "bits")
				}
			}
		case *ast.CallExpr:
			for i, a := range n.Args {
				if vector(a) && p.domain(a) != "bits" {
					n.Args[i] = p.convert(a, "bits")
				}
			}
		}
		return true
	})
	// The declarations, a vector variable's in its domain's type.
	types := map[string]string{"f32": SIMDTypeF32, "f64": SIMDTypeF64, "bits": SIMDType}
	postApplyStmts(fn.Body, func(list []ast.Stmt) []ast.Stmt {
		var out []ast.Stmt
		for _, s := range list {
			ds, ok := s.(*ast.DeclStmt)
			if !ok {
				out = append(out, s)
				continue
			}
			gd := ds.Decl.(*ast.GenDecl)
			if len(gd.Specs) != 1 {
				out = append(out, s)
				continue
			}
			vs, ok := gd.Specs[0].(*ast.ValueSpec)
			id, isID := vs.Type.(*ast.Ident)
			if !ok || !isID || id.Name != SIMDType || len(vs.Values) > 0 {
				out = append(out, s)
				continue
			}
			byDomain := map[string][]*ast.Ident{}
			var order []string
			for _, n := range vs.Names {
				d := p.domains[n.Name]
				if d == "" {
					d = "bits"
				}
				if byDomain[d] == nil {
					order = append(order, d)
				}
				byDomain[d] = append(byDomain[d], n)
			}
			for _, d := range order {
				out = append(out, &ast.DeclStmt{Decl: &ast.GenDecl{Tok: token.VAR, Specs: []ast.Spec{
					&ast.ValueSpec{Names: byDomain[d], Type: ast.NewIdent(types[d])}}}})
			}
		}
		return out
	})
}
