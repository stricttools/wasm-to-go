package main

import (
	"encoding/binary"
	"go/ast"
	"go/token"
	"strconv"
)

func (t *translator) constI32() (ast.Expr, error) {
	v, err := readSignedLEB128(t.in)
	if err != nil {
		return nil, err
	}

	t.helpers.add("i32")
	// This prevents constant folding/propagation.
	a := []ast.Expr{&ast.BasicLit{Kind: token.INT, Value: formatInt(v)}}
	return &ast.CallExpr{Fun: newID("i32"), Args: a}, nil
}

func (t *translator) constI64() (ast.Expr, error) {
	v, err := readSignedLEB128(t.in)
	if err != nil {
		return nil, err
	}

	t.helpers.add("i64")
	// This prevents constant folding/propagation.
	a := []ast.Expr{&ast.BasicLit{Kind: token.INT, Value: formatInt(v)}}
	return &ast.CallExpr{Fun: newID("i64"), Args: a}, nil
}

// Float constants are written as their bits, math.Float32frombits(0x...)
// and math.Float64frombits(0x...), never as Go constants: Go's constant
// evaluator computes exactly, has no negative zero, and refuses to compile
// an overflow or a division by zero, so an operation on two Go float
// constants would differ from WebAssembly's IEEE 754 arithmetic. The
// compiler still folds operations on these values, with IEEE arithmetic.

func (t *translator) constF32() (ast.Expr, error) {
	var v uint32
	if err := binary.Read(t.in, binary.LittleEndian, &v); err != nil {
		return nil, err
	}
	return &ast.CallExpr{
		Fun:  &ast.SelectorExpr{X: newID("math"), Sel: newID("Float32frombits")},
		Args: []ast.Expr{&ast.BasicLit{Kind: token.INT, Value: "0x" + strconv.FormatUint(uint64(v), 16)}},
	}, nil
}

func (t *translator) constF64() (ast.Expr, error) {
	var v uint64
	if err := binary.Read(t.in, binary.LittleEndian, &v); err != nil {
		return nil, err
	}
	return &ast.CallExpr{
		Fun:  &ast.SelectorExpr{X: newID("math"), Sel: newID("Float64frombits")},
		Args: []ast.Expr{&ast.BasicLit{Kind: token.INT, Value: "0x" + strconv.FormatUint(v, 16)}},
	}, nil
}

func (t *translator) globalGet() (ast.Expr, bool, wasmType, error) {
	v, err := readLEB128(t.in)
	if err != nil {
		return nil, false, 0, err
	}
	global := t.globals[v]
	var expr ast.Expr = &ast.SelectorExpr{
		X:   newID("m"),
		Sel: global.id,
	}
	if global.imported && global.mutable {
		expr = &ast.StarExpr{X: expr}
	}
	return expr, global.mutable, global.typ, nil
}

func formatInt(i int64) string {
	dec := strconv.FormatInt(i, 10)
	hex := strconv.FormatInt(i, 16)
	if i >= 0 {
		hex = "0x" + hex
	} else {
		hex = "-0x" + hex[1:]
	}
	if complexity(hex) < complexity(dec) {
		return hex
	}
	return dec
}

func formatUint(i uint64) string {
	dec := strconv.FormatUint(i, 10)
	hex := "0x" + strconv.FormatUint(i, 16)
	if complexity(hex) < complexity(dec) {
		return hex
	}
	return dec
}

// This helps decide if a number is better represented
// in decimal or hexadecimal by counting the number of
// of transitions between different characters
// (i.e. ignoring runs of the same character).
// Because s includes the 0x prefix,
// hexadecimal needs a 3 character advantage to win.
func complexity(s string) (transitions int) {
	for i := 1; i < len(s); i++ {
		if s[i] != s[i-1] {
			transitions++
		}
	}
	return transitions
}

var (
	literal0  = &ast.BasicLit{Kind: token.INT, Value: "0"}
	literal1  = &ast.BasicLit{Kind: token.INT, Value: "1"}
	literal16 = &ast.BasicLit{Kind: token.INT, Value: "16"}
	literal63 = &ast.BasicLit{Kind: token.INT, Value: "63"}
)
