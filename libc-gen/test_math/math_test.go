package mathtest

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"
)

var write = flag.String("write", "", "write the results to this file (expected.txt's format)")

// Operands as bits: zeros, ones, a subnormal, the extremes, arguments near
// where functions overflow or lose precision, infinities, and NaNs (the
// canonical one, a negative one with a payload, and a signaling one).
var edges = []uint64{
	0x0000000000000000, 0x8000000000000000, // ±0
	0x3ff0000000000000, 0xbff0000000000000, // ±1
	0x3fe0000000000000, 0x4000000000000000, // 0.5, 2
	0x400921fb54442d18,                     // pi
	0x0000000000000001, 0x0010000000000000, // smallest subnormal and normal
	0x7fefffffffffffff, 0xffefffffffffffff, // ±largest
	0x40862e42fefa39ef, // about 709.78, where exp overflows
	0x44b52d02c7e14af6, // 1e22
	0x7ff0000000000000, 0xfff0000000000000, // ±inf
	0x7ff8000000000000, 0xfff8000000000123, 0x7ff4000000000001, // NaNs
}

// A subset of edges for fma's three operands.
var fmaEdges = []uint64{
	0x0000000000000000, 0x8000000000000000, 0x3ff0000000000000, 0xbff0000000000000,
	0x0000000000000001, 0x7fefffffffffffff, 0x7ff0000000000000, 0xfff8000000000123,
}

// splitmix64, for random operands that are the same everywhere.
type rng uint64

func (r *rng) next() uint64 {
	*r += 0x9e3779b97f4a7c15
	z := uint64(*r)
	z = (z ^ z>>30) * 0xbf58476d1ce4e5b9
	z = (z ^ z>>27) * 0x94d049bb133111eb
	return z ^ z>>31
}

// operand returns a random double: any bits (a quarter of the time), or a
// random sign and mantissa with an exponent in [-1022, 1023], [-8, 8], or
// [-2, 2], so that most operands lie where the functions are interesting.
func (r *rng) operand() uint64 {
	x := r.next()
	mant := x & (1<<52 - 1)
	sign := x & (1 << 63)
	var exp uint64
	switch x >> 52 & 3 {
	case 0:
		return r.next()
	case 1:
		exp = 1 + r.next()%2046
	case 2:
		exp = 1023 - 8 + r.next()%17
	case 3:
		exp = 1023 - 2 + r.next()%5
	}
	return sign | exp<<52 | mant
}

const randoms = 64

// results calls every function over the edges (every combination of them,
// for two operands) and random operands, and returns one line per call:
// "name operands -> result", every value in hexadecimal bits.
func results() []string {
	m := New()
	var lines []string
	add := func(name string, args []uint64, res ...uint64) {
		var b strings.Builder
		b.WriteString(name)
		for _, a := range args {
			fmt.Fprintf(&b, " %#x", a)
		}
		b.WriteString(" ->")
		for _, r := range res {
			fmt.Fprintf(&b, " %#x", r)
		}
		lines = append(lines, b.String())
	}
	i := func(x uint64) int64 { return int64(x) }
	u := func(x int64) uint64 { return uint64(x) }

	one := []struct {
		name string
		f    func(int64) int64
		f2   func(int64) int64 // the second result, for frexp, modf, and lgamma_r
	}{
		{"acos", m.Xacos_, nil}, {"acosh", m.Xacosh_, nil}, {"asin", m.Xasin_, nil},
		{"asinh", m.Xasinh_, nil}, {"atan", m.Xatan_, nil}, {"atanh", m.Xatanh_, nil},
		{"cbrt", m.Xcbrt_, nil}, {"ceil", m.Xceil_, nil}, {"cos", m.Xcos_, nil},
		{"cosh", m.Xcosh_, nil}, {"erf", m.Xerf_, nil}, {"erfc", m.Xerfc_, nil},
		{"exp", m.Xexp_, nil}, {"exp2", m.Xexp2_, nil}, {"expm1", m.Xexpm1_, nil},
		{"fabs", m.Xfabs_, nil}, {"floor", m.Xfloor_, nil}, {"frexp", m.Xfrexp_, m.Xfrexp_2},
		{"ilogb", m.Xilogb_, nil}, {"j0", m.Xj0_, nil}, {"j1", m.Xj1_, nil},
		{"lgamma", m.Xlgamma_, nil}, {"lgamma_r", m.Xlgamma_r_, m.Xlgamma_r_2},
		{"llrint", m.Xllrint_, nil}, {"log", m.Xlog_, nil}, {"log10", m.Xlog10_, nil},
		{"log1p", m.Xlog1p_, nil}, {"log2", m.Xlog2_, nil}, {"logb", m.Xlogb_, nil},
		{"lrint", m.Xlrint_, nil}, {"modf", m.Xmodf_, m.Xmodf_2}, {"rint", m.Xrint_, nil},
		{"round", m.Xround_, nil}, {"roundeven", m.Xroundeven_, nil}, {"sin", m.Xsin_, nil},
		{"sinh", m.Xsinh_, nil}, {"sqrt", m.Xsqrt_, nil}, {"tan", m.Xtan_, nil},
		{"tanh", m.Xtanh_, nil}, {"tgamma", m.Xtgamma_, nil}, {"trunc", m.Xtrunc_, nil},
		{"y0", m.Xy0_, nil}, {"y1", m.Xy1_, nil},
	}
	for _, f := range one {
		r := rng(len(lines))
		ops := append([]uint64(nil), edges...)
		for range randoms {
			ops = append(ops, r.operand())
		}
		for _, a := range ops {
			res := u(f.f(i(a)))
			if f.f2 != nil {
				add(f.name, []uint64{a}, res, u(f.f2(i(a))))
			} else {
				add(f.name, []uint64{a}, res)
			}
		}
	}

	two := []struct {
		name string
		f    func(int64, int64) int64
	}{
		{"atan2", m.Xatan2_}, {"copysign", m.Xcopysign_}, {"fdim", m.Xfdim_},
		{"fmax", m.Xfmax_}, {"fmin", m.Xfmin_}, {"fmod", m.Xfmod_}, {"hypot", m.Xhypot_},
		{"nextafter", m.Xnextafter_}, {"pow", m.Xpow_}, {"remainder", m.Xremainder_},
	}
	for _, f := range two {
		r := rng(len(lines))
		for _, a := range edges {
			for _, b := range edges {
				add(f.name, []uint64{a, b}, u(f.f(i(a), i(b))))
			}
		}
		for range randoms {
			a, b := r.operand(), r.operand()
			add(f.name, []uint64{a, b}, u(f.f(i(a), i(b))))
		}
	}

	// ldexp, jn, and yn take an int: small and large ones (the extremes
	// only for ldexp, since jn and yn take time proportional to n).
	ints := []uint64{0, 1, 2, 5, 0xffffffffffffffff, 0xfffffffffffffc00, 1100}
	extremes := []uint64{0x7fffffff, 0xffffffff80000000}
	for _, f := range []struct {
		name  string
		f     func(int64, int64) int64
		first bool // the int is the first operand
	}{{"ldexp", m.Xldexp_, false}, {"jn", m.Xjn_, true}, {"yn", m.Xyn_, true}} {
		r := rng(len(lines))
		ops := append([]uint64(nil), edges...)
		for range randoms / 4 {
			ops = append(ops, r.operand())
		}
		ns := ints
		if !f.first {
			ns = append(ns[:len(ns):len(ns)], extremes...)
		}
		for _, n := range ns {
			for _, x := range ops {
				a, b := x, n
				if f.first {
					a, b = n, x
				}
				add(f.name, []uint64{a, b}, u(f.f(i(a), i(b))))
			}
		}
	}

	r := rng(len(lines))
	for _, a := range fmaEdges {
		for _, b := range fmaEdges {
			for _, c := range fmaEdges {
				add("fma", []uint64{a, b, c}, u(m.Xfma_(i(a), i(b), i(c))))
			}
		}
	}
	for range randoms {
		a, b, c := r.operand(), r.operand(), r.operand()
		add("fma", []uint64{a, b, c}, u(m.Xfma_(i(a), i(b), i(c))))
	}
	// Products that cancel c almost exactly, where a separate multiply and
	// add would differ from a fused one.
	for range randoms {
		a, b := r.operand()&^(0x7ff<<52)|0x3ff<<52, r.operand()&^(0x7ff<<52)|0x3ff<<52
		p := m.Xfma_(i(a), i(b), 0)
		c := u(p) ^ 1<<63
		add("fma", []uint64{a, b, c}, u(m.Xfma_(i(a), i(b), i(c))))
	}
	return lines
}

// Every C math function libc-gen provides returns the bits in expected.txt,
// on every architecture: they are computed by the translated module, not by
// host functions.
func Test_determinism_libm(t *testing.T) {
	got := results()
	if *write != "" {
		if err := os.WriteFile(*write, []byte(strings.Join(got, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile("expected.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(got) != len(want) {
		t.Fatalf("%d results, want %d", len(got), len(want))
	}
	bad := map[string]int{}
	var order []string
	for k := range got {
		if got[k] == want[k] {
			continue
		}
		name, _, _ := strings.Cut(want[k], " ")
		if bad[name] == 0 {
			order = append(order, name)
			if len(order) <= 20 {
				t.Errorf("got  %s\nwant %s", got[k], want[k])
			}
		}
		bad[name]++
	}
	if len(order) > 0 {
		var b strings.Builder
		for _, name := range order {
			fmt.Fprintf(&b, " %s (%d)", name, bad[name])
		}
		t.Errorf("results differ from expected.txt for:%s", b.String())
	}
}
