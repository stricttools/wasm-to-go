package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"github.com/stricttools/wasm-to-go/internal/passes"
)

type wasmType byte

const (
	i32 wasmType = 127 - iota
	i64
	f32
	f64
	v128
	funcref   wasmType = 0x70
	externref wasmType = 0x6f
)

func (t wasmType) ref() bool {
	return t == funcref || t == externref
}

func (t wasmType) check() error {
	switch t {
	case i32, i64, f32, f64, v128, funcref, externref:
		return nil
	default:
		return fmt.Errorf("unsupported type: 0x%02X", byte(t))
	}
}

func (t wasmType) ident() *ast.Ident {
	switch t {
	case i32:
		return newID("int32")
	case i64:
		return newID("int64")
	case f32:
		return newID("float32")
	case f64:
		return newID("float64")
	case v128:
		return newID(passes.SIMDType)
	case funcref, externref:
		return newID("any")
	}
	panic(fmt.Sprintf("unsupported type: 0x%02X", byte(t)))
}

// The Go type of t where the Module meets its host: a v128 is its 16
// bytes in memory order, [16]byte, in the signatures of exports and
// imports and in globals, since the type of the translated code's
// vectors depends on the build (passes.LowerSIMD).
func (t wasmType) apiType() ast.Expr {
	if t == v128 {
		return &ast.ArrayType{Len: &ast.BasicLit{Kind: token.INT, Value: "16"}, Elt: newID("byte")}
	}
	return t.ident()
}

// Reports whether t has a v128 parameter or result.
func (t funcType) hasV128() bool {
	return strings.IndexByte(t.params, byte(v128)) >= 0 || strings.IndexByte(t.results, byte(v128)) >= 0
}

// The function type of t where the Module meets its host (apiType).
func (t funcType) toAPI(names bool) *ast.FuncType {
	ft := t.toAST(names)
	for _, fl := range []*ast.FieldList{ft.Params, ft.Results} {
		if fl == nil {
			continue
		}
		for _, f := range fl.List {
			if id, ok := f.Type.(*ast.Ident); ok && id.Name == passes.SIMDType {
				f.Type = v128.apiType()
			}
		}
	}
	return ft
}

type funcType struct {
	params  string // wasmType of parameters
	results string // wasmType of results
}

func (t funcType) check() error {
	for _, t := range []byte(t.params) {
		if err := wasmType(t).check(); err != nil {
			return err
		}
	}
	for _, t := range []byte(t.results) {
		if err := wasmType(t).check(); err != nil {
			return err
		}
	}
	return nil
}

func (t funcType) toAST(names bool) *ast.FuncType {
	return &ast.FuncType{
		Params:  paramsToAST(t.params, names),
		Results: resultsToAST(t.results),
	}
}

func paramsToAST(types string, names bool) *ast.FieldList {
	if names && len(types) > 0 {
		var list stack[*ast.Field]
		for i, t := range []byte(types) {
			if i > 0 && t == types[i-1] {
				(*list.top()).Names = append((*list.top()).Names, localVar(i))
			} else {
				list.append(&ast.Field{
					Names: []*ast.Ident{localVar(i)},
					Type:  wasmType(t).ident()})
			}
		}
		return &ast.FieldList{List: list}
	}

	list := make([]*ast.Field, len(types))
	for i, t := range []byte(types) {
		list[i] = &ast.Field{Type: wasmType(t).ident()}
	}
	return &ast.FieldList{List: list}
}

func resultsToAST(types string) *ast.FieldList {
	if len(types) == 0 {
		return nil
	}
	list := make([]*ast.Field, len(types))
	for i, t := range []byte(types) {
		list[i] = &ast.Field{Type: wasmType(t).ident()}
	}
	return &ast.FieldList{List: list}
}

type externKind byte

const (
	externFunction externKind = iota
	externTable
	externMemory
	externGlobal
)

type importDef struct {
	module string
	name   string
	kind   externKind
	fnType funcType
	typ    wasmType
	index  int
}

type tableDef struct {
	id       *ast.Ident
	imported bool
	is64     bool
	min      uint64
	max      uint64
	// mutated: some instruction (table.set, table.grow, table.fill,
	// table.init, or table.copy into it) can change the table after New.
	mutated bool
}

// An indirect call site, through a table, with the called signature.
type indirectCall struct {
	table int
	typ   funcType
}

func (m *tableDef) stype() string {
	if m.is64 {
		return "int64"
	}
	return "int32"
}

type memoryDef struct {
	id       *ast.Ident
	selector ast.Expr
	imported bool
	shared   bool
	is64     bool
	min      uint64
	max      uint64
}

// owned reports whether the module owns its memory's slice outright: a
// memory neither imported nor shared, whose growth memory_grow implements
// over a backing array (the memBacking field).
func (m *memoryDef) owned() bool { return !m.imported && !m.shared }

// memBackingField is the Module field holding an owned memory's backing
// array, of which the memory's slice is a prefix.
const memBackingField = "memBacking"

func (m *memoryDef) stype() string {
	if m.is64 {
		return "int64"
	}
	return "int32"
}

func (m *memoryDef) utype() string {
	return "u" + m.stype()
}

type globalDef struct {
	id       *ast.Ident
	typ      wasmType
	mutable  bool
	imported bool
	init     ast.Expr
}

type export struct {
	kind  externKind
	index int
}

type elemSegment struct {
	init    []ast.Expr
	index   uint32
	offset  ast.Expr
	passive bool
	declive bool
}

type dataSegment struct {
	init    []byte
	offset  ast.Expr
	embed   *ast.ParenExpr
	passive bool
	merged  bool
}

type nameSubsection byte

const (
	nameModule nameSubsection = iota
	nameFunction
	nameLocal
	nameLabel
	nameType
	nameTable
	nameMemory
	nameGlobal
	nameElem
	nameData
)

type dylinkKind int

const dylinkMemInfo dylinkKind = 1

type dylinkDef struct {
	memorySize      int64
	memoryAlignment int64
	tableSize       int64
	tableAlignment  int64
}
