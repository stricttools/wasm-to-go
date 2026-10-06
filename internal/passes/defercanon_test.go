package passes

import "testing"

func TestDeferCanon(t *testing.T) {
	for _, tt := range []struct {
		name     string
		src      string
		deferred int
		want     string // "" for unchanged
	}{{
		// A local assigned canonicalized results holds them raw; its uses
		// in arithmetic and comparisons, min, and a conversion to integer
		// take it raw, and a store, a reinterpretation, neg, and a call
		// canonicalize it.
		name: "uses",
		src: `func (m *Module) f(v0, v1 float32) int32 {
			var v2 float32
			v2 = f32_canon(float32(v0 + v1))
			v2 = f32_canon(float32(v2 * v1))
			if v2 < v1 {
				m.g(v2)
			}
			t0 := f32_min(v2, v1)
			t1 := i32_trunc_sat_f32_s(v2)
			store32(m.memory, 0, math.Float32bits(v2))
			return int32(math.Float32bits(f32_neg(v2))) + t1 + int32(math.Float32bits(t0))
		}`,
		deferred: 1,
		want: `func (m *Module) f(v0, v1 float32) int32 {
			var v2 float32
			v2 = float32(v0 + v1)
			v2 = float32(v2 * v1)
			if v2 < v1 {
				m.g(f32_canon(v2))
			}
			t0 := f32_min(v2, v1)
			t1 := i32_trunc_sat_f32_s(v2)
			store32(m.memory, 0, math.Float32bits(f32_canon(v2)))
			return int32(math.Float32bits(f32_neg(f32_canon(v2)))) + t1 + int32(math.Float32bits(t0))
		}`,
	}, {
		// A chain through two locals canonicalizes where it leaves them;
		// a constant that is not a NaN and an integer converted to a
		// float keep a local deferred.
		name: "chain",
		src: `func (m *Module) f(v0 float64, v1 int32) float64 {
			var v2, v3 float64
			v2 = math.Float64frombits(0x3ff0000000000000)
			v2 = f64_canon(float64(v2 * v0))
			v3 = float64(v1)
			v3 = f64_canon(float64(v2 + v3))
			return v3
		}`,
		deferred: 2,
		want: `func (m *Module) f(v0 float64, v1 int32) float64 {
			var v2, v3 float64
			v2 = math.Float64frombits(0x3ff0000000000000)
			v2 = float64(v2 * v0)
			v3 = float64(v1)
			v3 = float64(v2 + v3)
			return f64_canon(v3)
		}`,
	}, {
		// A local also assigned a loaded value, a parameter, or a NaN
		// constant other than the canonical one stays as it is.
		name: "unsafe values",
		src: `func (m *Module) f(v0 float32) float32 {
			var v1, v2, v3 float32
			v1 = f32_canon(float32(v0 + v0))
			v1 = math.Float32frombits(load32(m.memory, 0))
			v2 = f32_canon(float32(v0 + v0))
			v2 = v0
			v3 = f32_canon(float32(v0 + v0))
			v3 = math.Float32frombits(0x7fc00001)
			return v1 + v2 + v3
		}`,
	}, {
		// A local holding only a constant and a min gains nothing.
		name: "nothing raw",
		src: `func (m *Module) f(v0 float32) float32 {
			var v1 float32
			v1 = math.Float32frombits(0x3f800000)
			v1 = f32_min(v1, v0)
			return v1
		}`,
	}, {
		// A local inside a function literal is left alone.
		name: "function literal",
		src: `func (m *Module) f(v0 float32) func() float32 {
			var v1 float32
			v1 = f32_canon(float32(v0 + v0))
			return func() float32 { return v1 }
		}`,
	}, {
		// Discarding a value, and assigning it to another deferred local
		// through a multi-value assignment, take it raw; a float
		// conversion of it (promote) under a canonicalization too.
		name: "assignments",
		src: `func (m *Module) f(v0 float32) float64 {
			var v1, v2 float32
			v1 = f32_canon(float32(v0 * v0))
			_ = v1
			v2, v1 = v1, f32_canon(float32(v0 - v0))
			return f64_canon(float64(v2))
		}`,
		deferred: 2,
		want: `func (m *Module) f(v0 float32) float64 {
			var v1, v2 float32
			v1 = float32(v0 * v0)
			_ = v1
			v2, v1 = v1, float32(v0-v0)
			return f64_canon(float64(v2))
		}`,
	}} {
		t.Run(tt.name, func(t *testing.T) {
			fn := parseFunc(t, tt.src)
			if got := DeferCanon(fn); got != tt.deferred {
				t.Errorf("DeferCanon = %d, want %d", got, tt.deferred)
			}
			want := tt.want
			if want == "" {
				want = tt.src
			}
			if got, want := formatFunc(t, fn), normalizeFunc(t, want); got != want {
				t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
			}
		})
	}
}
