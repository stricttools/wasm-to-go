//go:build !generator

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	bytes_bce "github.com/stricttools/wasm-to-go/testdata/bytes/bce"
	bytes_oob_trap "github.com/stricttools/wasm-to-go/testdata/bytes/oob_trap"
	bytes_oob_trap_imported "github.com/stricttools/wasm-to-go/testdata/bytes/oob_trap_imported"
)

// The modules of testdata/bytes have every memory access written as byte
// operations (Test_translate_bytes); scripts/cross-targets.sh runs these
// tests on each target.

func Test_regression_bytes_oob_trap(t *testing.T) {
	requireBytes(t, "testdata/bytes/oob_trap/oob_trap.go")
	testOOBTrap(t, bytes_oob_trap.New())
}

func Test_regression_bytes_oob_trap_use_memory(t *testing.T) {
	m := bytes_oob_trap.New()
	if !m.UseMemory(make([]byte, 0, 16<<16)) {
		t.Fatal("UseMemory refused an array of 16 pages")
	}
	testOOBTrap(t, m)
}

func Test_regression_bytes_oob_trap_imported(t *testing.T) {
	requireBytes(t, "testdata/bytes/oob_trap_imported/oob_trap_imported.go")
	testOOBTrap(t, bytes_oob_trap_imported.New(spareEnv{&spareMemory{make([]byte, 1<<16, 4<<16)}}))
}

// Fails unless every memory access of the module's functions in file is
// written as bytes.
func requireBytes(t *testing.T, file string) {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range f.Decls {
		// The module's functions are methods; the helpers, which the
		// module no longer calls, are not.
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv != nil {
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "LittleEndian" {
					t.Errorf("%s writes an access as a call of encoding/binary, not as bytes", fd.Name.Name)
				}
				return true
			})
		}
	}
}

// The generic file, which the platforms outside passes.ExpandPlatforms
// build, has its accesses as bytes; the expanded file as unsafe loads.
func Test_regression_bytes_bce(t *testing.T) {
	testBCE(t, bytes_bce.New())
}
