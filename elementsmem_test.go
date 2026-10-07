//go:build !generator && linux

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// elementsCompileBound is the most memory, in MiB of peak resident set
// size, compiling the output package of elementsModule(elementsCount)
// may take in Test_elements_compile_memory, the least peak of three
// compiles: above the 141 to 145 MiB measured with the segment written in
// chunks (maxElementsPerFunc), on linux/amd64, by the margin a loaded
// machine needs (one compile took 151 MiB in the whole suite). In one
// literal it took 710 to 734 MiB.
const elementsCompileBound = 160

const elementsCount = 2400

// elementsModule is a Wasm module of n functions, each adding a global
// and a constant of its own to its parameter, and a table holding them
// all, from one active element segment: enough code that it is written as
// several packages, and New's segment an entry per function, each a
// function literal binding the function to the module's instance.
func elementsModule(n int) []byte {
	leb := func(b []byte, v uint64) []byte {
		for {
			c := byte(v & 0x7f)
			v >>= 7
			if v != 0 {
				b = append(b, c|0x80)
				continue
			}
			return append(b, c)
		}
	}
	section := func(m []byte, id byte, body []byte) []byte {
		return append(leb(append(m, id), uint64(len(body))), body...)
	}
	m := []byte("\x00asm\x01\x00\x00\x00")
	m = section(m, 1, []byte{1, 0x60, 1, 0x7f, 1, 0x7f}) // (func (param i32) (result i32))
	funcs := leb(nil, uint64(n))
	for range n {
		funcs = append(funcs, 0)
	}
	m = section(m, 3, funcs)
	m = section(m, 4, leb([]byte{1, 0x70, 0}, uint64(n))) // funcref table, min n
	m = section(m, 6, []byte{1, 0x7f, 1, 0x41, 0, 0x0b})  // (global (mut i32) (i32.const 0))
	elems := []byte{1, 0, 0x41, 0, 0x0b}                  // one active segment at i32.const 0
	elems = leb(elems, uint64(n))
	for i := range n {
		elems = leb(elems, uint64(i))
	}
	m = section(m, 9, elems)
	code := leb(nil, uint64(n))
	for i := range n {
		body := []byte{0, 0x20, 0, 0x23, 0, 0x6a, 0x41} // no locals; local.get 0; global.get 0; i32.add; i32.const i
		body = leb(body, uint64(i)&0x3f)                // a small constant, a one-byte LEB128
		body = append(body, 0x6a, 0x0b)                 // i32.add; end
		code = append(leb(code, uint64(len(body))), body...)
	}
	return section(m, 10, code)
}

// The output package of a module whose element segment has an entry per
// function, thousands of them, compiles under elementsCompileBound (the
// least peak of three compiles).
func Test_elements_compile_memory(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	wasm := filepath.Join(dir, "elements.wasm")
	if err := os.WriteFile(wasm, elementsModule(elementsCount), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "m", "m.go")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self, translateFlag, "-pkg", "m", "-importpath", "elemtest/m", "-o", out, wasm)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("translating: %v\n%s", err, b)
	}
	if _, err := os.Stat(filepath.Join(dir, "m", "internal")); err != nil {
		t.Fatalf("the module is not written as several packages: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module elemtest\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The least peak of three compiles: the collector's timing moves one
	// compile's peak by a tenth.
	least := 0.0
	for i := range 3 {
		// A constant unique to this compile, so the build cache does not
		// hold the package and it is compiled, and measured.
		nonce := "package m\n\nconst _ = \"" + strconv.FormatInt(time.Now().UnixNano(), 10) + "\"\n"
		if err := os.WriteFile(filepath.Join(dir, "m", "zz_nonce.go"), []byte(nonce), 0o644); err != nil {
			t.Fatal(err)
		}
		log := filepath.Join(dir, "compile"+strconv.Itoa(i)+".log")
		build := exec.Command("go", "build", "-p", "1", "-toolexec", self+" "+toolexecFlag+log, "-o", os.DevNull, "./m")
		build.Dir = dir
		build.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "GOMAXPROCS=4", "GOFLAGS=", "GOTOOLCHAIN=local",
			"CGO_ENABLED=0", "GOGC=100", "GOMEMLIMIT=off", "GOEXPERIMENT=")
		if b, err := build.CombinedOutput(); err != nil {
			t.Fatalf("go build: %v\n%s", err, b)
		}
		peaks, err := readCompileLog(log, "elemtest/m")
		if err != nil {
			t.Fatal(err)
		}
		mib, ok := peaks["elemtest/m"]
		if !ok {
			t.Fatal("the output package's compile was not measured")
		}
		t.Logf("compiling the output package took %.0f MiB", mib)
		if i == 0 || mib < least {
			least = mib
		}
	}
	if least > elementsCompileBound {
		t.Errorf("compiling the output package took at least %.0f MiB, over the bound (%d MiB)", least, elementsCompileBound)
	}
}
