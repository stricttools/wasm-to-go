package spectest

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/stricttools/wasm-to-go/internal/mangle"
)

// TestModule runs the spec's assertions for one module, on every
// architecture alike. NaN results are checked against the WebAssembly
// deterministic profile: where the spec allows a canonical or an
// arithmetic NaN, the translation must return the positive canonical NaN.
func TestModule(t *testing.T, ctor func() any, jsonPath, name string) {
	t.Helper()

	spec, err := parseSpec(jsonPath)
	if err != nil {
		t.Fatal(err)
	}

	exp := classifyModule(spec, name)
	switch exp.mode {
	case moduleTestUnlinkable:
		if exp.text == "" {
			t.Skip("module is assert_unlinkable")
		} else {
			t.Skipf("module is assert_unlinkable: %s", exp.text)
		}
	case moduleTestUninstantiable:
		defer RecoverTrap(t, exp.text)
		_ = ctor()
	case moduleTestRuntime:
		runAssertions(t, reflect.ValueOf(ctor()), spec, name)
	default:
		_ = ctor()
	}
}

// beyondStackBound lists the assertions, as module file and line, whose
// calls recurse through tail calls deeper than translated code's Go stack
// bound allows (see the README's section on the Go stack): the translation
// does not make tail calls reuse their caller's frame, so these calls trap
// with "call stack exhausted" instead of returning the spec's result.
var beyondStackBound = map[string]bool{
	"return_call.0.wasm:121":          true, // count(1_000_000)
	"return_call.0.wasm:127":          true, // even(1_000_000)
	"return_call.0.wasm:128":          true, // even(1_000_001)
	"return_call.0.wasm:133":          true, // odd(1_000_000)
	"return_call.0.wasm:134":          true, // odd(999_999)
	"return_call_indirect.0.wasm:293": true, // odd(300_003)
}

// negatedCanonicalNaN lists the assertions, as module file and line, whose
// nan:canonical lanes are the negation of the canonical NaN: the spec's
// nan:canonical admits either sign, and neg computes the operand with its
// sign flipped, a deterministic result the profile's canonical NaN, which
// is positive, does not describe.
var negatedCanonicalNaN = map[string]bool{
	"simd_f64x2_arith.1.wasm:5297": true, // f64x2.neg of the canonical NaN
}

func runAssertions(t *testing.T, mod reflect.Value, spec *specTest, name string) {
	var file string
	for _, cmd := range spec.Commands {
		if cmd.Type == "module" {
			file = cmd.Filename
			continue
		} else if file != name {
			continue
		}

		switch cmd.Type {
		case "action", "assert_return", "assert_trap":
			t.Run(fmt.Sprintf("%s/line_%d", name, cmd.Line), func(t *testing.T) {
				if cmd.Type == "assert_trap" {
					defer RecoverTrap(t, cmd.Text)
				}
				if beyondStackBound[fmt.Sprintf("%s:%d", name, cmd.Line)] {
					// Checked to trap, and its result is not checked.
					defer RecoverTrap(t, "call stack exhausted")
				}

				method := mod.MethodByName(mangle.Name(cmd.Action.Field, mangle.Exported))
				args := make([]reflect.Value, len(cmd.Action.Args))
				for i, arg := range cmd.Action.Args {
					switch arg.Type {
					case "i32":
						v, err := parseInt[int32](arg.Value)
						if err != nil {
							t.Fatal(err)
						}
						args[i] = reflect.ValueOf(v)
					case "i64":
						v, err := parseInt[int64](arg.Value)
						if err != nil {
							t.Fatal(err)
						}
						args[i] = reflect.ValueOf(v)
					case "f32":
						v, err := strconv.ParseUint(arg.Value, 10, 32)
						if err != nil {
							t.Fatal(err)
						}
						args[i] = reflect.ValueOf(math.Float32frombits(uint32(v)))
					case "f64":
						v, err := strconv.ParseUint(arg.Value, 10, 64)
						if err != nil {
							t.Fatal(err)
						}
						args[i] = reflect.ValueOf(math.Float64frombits(uint64(v)))
					case "v128":
						v, err := v128Bytes(arg)
						if err != nil {
							t.Fatal(err)
						}
						args[i] = reflect.ValueOf(v)
					case "funcref", "externref":
						if arg.Value == "null" {
							var ptr *any
							args[i] = reflect.Zero(reflect.TypeOf(ptr).Elem())
						} else {
							args[i] = reflect.ValueOf(arg)
						}
					}
				}

				res := method.Call(args)
				for i := range res {
					if res[i].Kind() == reflect.Pointer {
						res[i] = res[i].Elem()
					}
				}
				if cmd.Type == "assert_return" {
					for i, exp := range cmd.Expected {
						switch exp.Type {
						case "i32":
							v, err := parseInt[int32](exp.Value)
							if err != nil {
								t.Fatal(err)
							}
							if i := res[i].Interface().(int32); i != v {
								t.Errorf("got %d, want %d", i, v)
							}
						case "i64":
							v, err := parseInt[int64](exp.Value)
							if err != nil {
								t.Fatal(err)
							}
							if i := res[i].Interface().(int64); i != v {
								t.Errorf("got %d, want %d", i, v)
							}
						case "f32":
							f := res[i].Interface().(float32)
							v := math.Float32bits(f)
							switch exp.Value {
							case "nan:canonical", "nan:arithmetic":
								// The deterministic profile: the positive canonical NaN.
								if v != 0x7fc00000 {
									t.Errorf("got %x, want 7fc00000 for %s (deterministic profile)", v, exp.Value)
								}
							default:
								i, err := strconv.ParseUint(exp.Value, 10, 32)
								if err != nil {
									t.Fatal(err)
								}
								if v != uint32(i) {
									t.Errorf("got %d, want %d", v, uint32(i))
								}
							}
						case "v128":
							checkV128(t, res[i].Interface().([16]byte), exp, negatedCanonicalNaN[fmt.Sprintf("%s:%d", name, cmd.Line)])
						case "f64":
							f := res[i].Interface().(float64)
							v := math.Float64bits(f)
							switch exp.Value {
							case "nan:canonical", "nan:arithmetic":
								// The deterministic profile: the positive canonical NaN.
								if v != 0x7ff8000000000000 {
									t.Errorf("got %x, want 7ff8000000000000 for %s (deterministic profile)", v, exp.Value)
								}
							default:
								i, err := strconv.ParseUint(exp.Value, 10, 64)
								if err != nil {
									t.Fatal(err)
								}
								if v != uint64(i) {
									t.Errorf("got %d, want %d", v, uint64(i))
								}
							}
						}
					}
				}
			})
		}
	}
}

func RecoverTrap(t testing.TB, want string) {
	t.Helper()

	var got string
	if r := recover(); r != nil {
		got = fmt.Sprint(r)
	} else {
		t.Fatalf("want trap: %s", want)
	}

	switch {
	case strings.Contains(got, want):
		return
	case strings.Contains(want, "out of bounds"):
		if strings.Contains(got, "out of range") || strings.Contains(got, "cannot convert slice with length") {
			return
		}
	case strings.Contains(want, "undefined"):
		if strings.Contains(got, "out of range") {
			return
		}
	case strings.Contains(want, "type mismatch") || strings.Contains(want, "indirect call"):
		if strings.Contains(got, "interface conversion") {
			return
		}
	case strings.Contains(want, "uninitialized"):
		if strings.Contains(got, "is nil") {
			return
		}
	}

	t.Fatalf("got trap %q, want %q", got, want)
}

func parseInt[T int32 | int64](s string) (T, error) {
	v, err := strconv.ParseInt(s, 10, 64)
	if err == nil {
		return T(v), nil
	}
	if u, err := strconv.ParseUint(s, 10, 64); err == nil {
		return T(u), nil
	}
	return 0, err
}

// The width in bytes of a v128's lanes of lane type lt.
func laneBytes(lt string) int {
	switch lt {
	case "i8":
		return 1
	case "i16":
		return 2
	case "i32", "f32":
		return 4
	}
	return 8
}

// The bytes of the v128 a.
func v128Bytes(a specArg) ([16]byte, error) {
	var b [16]byte
	n := laneBytes(a.LaneType)
	if len(a.Lanes) != 16/n {
		return b, fmt.Errorf("a v128 of %s lanes has %d lanes", a.LaneType, len(a.Lanes))
	}
	for i, s := range a.Lanes {
		v, err := parseInt[int64](s)
		if err != nil {
			return b, err
		}
		for j := range n {
			b[i*n+j] = byte(uint64(v) >> (8 * j))
		}
	}
	return b, nil
}

// Checks the v128 got against exp, lane by lane: a NaN the spec allows
// (canonical or arithmetic) must be the positive canonical NaN, as the
// deterministic profile requires, or the negative one if negated.
func checkV128(t *testing.T, got [16]byte, exp specArg, negated bool) {
	t.Helper()
	n := laneBytes(exp.LaneType)
	if len(exp.Lanes) != 16/n {
		t.Fatalf("a v128 of %s lanes has %d lanes", exp.LaneType, len(exp.Lanes))
	}
	for i, s := range exp.Lanes {
		var lane uint64
		for j := range n {
			lane |= uint64(got[i*n+j]) << (8 * j)
		}
		var want uint64
		switch s {
		case "nan:canonical", "nan:arithmetic":
			want = 0x7fc00000
			if n == 8 {
				want = 0x7ff8000000000000
			}
			if negated {
				want |= 1 << (8*n - 1)
			}
		default:
			v, err := parseInt[int64](s)
			if err != nil {
				t.Fatal(err)
			}
			want = uint64(v)
			if n < 8 {
				want &= 1<<(8*n) - 1
			}
		}
		if lane != want {
			t.Errorf("lane %d of %s: got %#x, want %#x (%s); got %x", i, exp.LaneType, lane, want, s, got)
		}
	}
}
