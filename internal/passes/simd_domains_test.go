package passes

import "testing"

// A vector variable whose every value is a float operation's of one lane
// shape holds floats; a parameter, a variable of two shapes, or of a load
// holds words; a constant and the zero fit any domain.
func TestPortableDomains(t *testing.T) {
	fn := parseFunc(t, `func f(v0 vec128) vec128 {
		var v1, v2, v3, v4 vec128
		v1 = simd_v128_const(0, 0, 0, 0)
		v1 = simd_f32x4_add(v1, v0)
		v2 = simd_f64x2_mul(v0, v0)
		v3 = simd_f32x4_mul(v0, v0)
		v3 = simd_f64x2_mul(v0, v0)
		t0 := simd_v128_load(q16)
		v4 = v1
		return v4
	}`)
	want := map[string]string{"v0": "bits", "v1": "f32", "v2": "f64", "v3": "bits", "t0": "bits", "v4": "f32"}
	got := portableDomains(fn)
	for v, d := range want {
		if got[v] != d {
			t.Errorf("domain of %s = %q, want %q", v, got[v], d)
		}
	}
}
