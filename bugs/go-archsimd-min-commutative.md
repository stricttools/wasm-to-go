# cmd/compile: archsimd Float32x4.Min and Max are treated as commutative on amd64

Status: draft, not filed.

## Symptom

On amd64 with GOEXPERIMENT=simd, `a.Min(b)` and `b.Min(a)` of
`simd/archsimd.Float32x4` compile to one VMINPS: the compiler treats the
operation as commutative and eliminates the second as a common
subexpression. VMINPS is not commutative: when the operands are equal (+0
and -0) or either is a NaN, it returns its second source operand. So
`a.Min(b).ToBits().Or(b.Min(a).ToBits())`, which combines both orders to
get -0 from +0 and -0, computes `x | x` and gives +0 for one operand order.

## Minimal reproduction

```go
package main

import (
	"fmt"
	"math"
	"simd/archsimd"
)

//go:noinline
func both(a, b archsimd.Float32x4) archsimd.Uint32x4 {
	return a.Min(b).ToBits().Or(b.Min(a).ToBits())
}

//go:noinline
func one(a, b archsimd.Float32x4) archsimd.Uint32x4 { return a.Min(b).ToBits() }

func main() {
	p := archsimd.BroadcastFloat32x4(0)
	n := archsimd.BroadcastFloat32x4(float32(math.Copysign(0, -1)))
	fmt.Printf("one(+0,-0)=%#x one(-0,+0)=%#x both(+0,-0)=%#x both(-0,+0)=%#x\n",
		one(p, n).GetElem(0), one(n, p).GetElem(0), both(p, n).GetElem(0), both(n, p).GetElem(0))
}
```

`GOEXPERIMENT=simd GOAMD64=v3 go run .` prints

```
one(+0,-0)=0x80000000 one(-0,+0)=0x0 both(+0,-0)=0x0 both(-0,+0)=0x80000000
```

where `both` should give 0x80000000 for both orders. `go build -gcflags=-S`
shows `both` as `VMINPS X0, X1, X1; VPOR X1, X1, X0`: one VMINPS, or'ed
with itself.

## Versions and platforms

- go1.27.1 linux/amd64 (AMD Ryzen 7 6800HS, AVX2, no AVX-512).
- go1.28-devel_2ff5743d (gotip of 2026-09-26) linux/amd64: the same.
- Float32x4.Max is expected to behave alike (VMAXPS); Float64x2's Min and
  Max were not checked separately.

## Evidence

The WebAssembly SIMD spec tests (simd_f32x4.wast, simd_f64x2.wast) for
f32x4.min and f32x4.max of opposite zeros failed in wasm2go's amd64
translation written with both operand orders, and pass with the
workaround below.

## Workaround in wasm2go

The translation no longer relies on the operand order: where the
operands compare equal it takes the or (min) or and (max) of their bits,
which is what the two orders give for +0 and -0, and VMINPS's result
elsewhere (internal/passes/simd_archsimd.go).

## Where to file

github.com/golang/go issue, title `cmd/compile: archsimd Float32x4.Min is
treated as commutative on amd64`, with the program above; the fix is in
the SIMD op table of cmd/compile (the generated ops for VMINPS/VMAXPS must
not be marked commutative).
