//go:build !generator

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	determinism_test "github.com/stricttools/wasm-to-go/testdata/determinism"
	packages_bce "github.com/stricttools/wasm-to-go/testdata/packages/bce"
	packages_determinism "github.com/stricttools/wasm-to-go/testdata/packages/determinism"
	packages_dispatch "github.com/stricttools/wasm-to-go/testdata/packages/dispatch"
	packages_loops "github.com/stricttools/wasm-to-go/testdata/packages/loops"
	packages_memgrow "github.com/stricttools/wasm-to-go/testdata/packages/memgrow"
	packages_provided_helper "github.com/stricttools/wasm-to-go/testdata/packages/provided_helper"
	packages_recursion "github.com/stricttools/wasm-to-go/testdata/packages/recursion"
)

// The modules of testdata/packages are written with one function per
// package (Test_translate_packages): each runs the tests of the module
// written as one package.

// Every function has a package of its own, and calls cross packages both
// ways: forward directly, backward through the instance package.
func Test_packages_layout(t *testing.T) {
	dirs, err := filepath.Glob("testdata/packages/recursion/internal/functions*")
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 3 {
		t.Errorf("found %d function packages for recursion's 3 functions, want 3", len(dirs))
	}
	src, err := os.ReadFile("testdata/packages/recursion/internal/instance/instance.go")
	if err != nil {
		t.Fatal(err)
	}
	// is_even and is_odd call each other. Both are recursive, so their
	// exports are wrappers and the functions keep their own names
	// (stackbound.go): is_even is fn1.
	if !strings.Contains(string(src), "Fn1 func(") {
		t.Errorf("the instance package has no variable for the backward call of is_even:\n%s", src)
	}
}

func Test_regression_packages_recursion(t *testing.T) {
	testFactorial(t, &packages_recursion.Module{})
	testEvenOdd(t, &packages_recursion.Module{})
}

// loops imports its memory, which the functions' packages and the output
// package's New use; the output package also holds user.go, whose names
// are those of the translation's packages.
func Test_regression_packages_loops(t *testing.T) {
	env := new(loopsEnv)
	testLoops(t, packages_loops.New(env), env)
}

func Test_regression_packages_dispatch(t *testing.T) {
	testDispatch(t, packages_dispatch.New())
}

func Test_regression_packages_memgrow(t *testing.T) {
	env := &memgrowEnv{}
	m := packages_memgrow.New(env)
	env.m = m
	testMemgrow(t, m)
}

// The provided function's receiver is named mod.
func Test_regression_packages_provided_helper(t *testing.T) {
	testProvidedHelper(t, packages_provided_helper.New())
}

func Test_regression_packages_bce(t *testing.T) {
	testBCE(t, packages_bce.New())
}

// The results of every float instruction are the same as the module's
// written as one package, and as expected.txt (scripts/cross-targets.sh
// runs this test on each target).
func Test_determinism_packages(t *testing.T) {
	want := determinism_test.Results()
	got := determinism_test.ResultsOf(packages_determinism.New())
	if len(got) != len(want) {
		t.Fatalf("got %d results, want %d", len(got), len(want))
	}
	diffs := 0
	for i := range want {
		if got[i] != want[i] {
			if diffs++; diffs <= 20 {
				t.Errorf("got  %s\nwant %s", got[i], want[i])
			}
		}
	}
}

// A module larger than a package needs -importpath; given, the error
// clears.
func Test_packages_importpath(t *testing.T) {
	limit, path := maxPackageSize, *importPath
	t.Cleanup(func() { maxPackageSize, *importPath = limit, path })
	maxPackageSize, *importPath = 1, ""

	dir := t.TempDir()
	translateTo := func() error { return translateRecursion(t, dir) }
	err := translateTo()
	if err == nil || !strings.Contains(err.Error(), "-importpath") {
		t.Fatalf("translate = %v, want an error naming -importpath", err)
	}
	*importPath = "example.com/recursion"
	if err := translateTo(); err != nil {
		t.Fatalf("with -importpath, translate = %v", err)
	}
}

// A translation into a directory removes the packages an earlier one wrote
// there and this one did not, and refuses to remove a file it did not
// write.
func Test_packages_stale(t *testing.T) {
	limit, path := maxPackageSize, *importPath
	t.Cleanup(func() { maxPackageSize, *importPath = limit, path })
	maxPackageSize, *importPath = 1, "example.com/recursion"

	dir := t.TempDir()
	stale := filepath.Join(dir, "internal", "functions9", "functions9.go")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte(generatedLine+"\n\npackage functions9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	translateTo := func() error { return translateRecursion(t, dir) }
	if err := translateTo(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(stale)); !os.IsNotExist(err) {
		t.Errorf("the stale package %s is still there (%v)", filepath.Dir(stale), err)
	}
	if _, err := os.Stat(filepath.Join(dir, "internal", "functions3", "functions3.go")); err != nil {
		t.Errorf("the translation's own package is missing: %v", err)
	}

	foreign := filepath.Join(dir, "internal", "functions2", "notes.txt")
	if err := os.WriteFile(foreign, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := translateTo(); err == nil || !strings.Contains(err.Error(), "not written by wasm2go") {
		t.Errorf("translate with a foreign file in a package directory = %v, want an error naming it", err)
	}
	// Moved, the error clears.
	if err := os.Rename(foreign, filepath.Join(dir, "notes.txt")); err != nil {
		t.Fatal(err)
	}
	if err := translateTo(); err != nil {
		t.Errorf("with the file moved, translate = %v", err)
	}
}

// Translates recursion.wasm into dir, as the command does with -o.
func translateRecursion(t *testing.T, dir string) error {
	in, err := os.Open("testdata/recursion/recursion.wasm")
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	files := newPackageFiles(dir)
	var out bytes.Buffer
	err = translate(in, &out, nil, files.create)
	if cerr := files.close(); err == nil {
		err = cerr
	}
	return err
}

// -noopt, which turns the optimization passes off, still writes a large
// module as several packages: the limit bounds compile memory, whatever
// the passes.
func Test_packages_noopt(t *testing.T) {
	limit, path, off := maxPackageSize, *importPath, *noopt
	t.Cleanup(func() { maxPackageSize, *importPath, *noopt = limit, path, off })
	maxPackageSize, *importPath, *noopt = 1, "example.com/recursion", true

	dir := t.TempDir()
	if err := translateRecursion(t, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "internal", "functions3", "functions3.go")); err != nil {
		t.Errorf("with -noopt, the functions are not in packages of their own: %v", err)
	}
}
