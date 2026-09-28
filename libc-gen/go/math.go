package libc

import "math"

// fma is the only math function libc-gen provides in Go; the others are
// musl's, compiled into the module. IEEE 754 defines fma's result exactly
// (x*y+z rounded once), so math.FMA gives the same bits on every CPU,
// whether it uses an instruction or software, and it is many times faster
// than musl's fma compiled to WebAssembly, which has no fused multiply-add.
// Only a NaN result can differ by CPU, so it is the positive canonical NaN,
// as the translator makes every arithmetic NaN (f64_canon is its helper).
func fma(x, y, z float64) float64 { return f64_canon(math.FMA(x, y, z)) }
