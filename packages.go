package main

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"go/parser"
	"maps"

	"golang.org/x/tools/go/ast/astutil"

	"github.com/stricttools/wasm-to-go/internal/mangle"
	"github.com/stricttools/wasm-to-go/internal/passes"
)

// maxPackageSize is the most translated code, in AST nodes (passes.Size),
// the translator writes into one Go package. A module with more code is
// written as several packages: see the README's compile cost section for
// why, and for how the value was chosen.
var maxPackageSize = 25_000

// The directories and names of the packages of a module written as
// several packages, relative to the output package's directory.
const (
	instancePkg  = "instance"
	functionsPkg = "functions"
	internalDir  = "internal"
)

// A file of a package, to print.
type pkgFile struct {
	rel   string // path relative to the output file's directory; "" for it
	name  string // the package name
	decls []ast.Decl
	doc   bool // has doc comments go/printer needs gofmt to place
}

// A function of the module's code, in the package plan.
type codeFunc struct {
	decl     *ast.FuncDecl
	name     string         // its name in the single package
	pub      string         // its exported name in the packages
	recv     bool           // takes the module (a method in the single package)
	params   *ast.FieldList // its parameters, without the module
	self     string         // the name of its receiver, the module
	size     int
	pkg      int // 1..n
	calls    map[string]int
	up       bool // called from a package it follows
	provided bool // from a -provided file
}

// The translation written as several packages.
type packaging struct {
	t          *translator
	importPath string
	funcs      []*codeFunc
	byName     map[string]*codeFunc
	fields     map[string]string // Module field → exported name
	dataNames  map[string]string // data constant → exported name
	moduleDecl *ast.GenDecl
	helpers    *helperSet
	n          int // code packages
}

// Reports whether the module's code is larger than one package may hold.
func (t *translator) needsPackages() bool {
	return t.codeSize() > maxPackageSize
}

// The size of the translated functions, in AST nodes.
func (t *translator) codeSize() int {
	total := 0
	for _, fd := range t.code {
		total += passes.Size(fd)
	}
	return total
}

// Writes the module as several packages: the output package, with the
// Module type users see, New, and the exports; internal/instance, with the
// state of a module instance; and internal/functions1 to functionsN,
// with the module's functions, each package at most maxPackageSize nodes
// (a function larger than that has a package of its own). It rewrites the
// translator's declarations in place, and returns the files to print.
func (t *translator) writePackages(moduleDecl *ast.GenDecl) (*packaging, []pkgFile, error) {
	if *importPath == "" {
		return nil, nil, fmt.Errorf("the module has %d AST nodes of translated code, more than one package holds (%d): "+
			"it is written as several packages, whose imports need -importpath, the import path of the directory of -o",
			t.codeSize(), maxPackageSize)
	}
	if *dwarfline {
		return nil, nil, errors.New("-dwarfline is not supported for a module written as several packages")
	}
	p := &packaging{
		t:          t,
		importPath: *importPath,
		byName:     map[string]*codeFunc{},
		fields:     map[string]string{},
		dataNames:  map[string]string{},
		moduleDecl: moduleDecl,
	}
	pubs := set[string]{}
	original := map[ast.Decl]bool{}
	for i := range t.functions {
		fn := &t.functions[i]
		if fn.decl == nil || fn.decl.Body == nil || fn.provided {
			continue
		}
		// Each package's declarations are rewritten in their own copy:
		// the translator's trees share nodes between functions.
		d := passes.Clone(fn.decl)
		original[fn.decl] = true
		// The copy replaces the original, whose body nothing reads
		// again: dropping it keeps one tree of each function, not two.
		fn.decl.Body = nil
		f := &codeFunc{decl: d, name: d.Name.Name, recv: d.Recv != nil, size: passes.Size(d), params: d.Type.Params, self: receiverName(d)}
		f.pub = exportName(f.name)
		if !pubs.add(f.pub) {
			return nil, nil, fmt.Errorf("two functions are named %s in the packages", f.pub)
		}
		p.funcs = append(p.funcs, f)
		p.byName[f.name] = f
	}
	// Provided functions, and the other declarations of their files, go
	// to a package of their own, the last: a declaration the files share
	// (a variable, say) must stay one.
	var providedOther []ast.Decl
	for _, pf := range t.providedFiles {
		for _, d := range pf.Decls {
			if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
				continue
			}
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || t.providedDecls[fd.Name.Name] != fd {
				providedOther = append(providedOther, d)
				continue
			}
			original[fd] = true
			fd = passes.Clone(fd)
			f := &codeFunc{decl: fd, name: fd.Name.Name, recv: true, size: passes.Size(fd), params: fd.Type.Params, provided: true, self: receiverName(fd)}
			f.pub = exportName(f.name)
			if !pubs.add(f.pub) {
				return nil, nil, fmt.Errorf("two functions are named %s in the packages", f.pub)
			}
			p.funcs = append(p.funcs, f)
			p.byName[f.name] = f
		}
	}
	for _, spec := range moduleDecl.Specs {
		for _, fl := range spec.(*ast.TypeSpec).Type.(*ast.StructType).Fields.List {
			for _, id := range fl.Names {
				p.fields[id.Name] = exportName(id.Name)
			}
		}
	}
	for i, seg := range t.data {
		if !seg.merged {
			p.dataNames[dataID(i).Name] = exportName(dataID(i).Name)
		}
	}
	if *embed {
		p.dataNames["data"] = "Data"
	}
	p.plan()

	// Sort the other declarations.
	helpers := t.helperDecls()
	var hostTypes, memTypes, instanceDecls, outDecls []ast.Decl
	var newDecl *ast.FuncDecl
	var dataDecl *ast.GenDecl
	fromProvided := map[ast.Decl]bool{}
	for _, d := range providedOther {
		fromProvided[d] = true
	}
	for _, d := range t.out.Decls {
		if fromProvided[d] {
			continue
		}
		switch d := d.(type) {
		case *ast.FuncDecl:
			switch {
			case original[d] || helpers.decls[d]:
			case d.Name.Name == "New" && d.Recv == nil:
				newDecl = passes.Clone(d)
			case d.Recv != nil && recvType(d) == "wasmMemory":
				memTypes = append(memTypes, passes.Clone(d))
			default:
				outDecls = append(outDecls, passes.Clone(d))
			}
		case *ast.GenDecl:
			switch {
			case d == moduleDecl || d.Tok == token.IMPORT || helpers.decls[d]:
			case d.Tok == token.CONST || (d.Tok == token.VAR && *embed && declares(d, "data")):
				dataDecl = d
			case d.Tok == token.TYPE && (declares(d, "Memory") || declares(d, "wasmMemory")):
				memTypes = append(memTypes, passes.Clone(d))
			case d.Tok == token.TYPE:
				hostTypes = append(hostTypes, passes.Clone(d))
			default:
				outDecls = append(outDecls, passes.Clone(d))
			}
		}
	}

	// The functions: a method takes the module as its first parameter,
	// named as its receiver was.
	for _, f := range p.funcs {
		d := f.decl
		if f.recv {
			d.Type.Params = &ast.FieldList{List: append([]*ast.Field{{
				Names: []*ast.Ident{ast.NewIdent(f.self)},
				Type:  &ast.StarExpr{X: &ast.SelectorExpr{X: ast.NewIdent(instancePkg), Sel: ast.NewIdent("Module")}},
			}}, d.Type.Params.List...)}
			d.Recv = nil
		}
		d.Name = ast.NewIdent(f.pub)
		self := func() ast.Expr { return ast.NewIdent(f.self) }
		p.rewrite(d.Body, f.pkg, f.self, self, self)
	}
	var files []pkgFile
	for i := 1; i <= p.n; i++ {
		var decls []ast.Decl
		for _, f := range p.funcs {
			if f.pkg == i {
				decls = append(decls, f.decl)
				if f.provided {
					decls = append(decls, providedOther...)
					providedOther = nil
				}
			}
		}
		files = append(files, p.file(p.pkgName(i), path.Join(internalDir, p.pkgName(i), p.pkgName(i)+".go"), decls, helpers, false))
	}

	// The instance package.
	inst := p.instanceModule()
	instanceDecls = append(instanceDecls, inst)
	for _, d := range hostTypes {
		instanceDecls = append(instanceDecls, d)
	}
	for _, d := range memTypes {
		p.renameWasmMemory(d)
		instanceDecls = append(instanceDecls, d)
	}
	var upVars []ast.Spec
	for _, f := range p.funcs {
		if !f.up {
			continue
		}
		ft := &ast.FuncType{Params: &ast.FieldList{}, Results: f.decl.Type.Results}
		for i, fl := range f.decl.Type.Params.List {
			typ := fl.Type
			if i == 0 && f.recv {
				typ = &ast.StarExpr{X: ast.NewIdent("Module")}
			}
			ft.Params.List = append(ft.Params.List, &ast.Field{Names: fl.Names, Type: typ})
		}
		upVars = append(upVars, &ast.ValueSpec{Names: []*ast.Ident{ast.NewIdent(f.pub)}, Type: ft})
	}
	if len(upVars) > 0 {
		instanceDecls = append(instanceDecls, &ast.GenDecl{
			Tok:   token.VAR,
			Doc:   comment("The functions called from a package that precedes theirs, set by the output package when it is initialized."),
			Specs: upVars})
	}
	if dataDecl != nil {
		if *embed {
			instanceDecls = append(instanceDecls, &ast.GenDecl{Tok: token.VAR, Specs: []ast.Spec{
				&ast.ValueSpec{Names: []*ast.Ident{ast.NewIdent("Data")}, Type: ast.NewIdent("string")}}})
		} else {
			for _, spec := range dataDecl.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, id := range vs.Names {
					vs.Names[i] = ast.NewIdent(p.dataNames[id.Name])
				}
			}
			instanceDecls = append(instanceDecls, dataDecl)
		}
	}
	files = append(files, p.file(instancePkg, path.Join(internalDir, instancePkg, instancePkg+".go"), instanceDecls, helpers, true))

	// The output package.
	var out []ast.Decl
	out = append(out, &ast.GenDecl{Tok: token.TYPE, Doc: moduleDecl.Doc, Specs: []ast.Spec{&ast.TypeSpec{
		Name: ast.NewIdent("Module"),
		Type: &ast.StructType{Fields: &ast.FieldList{List: []*ast.Field{{
			Names: []*ast.Ident{ast.NewIdent(instancePkg)},
			Type:  &ast.SelectorExpr{X: ast.NewIdent(instancePkg), Sel: ast.NewIdent("Module")}}}}}}}})
	for _, d := range append(slices.Clone(hostTypes), memTypes...) {
		gd, ok := d.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gd.Specs {
			ts := spec.(*ast.TypeSpec)
			if ts.Assign == 0 {
				continue // wasmMemory: users see it only as a Memory
			}
			out = append(out, &ast.GenDecl{Tok: token.TYPE, Specs: []ast.Spec{&ast.TypeSpec{
				Name: ast.NewIdent(ts.Name.Name), Assign: 1,
				Type: &ast.SelectorExpr{X: ast.NewIdent(instancePkg), Sel: ast.NewIdent(ts.Name.Name)}}}})
		}
	}
	if newDecl != nil {
		p.rewriteNew(newDecl)
		out = append(out, newDecl)
	}
	out = append(out, p.exportMethods(outDecls)...)
	if *embed && dataDecl != nil {
		out = append(out, dataDecl)
	}
	var wiring []ast.Stmt
	if *embed && dataDecl != nil {
		wiring = append(wiring, &ast.AssignStmt{Tok: token.ASSIGN,
			Lhs: []ast.Expr{&ast.SelectorExpr{X: ast.NewIdent(instancePkg), Sel: ast.NewIdent("Data")}},
			Rhs: []ast.Expr{ast.NewIdent("data")}})
	}
	for _, f := range p.funcs {
		if f.up {
			wiring = append(wiring, &ast.AssignStmt{Tok: token.ASSIGN,
				Lhs: []ast.Expr{&ast.SelectorExpr{X: ast.NewIdent(instancePkg), Sel: ast.NewIdent(f.pub)}},
				Rhs: []ast.Expr{&ast.SelectorExpr{X: ast.NewIdent(p.pkgName(f.pkg)), Sel: ast.NewIdent(f.pub)}}})
		}
	}
	if len(wiring) > 0 {
		out = append(out, &ast.FuncDecl{Name: ast.NewIdent("init"), Type: &ast.FuncType{Params: &ast.FieldList{}},
			Body: &ast.BlockStmt{List: wiring}})
	}
	aliasPackages(out)
	files = append(files, p.file(t.out.Name.Name, "", out, helpers, moduleDecl.Doc != nil))
	return p, files, nil
}

// The Module type of the instance package: the translator's, with its
// fields exported.
func (p *packaging) instanceModule() *ast.GenDecl {
	ts := p.moduleDecl.Specs[0].(*ast.TypeSpec)
	st := ts.Type.(*ast.StructType)
	fields := &ast.FieldList{}
	for _, fl := range st.Fields.List {
		nf := &ast.Field{Type: fl.Type}
		for _, id := range fl.Names {
			nf.Names = append(nf.Names, ast.NewIdent(p.fields[id.Name]))
		}
		fields.List = append(fields.List, nf)
	}
	return &ast.GenDecl{Tok: token.TYPE,
		Doc:   comment("Module is the state of an instance of the module: the output package's Module holds one."),
		Specs: []ast.Spec{&ast.TypeSpec{Name: ast.NewIdent("Module"), Type: &ast.StructType{Fields: fields}}}}
}

// Renames wasmMemory, exported in the instance package, and the fields its
// methods reach through their receiver (an owned memory's wasmMemory is the
// Module).
func (p *packaging) renameWasmMemory(d ast.Decl) {
	recv := ""
	if fd, ok := d.(*ast.FuncDecl); ok {
		recv = receiverName(fd)
	}
	astutil.Apply(d, nil, func(c *astutil.Cursor) bool {
		switch n := c.Node().(type) {
		case *ast.Ident:
			if n.Name == "wasmMemory" {
				c.Replace(ast.NewIdent("WasmMemory"))
			}
		case *ast.SelectorExpr:
			if id, ok := n.X.(*ast.Ident); ok && recv != "" && id.Name == recv {
				if pub, ok := p.fields[n.Sel.Name]; ok {
					c.Replace(&ast.SelectorExpr{X: n.X, Sel: ast.NewIdent(pub)})
				}
			}
		}
		return true
	})
}

// Rewrites New for the output package: it fills the Module's instance,
// through s.
func (p *packaging) rewriteNew(d *ast.FuncDecl) {
	used := false
	s := func() ast.Expr {
		used = true
		return ast.NewIdent("s")
	}
	p.rewrite(d.Body, 0, "m", s, s)
	if !used {
		return
	}
	// m := new(Module) is first; s := &m.instance follows it.
	d.Body.List = slices.Insert(d.Body.List, 1, ast.Stmt(&ast.AssignStmt{
		Lhs: []ast.Expr{ast.NewIdent("s")}, Tok: token.DEFINE,
		Rhs: []ast.Expr{&ast.UnaryExpr{Op: token.AND, X: &ast.SelectorExpr{X: ast.NewIdent("m"), Sel: ast.NewIdent(instancePkg)}}}}))
}

// The exported methods of the output package's Module: the translator's
// export methods, rewritten, and a method for each exported function.
func (p *packaging) exportMethods(decls []ast.Decl) []ast.Decl {
	inst := func() ast.Expr {
		return &ast.UnaryExpr{Op: token.AND, X: &ast.SelectorExpr{X: ast.NewIdent("m"), Sel: ast.NewIdent(instancePkg)}}
	}
	base := func() ast.Expr { return &ast.SelectorExpr{X: ast.NewIdent("m"), Sel: ast.NewIdent(instancePkg)} }
	var out []ast.Decl
	for _, d := range decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv != nil {
			p.rewrite(fd.Body, 0, "m", inst, base)
			astutil.Apply(fd.Body, nil, func(c *astutil.Cursor) bool {
				switch n := c.Node().(type) {
				case *ast.Ident:
					if n.Name == "wasmMemory" {
						c.Replace(&ast.SelectorExpr{X: ast.NewIdent(instancePkg), Sel: ast.NewIdent("WasmMemory")})
					}
				case *ast.CallExpr:
					// (*wasmMemory)(m), an owned memory's: the instance is its Module.
					fun := n.Fun
					if par, ok := fun.(*ast.ParenExpr); ok {
						fun = par.X
					}
					if star, ok := fun.(*ast.StarExpr); ok && isWasmMemory(star.X) && len(n.Args) == 1 && isIdent(n.Args[0], "m") {
						n.Args[0] = inst()
					}
				}
				return true
			})
		}
		out = append(out, d)
	}
	names := make([]string, 0, len(p.t.exports))
	for name := range p.t.exports {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		exp := p.t.exports[name]
		if exp.kind != externFunction {
			continue
		}
		fn := &p.t.functions[exp.index]
		f := p.byName[fn.decl.Name.Name]
		if f == nil || f.name != mangleExported(name) {
			continue // the translator wrote a method for it
		}
		typ := fn.typ.toAST(true)
		call := &ast.CallExpr{Fun: p.funcExpr(f, 0)}
		if f.recv {
			call.Args = append(call.Args, inst())
		}
		for i := range fn.typ.params {
			call.Args = append(call.Args, localVar(i))
		}
		var body ast.Stmt = &ast.ExprStmt{X: call}
		if len(fn.typ.results) > 0 {
			body = &ast.ReturnStmt{Results: []ast.Expr{call}}
		}
		out = append(out, &ast.FuncDecl{Recv: modRecvList, Name: ast.NewIdent(f.name), Type: typ,
			Body: &ast.BlockStmt{List: []ast.Stmt{body}}})
	}
	return out
}

// The file of package name at rel (the output file if ""), with decls and
// the helpers they use.
func (p *packaging) file(name, rel string, decls []ast.Decl, helpers *helperSet, doc bool) pkgFile {
	p.helpers = helpers
	return pkgFile{rel: rel, name: name, decls: decls, doc: doc}
}

// decls with the helpers they use.
func (p *packaging) withHelpers(decls []ast.Decl) []ast.Decl {
	return append(slices.Clone(decls), p.helpers.closure(decls)...)
}

// The prefix of the names under which the output package imports the
// packages of the translation: the output package holds the user's code
// too (-provided files, and a consumer's own files), whose package-level
// names an import name must not collide with.
const aliasPrefix = "wasm2go_"

// Qualifies the references of decls, the output package's, to the packages
// of the translation by their import names there.
func aliasPackages(decls []ast.Decl) {
	for _, d := range decls {
		astutil.Apply(d, nil, func(c *astutil.Cursor) bool {
			sel, ok := c.Node().(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && (id.Name == instancePkg || strings.HasPrefix(id.Name, functionsPkg)) {
				c.Replace(&ast.SelectorExpr{X: ast.NewIdent(aliasPrefix + id.Name), Sel: sel.Sel})
			}
			return true
		})
	}
}

// The packages of the translation decls use, each as its path, or as
// "name path" when decls use an alias (aliasPackages).
func (p *packaging) imports(decls []ast.Decl) []string {
	paths := set[string]{}
	for _, d := range decls {
		ast.Inspect(d, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			id, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			name, alias := strings.CutPrefix(id.Name, aliasPrefix)
			spec := ""
			if name == instancePkg {
				spec = path.Join(p.importPath, internalDir, instancePkg)
			} else if i, err := strconv.Atoi(strings.TrimPrefix(name, functionsPkg)); err == nil && strings.HasPrefix(name, functionsPkg) && i >= 1 && i <= p.n {
				spec = p.pkgPath(i)
			}
			if spec != "" {
				if alias {
					spec = id.Name + " " + spec
				}
				paths.add(spec)
			}
			return true
		})
	}
	return slices.Sorted(maps.Keys(paths))
}

// The name of d's receiver: "m" for the translator's methods, whatever a
// provided file names it, and "m" for none or an unnamed one.
// Reports whether e names wasmMemory, as the translator wrote it or as
// exportMethods renamed it.
func isWasmMemory(e ast.Expr) bool {
	if sel, ok := e.(*ast.SelectorExpr); ok {
		return isIdent(sel.X, instancePkg) && sel.Sel.Name == "WasmMemory"
	}
	return isIdent(e, "wasmMemory")
}

func receiverName(d *ast.FuncDecl) string {
	if d.Recv != nil && len(d.Recv.List) == 1 && len(d.Recv.List[0].Names) == 1 && d.Recv.List[0].Names[0].Name != "_" {
		return d.Recv.List[0].Names[0].Name
	}
	return "m"
}

func recvType(d *ast.FuncDecl) string {
	typ := d.Recv.List[0].Type
	if st, ok := typ.(*ast.StarExpr); ok {
		typ = st.X
	}
	if id, ok := typ.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

func declares(d *ast.GenDecl, name string) bool {
	for _, spec := range d.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			if s.Name.Name == name {
				return true
			}
		case *ast.ValueSpec:
			for _, id := range s.Names {
				if id.Name == name {
					return true
				}
			}
		}
	}
	return false
}

func comment(text string) *ast.CommentGroup {
	return &ast.CommentGroup{List: []*ast.Comment{{Text: "// " + text}}}
}

// Plans the code packages: orders the functions callers first (the
// strongly connected components of the call graph in topological order),
// then fills packages in that order. A call to a function in a later
// package is a direct call; to an earlier one, a call through a function
// variable of the instance package.
func (p *packaging) plan() {
	// The call graph.
	for _, f := range p.funcs {
		f.calls = map[string]int{}
		ast.Inspect(f.decl.Body, func(n ast.Node) bool {
			if name, ok := p.funcRef(n, f.self); ok {
				f.calls[name]++
			}
			return true
		})
	}
	order := p.callersFirst()
	pkg, size := 1, 0
	var provided bool
	for _, f := range order {
		if f.provided {
			provided = true
			continue
		}
		if size > 0 && size+f.size > maxPackageSize {
			pkg++
			size = 0
		}
		f.pkg = pkg
		size += f.size
	}
	if provided {
		pkg++
		for _, f := range p.funcs {
			if f.provided {
				f.pkg = pkg
			}
		}
	}
	p.n = pkg
	for _, f := range p.funcs {
		for name := range f.calls {
			if g := p.byName[name]; g.pkg < f.pkg {
				g.up = true
			}
		}
	}
}

// The name of the module function n refers to, if it is m.name or name.
func (p *packaging) funcRef(n ast.Node, self string) (string, bool) {
	switch n := n.(type) {
	case *ast.SelectorExpr:
		if id, ok := n.X.(*ast.Ident); ok && id.Name == self {
			if f := p.byName[n.Sel.Name]; f != nil && f.recv {
				return f.name, true
			}
		}
	case *ast.Ident:
		if f := p.byName[n.Name]; f != nil && !f.recv {
			return f.name, true
		}
	}
	return "", false
}

// Orders the functions so that calls go forward wherever the call graph
// allows: Tarjan's strongly connected components, in topological order
// (callers first), each in the order a depth-first search from its first
// function visits it.
func (p *packaging) callersFirst() []*codeFunc {
	index := map[*codeFunc]int{}
	low := map[*codeFunc]int{}
	onStack := map[*codeFunc]bool{}
	var stack []*codeFunc
	var sccs [][]*codeFunc
	next := 0
	var visit func(f *codeFunc)
	callees := func(f *codeFunc) []*codeFunc {
		names := make([]string, 0, len(f.calls))
		for name := range f.calls {
			names = append(names, name)
		}
		slices.Sort(names)
		var gs []*codeFunc
		for _, name := range names {
			gs = append(gs, p.byName[name])
		}
		return gs
	}
	visit = func(f *codeFunc) {
		index[f], low[f] = next, next
		next++
		stack = append(stack, f)
		onStack[f] = true
		for _, g := range callees(f) {
			if _, ok := index[g]; !ok {
				visit(g)
				low[f] = min(low[f], low[g])
			} else if onStack[g] {
				low[f] = min(low[f], index[g])
			}
		}
		if low[f] == index[f] {
			var scc []*codeFunc
			for {
				g := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[g] = false
				scc = append(scc, g)
				if g == f {
					break
				}
			}
			sccs = append(sccs, scc)
		}
	}
	for _, f := range p.funcs {
		if _, ok := index[f]; !ok {
			visit(f)
		}
	}
	// Tarjan finds components callees first.
	slices.Reverse(sccs)
	var order []*codeFunc
	for _, scc := range sccs {
		// Within a component: depth-first from its function with the
		// most calls from outside it, visiting callees after callers.
		in := map[*codeFunc]bool{}
		for _, f := range scc {
			in[f] = true
		}
		slices.SortFunc(scc, func(a, b *codeFunc) int { return strings.Compare(a.name, b.name) })
		seen := map[*codeFunc]bool{}
		var dfs func(f *codeFunc)
		dfs = func(f *codeFunc) {
			seen[f] = true
			order = append(order, f)
			for _, g := range callees(f) {
				if in[g] && !seen[g] {
					dfs(g)
				}
			}
		}
		for _, f := range scc {
			if !seen[f] {
				dfs(f)
			}
		}
	}
	return order
}

// The exported name of name, a function, field, or constant of the
// single package: capitalized, or with a U before a leading underscore.
func exportName(name string) string {
	switch {
	case name == "":
		return name
	case name[0] == '_':
		return "U" + name
	case name[0] >= 'a' && name[0] <= 'z':
		return strings.ToUpper(name[:1]) + name[1:]
	}
	return name
}

func (p *packaging) pkgName(i int) string { return functionsPkg + strconv.Itoa(i) }

func (p *packaging) pkgPath(i int) string {
	return path.Join(p.importPath, internalDir, p.pkgName(i))
}

// Rewrites the references to module functions and fields in n, code of
// package pkg (0 for the output package), where inst is the expression of
// the instance (a *instance.Module) and base the expression whose fields
// are the instance's.
func (p *packaging) rewrite(n ast.Node, pkg int, self string, inst, base func() ast.Expr) ast.Node {
	return astutil.Apply(n, func(c *astutil.Cursor) bool {
		e, ok := c.Node().(*ast.CallExpr)
		if !ok {
			return true
		}
		fun := e.Fun
		for {
			par, ok := fun.(*ast.ParenExpr)
			if !ok {
				break
			}
			fun = par.X
		}
		name, ok := p.calleeName(fun, self)
		if !ok {
			return true
		}
		f := p.byName[name]
		call := &ast.CallExpr{Fun: p.funcExpr(f, pkg), Args: e.Args, Ellipsis: e.Ellipsis}
		if f.recv {
			call.Args = append([]ast.Expr{inst()}, e.Args...)
		}
		c.Replace(call)
		return true
	}, func(c *astutil.Cursor) bool {
		switch e := c.Node().(type) {
		case *ast.SelectorExpr:
			id, ok := e.X.(*ast.Ident)
			if !ok || id.Name != self {
				return true
			}
			if f := p.byName[e.Sel.Name]; f != nil && f.recv {
				// A method value (ref.func, a table element).
				c.Replace(p.closure(f, pkg, inst))
				return true
			}
			if pub, ok := p.fields[e.Sel.Name]; ok {
				c.Replace(&ast.SelectorExpr{X: base(), Sel: ast.NewIdent(pub)})
			}
		case *ast.Ident:
			if f := p.byName[e.Name]; f != nil && !f.recv {
				c.Replace(p.funcExpr(f, pkg))
				return true
			}
			// The output package declares the embedded data itself.
			if pub, ok := p.dataNames[e.Name]; ok && (pkg > 0 || e.Name != "data") {
				c.Replace(&ast.SelectorExpr{X: ast.NewIdent(instancePkg), Sel: ast.NewIdent(pub)})
			}
		}
		return true
	})
}

// The module function fun names, as the callee of a call.
func (p *packaging) calleeName(fun ast.Expr, self string) (string, bool) {
	switch f := fun.(type) {
	case *ast.SelectorExpr:
		if id, ok := f.X.(*ast.Ident); ok && id.Name == self {
			if g := p.byName[f.Sel.Name]; g != nil && g.recv {
				return g.name, true
			}
		}
	case *ast.Ident:
		if g := p.byName[f.Name]; g != nil && !g.recv {
			return g.name, true
		}
	}
	return "", false
}

// The expression that names function f from package pkg.
func (p *packaging) funcExpr(f *codeFunc, pkg int) ast.Expr {
	switch {
	case pkg == f.pkg:
		return ast.NewIdent(f.pub)
	case pkg > f.pkg:
		return &ast.SelectorExpr{X: ast.NewIdent(instancePkg), Sel: ast.NewIdent(f.pub)}
	}
	return &ast.SelectorExpr{X: ast.NewIdent(p.pkgName(f.pkg)), Sel: ast.NewIdent(f.pub)}
}

// A function literal calling f on the instance: what the method value
// m.f was.
func (p *packaging) closure(f *codeFunc, pkg int, inst func() ast.Expr) ast.Expr {
	ft := f.decl.Type
	params := &ast.FieldList{}
	var args []ast.Expr
	if f.recv {
		args = append(args, inst())
	}
	i := 0
	for _, fl := range f.params.List {
		n := max(len(fl.Names), 1)
		field := &ast.Field{Type: fl.Type}
		for range n {
			id := ast.NewIdent("x" + strconv.Itoa(i))
			field.Names = append(field.Names, id)
			args = append(args, id)
			i++
		}
		params.List = append(params.List, field)
	}
	call := &ast.CallExpr{Fun: p.funcExpr(f, pkg), Args: args}
	var body ast.Stmt = &ast.ExprStmt{X: call}
	if ft.Results != nil && len(ft.Results.List) > 0 {
		body = &ast.ReturnStmt{Results: []ast.Expr{call}}
	}
	return &ast.FuncLit{
		Type: &ast.FuncType{Params: params, Results: ft.Results},
		Body: &ast.BlockStmt{List: []ast.Stmt{body}}}
}

// The helper declarations of the translation, by the names they declare.
type helperSet struct {
	decls  map[ast.Decl]bool
	byName map[string]ast.Decl
	order  []ast.Decl
}

// The declarations t.out has from the helper sources.
func (t *translator) helperDecls() *helperSet {
	h := &helperSet{decls: map[ast.Decl]bool{}, byName: map[string]ast.Decl{}}
	names := set[string]{}
	fset := token.NewFileSet()
	for _, src := range []string{helpersSrc, helpersUnsafeSrc, helpersAtomicsSrc, helpersCpuArchSrc} {
		f, err := parserParse(fset, src)
		if err != nil {
			panic(err)
		}
		for _, d := range f.Decls {
			for _, name := range declNames(d) {
				names.add(name)
			}
		}
	}
	code := set[string]{}
	for i := range t.functions {
		if d := t.functions[i].decl; d != nil {
			code.add(d.Name.Name)
		}
	}
	for _, d := range t.out.Decls {
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.IMPORT {
			continue
		}
		if fd, ok := d.(*ast.FuncDecl); ok && (fd.Recv != nil || code.has(fd.Name.Name)) {
			continue
		}
		dn := declNames(d)
		if len(dn) == 0 {
			continue
		}
		all := true
		for _, name := range dn {
			if name != "_" && !names.has(name) {
				all = false
			}
		}
		if !all {
			continue
		}
		h.decls[d] = true
		h.order = append(h.order, d)
		for _, name := range dn {
			if name != "_" {
				h.byName[name] = d
			}
		}
	}
	return h
}

// The helper declarations decls use, directly or through other helpers,
// in the order of t.out.
func (h *helperSet) closure(decls []ast.Decl) []ast.Decl {
	used := map[ast.Decl]bool{}
	var work []ast.Decl
	visit := func(n ast.Node) {
		ast.Inspect(n, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				if d := h.byName[id.Name]; d != nil && !used[d] {
					used[d] = true
					work = append(work, d)
				}
			}
			return true
		})
	}
	for _, d := range decls {
		if !h.decls[d] {
			visit(d)
		}
	}
	for len(work) > 0 {
		d := work[len(work)-1]
		work = work[:len(work)-1]
		visit(d)
	}
	var out []ast.Decl
	for _, d := range h.order {
		if used[d] {
			out = append(out, d)
		}
	}
	return out
}

// The names d declares at package level.
func declNames(d ast.Decl) []string {
	var names []string
	switch d := d.(type) {
	case *ast.FuncDecl:
		if d.Recv == nil {
			names = append(names, d.Name.Name)
		}
	case *ast.GenDecl:
		for _, spec := range d.Specs {
			switch s := spec.(type) {
			case *ast.TypeSpec:
				names = append(names, s.Name.Name)
			case *ast.ValueSpec:
				for _, id := range s.Names {
					names = append(names, id.Name)
				}
			}
		}
	}
	return names
}

func mangleExported(name string) string { return mangle.Name(name, mangle.Exported) }

func parserParse(fset *token.FileSet, src string) (*ast.File, error) {
	return parser.ParseFile(fset, "", src, 0)
}

// Prints the packages' files: each to sub(rel), the output package's to w;
// with -unsafe (generic set), each twice, like the single file.
func (p *packaging) print(files []pkgFile, w, generic io.Writer, sub func(rel string) (io.Writer, error), fset *token.FileSet) error {
	if sub == nil {
		return errors.New("a module written as several packages needs -o")
	}
	open := func(rel string, generic io.Writer) (io.Writer, error) {
		if rel == "" {
			return generic, nil
		}
		return sub(rel)
	}
	var code []*ast.FuncDecl
	isCode := map[ast.Decl]bool{}
	for _, f := range p.funcs {
		code = append(code, f.decl)
		isCode[f.decl] = true
	}
	p.t.code = code
	// Each file's functions are lowered (or expanded) when the file is
	// printed, and dropped once every file holding them is printed, so the
	// translator holds the larger lowered code of one package at a time.
	codeOf := func(decls []ast.Decl) []*ast.FuncDecl {
		var fds []*ast.FuncDecl
		for _, d := range decls {
			if isCode[d] {
				fds = append(fds, d.(*ast.FuncDecl))
			}
		}
		return fds
	}
	release := func(fds []*ast.FuncDecl) {
		for _, fd := range fds {
			fd.Body = nil
		}
	}
	if generic == nil {
		for _, pf := range files {
			out, err := open(pf.rel, w)
			if err != nil {
				return err
			}
			fds := codeOf(pf.decls)
			p.t.lower(fds)
			if err := p.t.printDecls(out, fset, pf.name, p.withHelpers(pf.decls), *tags, pf.doc, p.imports); err != nil {
				return err
			}
			release(fds)
		}
		return nil
	}
	expanded, others, err := expandConstraints()
	if err != nil {
		return err
	}
	for _, pf := range files {
		rel := pf.rel
		if rel != "" {
			rel = genericFile(rel)
		}
		out, err := open(rel, generic)
		if err != nil {
			return err
		}
		if err := p.t.printDecls(out, fset, pf.name, p.withHelpers(p.t.lowered(pf.decls)), others, pf.doc, p.imports); err != nil {
			return err
		}
		fds := codeOf(pf.decls)
		p.t.expand(fds)
		if out, err = open(pf.rel, w); err != nil {
			return err
		}
		if err := p.t.printDecls(out, fset, pf.name, p.withHelpers(pf.decls), expanded, pf.doc, p.imports); err != nil {
			return err
		}
		release(fds)
	}
	return nil
}

// The files of a translation, under dir, the directory of the output
// file: those of the packages besides the output package.
type packageFiles struct {
	dir     string
	written set[string] // paths relative to dir, with slashes
	files   []*os.File
}

func newPackageFiles(dir string) *packageFiles {
	return &packageFiles{dir: dir, written: set[string]{}}
}

// Creates the file at rel, a slash path relative to dir.
func (pf *packageFiles) create(rel string) (io.Writer, error) {
	path := filepath.Join(pf.dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	pf.written.add(rel)
	pf.files = append(pf.files, f)
	return f, nil
}

// Closes the files, and removes the files of the packages an earlier
// translation into dir wrote that this one did not (a module that now
// needs fewer packages, or one): the files wasm2go generated in
// internal/instance and internal/functionsN, and those directories once
// empty. A file there that wasm2go did not generate is an error.
func (pf *packageFiles) close() error {
	var err error
	for _, f := range pf.files {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}
	if err != nil {
		return err
	}
	internal := filepath.Join(pf.dir, internalDir)
	entries, rerr := os.ReadDir(internal)
	if errors.Is(rerr, fs.ErrNotExist) {
		return nil
	} else if rerr != nil {
		return rerr
	}
	for _, e := range entries {
		name := e.Name()
		n, numErr := strconv.Atoi(strings.TrimPrefix(name, functionsPkg))
		if !e.IsDir() || !(name == instancePkg || strings.HasPrefix(name, functionsPkg) && numErr == nil && n > 0) {
			continue
		}
		dir := filepath.Join(internal, name)
		files, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, f := range files {
			rel := path.Join(internalDir, name, f.Name())
			if pf.written.has(rel) {
				continue
			}
			file := filepath.Join(dir, f.Name())
			head, err := readHead(file, len(generatedLine))
			if err != nil {
				return err
			}
			if f.IsDir() || head != generatedLine {
				return fmt.Errorf("%s: not written by wasm2go, in a directory of its packages: move it", file)
			}
			if err := os.Remove(file); err != nil {
				return err
			}
		}
		if left, err := os.ReadDir(dir); err == nil && len(left) == 0 {
			if err := os.Remove(dir); err != nil {
				return err
			}
		}
	}
	if left, err := os.ReadDir(internal); err == nil && len(left) == 0 {
		return os.Remove(internal)
	}
	return nil
}

// The first line of every file wasm2go writes.
const generatedLine = "// Code generated by wasm2go. DO NOT EDIT."

func readHead(file string, n int) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, n)
	m, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "", err
	}
	return string(buf[:m]), nil
}
