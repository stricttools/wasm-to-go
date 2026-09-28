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
