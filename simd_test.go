//go:build !generator

package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	bytes_simd_api "github.com/stricttools/wasm-to-go/testdata/bytes/simd_api"
	packages_simd_api "github.com/stricttools/wasm-to-go/testdata/packages/simd_api"
	simd_api_test "github.com/stricttools/wasm-to-go/testdata/regression/simd_api"
	simd_shr_s_test "github.com/stricttools/wasm-to-go/testdata/regression/simd_shr_s"
	unsafe_simd_api "github.com/stricttools/wasm-to-go/testdata/unsafe/simd_api"
)

// i32x4 is the bytes of a v128 of four i32 lanes, as a Module's exports,
// imports, and globals take and give them.
func i32x4(a, b, c, d uint32) (v [16]byte) {
	for i, x := range []uint32{a, b, c, d} {
		binary.LittleEndian.PutUint32(v[4*i:], x)
	}
	return v
}

// The host of the simd_api module: double doubles each i32 lane, and g is
// the imported global.
type simdHost struct{ g [16]byte }

func (h *simdHost) Xdouble(v [16]byte) [16]byte {
	for i := 0; i < 16; i += 4 {
		binary.LittleEndian.PutUint32(v[i:], 2*binary.LittleEndian.Uint32(v[i:]))
	}
	return v
}

func (h *simdHost) Xg() *[16]byte { return &h.g }

type simdAPIModule interface {
	Xadd(v [16]byte) [16]byte
	Xset(v [16]byte)
	XimportedLane() int32
	Xdispatch(i int32, v [16]byte) [16]byte
	Xchoose(c int32, a, b [16]byte) [16]byte
	Xsum(n int32) int32
	Xg() *[16]byte
}

// v128 values cross the Module's exports, imports, and globals as their
// bytes, and pass through a closed table's indirect call, select, a
// block's parameter and result, and a loop, on whichever SIMD code the
// build selects (the portable code, or simd/archsimd's).
func Test_regression_simd_api(t *testing.T) {
	for name, newModule := range map[string]func(h *simdHost) (simdAPIModule, []byte){
		"one package": func(h *simdHost) (simdAPIModule, []byte) {
			m := simd_api_test.New(h)
			return m, *m.Xmemory().Slice()
		},
		"several packages": func(h *simdHost) (simdAPIModule, []byte) {
			m := packages_simd_api.New(h)
			return m, *m.Xmemory().Slice()
		},
		"memory accesses as bytes": func(h *simdHost) (simdAPIModule, []byte) {
			m := bytes_simd_api.New(h)
			return m, *m.Xmemory().Slice()
		},
		"-unsafe": func(h *simdHost) (simdAPIModule, []byte) {
			m := unsafe_simd_api.New(h)
			return m, *m.Xmemory().Slice()
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := &simdHost{}
			m, mem := newModule(h)
			if got, want := *m.Xg(), i32x4(1, 2, 3, 4); got != want {
				t.Errorf("g = %x, want %x", got, want)
			}
			if got, want := m.Xadd(i32x4(10, 20, 30, 0xffffffff)), i32x4(22, 44, 66, 6); got != want {
				t.Errorf("add = %x, want %x", got, want)
			}
			m.Xset(i32x4(5, 6, 7, 8))
			if got, want := *m.Xg(), i32x4(5, 6, 7, 8); got != want {
				t.Errorf("g after set = %x, want %x", got, want)
			}
			if got := m.XimportedLane(); got != 7 {
				t.Errorf("importedLane = %d, want 7", got)
			}
			if got, want := m.Xdispatch(0, i32x4(1, 2, 3, 4)), i32x4(2, 3, 4, 5); got != want {
				t.Errorf("dispatch(0) = %x, want %x", got, want)
			}
			if got, want := m.Xdispatch(1, i32x4(1, 2, 3, 4)), i32x4(-1&0xffffffff, 0xfffffffe, 0xfffffffd, 0xfffffffc); got != want {
				t.Errorf("dispatch(1) = %x, want %x", got, want)
			}
			func() {
				defer func() {
					if recover() == nil {
						t.Errorf("dispatch(2) did not trap")
					}
				}()
				m.Xdispatch(2, i32x4(1, 2, 3, 4))
			}()
			a, b := i32x4(1, 1, 1, 1), i32x4(2, 2, 2, 2)
			if got := m.Xchoose(1, a, b); got != a {
				t.Errorf("choose(1) = %x, want %x", got, a)
			}
			if got := m.Xchoose(0, a, b); got != b {
				t.Errorf("choose(0) = %x, want %x", got, b)
			}
			for i := range 64 {
				binary.LittleEndian.PutUint32(mem[4*i:], uint32(i))
			}
			if got, want := m.Xsum(16), int32(64*63/2); got != want {
				t.Errorf("sum(16) = %d, want %d", got, want)
			}
		})
	}
}

// The arithmetic right shift of every lane width by a variable count keeps
// each lane's sign, whatever the bits of the neighboring lanes: Go's mips
// and mipsle compilers sign-extend an int8 shifted by a variable from 16
// bits instead of 8 (MIPS.rules' Rsh8x32), so int8(x)>>s there shifts the
// next lane's byte into the result.
func Test_regression_simd_shr_s(t *testing.T) {
	m := simd_shr_s_test.New()
	shifts := []func(v [16]byte, s int32) [16]byte{m.Xi8x16, m.Xi16x8, m.Xi32x4, m.Xi64x2}
	for i, width := range []int{8, 16, 32, 64} {
		bytes := width / 8
		for _, s := range []int32{0, 1, 3, int32(width) - 1, int32(width) + 1, -1} {
			for b := range 256 {
				// Every other lane's top byte is b, and the other bytes
				// are 0x01, 0x80, and 0x7f: no lane's sign extends
				// through its neighbor.
				var v [16]byte
				for j := range v {
					v[j] = [...]byte{0x01, 0x80, 0x7f}[j%3]
					if j%bytes == bytes-1 && j/bytes%2 == 0 {
						v[j] = byte(b)
					}
				}
				got := shifts[i](v, s)
				var want [16]byte
				for l := 0; l < 16; l += bytes {
					var x uint64
					for k := bytes - 1; k >= 0; k-- {
						x = x<<8 | uint64(v[l+k])
					}
					n := uint(64 - width)
					y := uint64(int64(x<<n)>>n>>(uint(s)&uint(width-1))) // sign-extended, shifted
					for k := range bytes {
						want[l+k] = byte(y >> (8 * k))
					}
				}
				if got != want {
					t.Fatalf("i%dx%d.shr_s(%x, %d) = %x, want %x", width, 128/width, v, s, got, want)
				}
			}
		}
	}
}

// A module with SIMD instructions needs -o, which its refusal names:
// written with a file creator, as -o gives, it translates.
func Test_simd_needs_output_files(t *testing.T) {
	src, err := os.ReadFile("testdata/regression/simd_api/simd_api.wasm")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = translate(bytes.NewReader(src), &out, nil, nil, "")
	if err == nil || !strings.Contains(err.Error(), "needs -o") {
		t.Fatalf("without -o: got %v, want an error naming -o", err)
	}
	dir := t.TempDir()
	files := newPackageFiles(dir)
	err = translate(bytes.NewReader(src), &out, nil, files.create, "simd_api.go")
	if cerr := files.close(); err == nil {
		err = cerr
	}
	if err != nil {
		t.Fatalf("with output files: %v", err)
	}
}

// A module with a relaxed SIMD instruction is refused, naming the build
// flag that brings it in.
func Test_simd_relaxed_refused(t *testing.T) {
	// (func (param v128) (result v128) (i32x4.relaxed_trunc_f32x4_s (local.get 0)))
	src := []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x06, 0x01, 0x60, 0x01, 0x7b, 0x01, 0x7b, // type section
		0x03, 0x02, 0x01, 0x00, // function section
		0x0a, 0x09, 0x01, 0x07, 0x00, 0x20, 0x00, 0xfd, 0x81, 0x02, 0x0b, // code section
	}
	files := newPackageFiles(t.TempDir())
	var out bytes.Buffer
	err := translate(bytes.NewReader(src), &out, nil, files.create, "relaxed.go")
	files.close()
	if err == nil || !strings.Contains(err.Error(), "relaxed SIMD") || !strings.Contains(err.Error(), "build without -mrelaxed-simd") {
		t.Fatalf("got %v, want a relaxed SIMD refusal naming -mrelaxed-simd", err)
	}
}

// A build calling simd/archsimd with a Go version the SIMD code was not
// written for fails, naming the ways out, which work: Go 1.27, or no
// GOEXPERIMENT=simd.
func Test_simd_untested_go_version(t *testing.T) {
	build := func(env ...string) (string, error) {
		cmd := exec.Command("go", "build", "-o", os.DevNull, "./testdata/regression/simd_api")
		cmd.Env = append(os.Environ(), append([]string{"GOOS=linux", "GOARCH=amd64", "GOAMD64=v3", "GOFLAGS=", "GOTOOLCHAIN=local", "CGO_ENABLED=0"}, env...)...)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	out, err := build("GOEXPERIMENT=simd")
	if strings.HasPrefix(runtime.Version(), "go1.27") {
		if err != nil {
			t.Fatalf("GOEXPERIMENT=simd with %s: %v\n%s", runtime.Version(), err, out)
		}
	} else if err == nil || !strings.Contains(out, simdUntested) {
		t.Fatalf("GOEXPERIMENT=simd with %s: got %v\n%s\nwant the build to fail naming %s", runtime.Version(), err, out, simdUntested)
	}
	if out, err := build("GOEXPERIMENT=nosimd"); err != nil {
		t.Fatalf("without GOEXPERIMENT=simd: %v\n%s", err, out)
	}
}

// Translating again into the same directory removes the SIMD files of an
// earlier translation that this one does not write.
func Test_simd_stale_files(t *testing.T) {
	dir := t.TempDir()
	if err := translateFile("testdata/regression/simd_api/simd_api.wasm", dir+"/module"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir + "/module_simd.go"); err != nil {
		t.Fatalf("the SIMD file was not written: %v", err)
	}
	if err := translateFile("testdata/fib/fib.wasm", dir+"/module"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir + "/module_simd.go"); err == nil {
		t.Errorf("module_simd.go of the earlier translation is left")
	}
}
