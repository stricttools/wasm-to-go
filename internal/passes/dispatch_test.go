package passes

import (
	"go/ast"
	"go/format"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

// A site function for tests: every indirect call of type func(int32) int32
// dispatches to m.a in slots 1 and 3, and to b in slot 2; indirect calls of
// type func() have one case, m.c in slot 4; any other call has none.
func testSites(call *ast.CallExpr) *DispatchSite {
	assert, ok := call.Fun.(*ast.TypeAssertExpr)
	if !ok {
		return nil
	}
	switch formatExpr(assert.Type) {
	case "func(int32) int32":
		return &DispatchSite{
			Cases: []DispatchCase{
				{Slots: []uint64{1, 3}, Fun: mustParseExpr("m.a")},
				{Slots: []uint64{2}, Fun: mustParseExpr("b")},
			},
			Results: []ast.Expr{ast.NewIdent("int32")},
		}
	case "func()":
		return &DispatchSite{
			Cases: []DispatchCase{{Slots: []uint64{4}, Fun: mustParseExpr("m.c")}},
		}
	case "func(int32) (int32, int64)":
		return &DispatchSite{
			Cases:   []DispatchCase{{Slots: []uint64{5}, Fun: mustParseExpr("m.d")}},
			Results: []ast.Expr{ast.NewIdent("int32"), ast.NewIdent("int64")},
		}
	}
	return &DispatchSite{}
}

func testTemps() func() *ast.Ident {
	n := 90
	return func() *ast.Ident {
		n++
		return ast.NewIdent("t" + strconv.Itoa(n))
	}
}

func TestDispatch(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "define",
			src: `func (m *Module) f(v0, v1 int32) int32 {
				t1 := m.t0[uint(v0)].(func(int32) int32)(v1 + 1)
				return t1
			}`,
			want: `func (m *Module) f(v0, v1 int32) int32 {
				var t1 int32
				switch t91 := uint(v0); t91 {
				case 1, 3:
					t1 = m.a(v1 + 1)
				case 2:
					t1 = b(v1 + 1)
				default:
					t1 = m.t0[t91].(func(int32) int32)(v1 + 1)
				}
				return t1
			}`,
		},
		{
			name: "assign, blank",
			src: `func (m *Module) f(v0, v1 int32) {
				_ = m.t0[uint(v0)].(func(int32) int32)(v1)
			}`,
			want: `func (m *Module) f(v0, v1 int32) {
				switch t91 := uint(v0); t91 {
				case 1, 3:
					_ = m.a(v1)
				case 2:
					_ = b(v1)
				default:
					_ = m.t0[t91].(func(int32) int32)(v1)
				}
			}`,
		},
		{
			name: "multiple results",
			src: `func (m *Module) f(v0 int32) int64 {
				t1, _ := m.t0[uint(v0)].(func(int32) (int32, int64))(v0)
				return int64(t1)
			}`,
			want: `func (m *Module) f(v0 int32) int64 {
				var t1 int32
				switch t91 := uint(v0); t91 {
				case 5:
					t1, _ = m.d(v0)
				default:
					t1, _ = m.t0[t91].(func(int32) (int32, int64))(v0)
				}
				return int64(t1)
			}`,
		},
		{
			name: "statement and return",
			src: `func (m *Module) f(v0 int32) int32 {
				m.t0[uint(i32(4))].(func())()
				return m.t0[uint(v0)].(func(int32) int32)(v0)
			}`,
			want: `func (m *Module) f(v0 int32) int32 {
				switch t91 := uint(i32(4)); t91 {
				case 4:
					m.c()
				default:
					m.t0[t91].(func())()
				}
				switch t92 := uint(v0); t92 {
				case 1, 3:
					return m.a(v0)
				case 2:
					return b(v0)
				default:
					return m.t0[t92].(func(int32) int32)(v0)
				}
			}`,
		},
		{
			name: "labeled, nested",
			src: `func (m *Module) f(v0 int32) int32 {
			l0:
				if v0 != 0 {
					t1 := m.t0[uint(v0)].(func(int32) int32)(v0)
					v0 = t1
					goto l0
				}
			l1:
				m.t0[uint(v0)].(func())()
				goto l1
			}`,
			want: `func (m *Module) f(v0 int32) int32 {
			l0:
				if v0 != 0 {
					var t1 int32
					switch t91 := uint(v0); t91 {
					case 1, 3:
						t1 = m.a(v0)
					case 2:
						t1 = b(v0)
					default:
						t1 = m.t0[t91].(func(int32) int32)(v0)
					}
					v0 = t1
					goto l0
				}
			l1:
				switch t92 := uint(v0); t92 {
				case 4:
					m.c()
				default:
					m.t0[t92].(func())()
				}
				goto l1
			}`,
		},
		{
			name: "labeled define",
			src: `func (m *Module) f(v0 int32) int32 {
			l0:
				t1 := m.t0[uint(v0)].(func(int32) int32)(v0)
				return t1
			}`,
			want: `func (m *Module) f(v0 int32) int32 {
			l0:
				var t1 int32
				switch t91 := uint(v0); t91 {
				case 1, 3:
					t1 = m.a(v0)
				case 2:
					t1 = b(v0)
				default:
					t1 = m.t0[t91].(func(int32) int32)(v0)
				}
				return t1
			}`,
		},
		{
			name: "index or argument may trap, or reads memory",
			src: `func (m *Module) f(v0, v1 int32) {
				_ = m.t0[uint(v0/v1)].(func(int32) int32)(v1)
				_ = m.t0[uint(v0)].(func(int32) int32)(v0 % v1)
				_ = m.t0[uint(v0)].(func(int32) int32)(v0 << v1)
				_ = m.t0[uint(v0)].(func(int32) int32)(m.g0)
				_ = m.t0[uint(v0)].(func(int32) int32)(int32(load32(m.memory, uint32(v0))))
			}`,
		},
		{
			name: "nested in an expression",
			src: `func (m *Module) f(v0 int32) int32 {
				return 1 + m.t0[uint(v0)].(func(int32) int32)(v0)
			}`,
		},
		{
			name: "no candidates",
			src: `func (m *Module) f(v0 int32) float64 {
				return m.t0[uint(v0)].(func(int32) float64)(v0)
			}`,
		},
		{
			name: "not an indirect call",
			src: `func (m *Module) f(v0 int32) int32 {
				return m.g(v0)
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn := parseFunc(t, tt.src)
			want := tt.want
			if want == "" {
				want = tt.src
			}
			Dispatch(fn, testSites, testTemps())
			if got, want := formatFunc(t, fn), normalizeFunc(t, want); got != want {
				t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
			}
		})
	}
}

func TestDispatch_sharedCall(t *testing.T) {
	// The same call node reached twice is left alone:
	// the default branch reuses the original node.
	fn := parseFunc(t, `func (m *Module) f(v0 int32) {
		m.t0[uint(v0)].(func())()
		if v0 != 0 {
			return
		}
	}`)
	shared := fn.Body.List[0]
	fn.Body.List[1].(*ast.IfStmt).Body.List = []ast.Stmt{shared}
	want := formatFunc(t, fn)
	if n := Dispatch(fn, testSites, testTemps()); n != 0 {
		t.Errorf("rewrote %d calls", n)
	}
	if got := formatFunc(t, fn); got != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
	}
}

func TestTrapFree(t *testing.T) {
	tests := map[string]bool{
		"v0":                                    true,
		"i32(4)":                                true,
		"uint(v0)":                              true,
		"float64(v0) + 1.5":                     true,
		"f64_canon(float64(v0*v1))":             true,
		"f32_neg(f32_abs(v0))":                  true,
		"f64_canon(v0 / v1)":                    false,
		"-v0 ^ v1&7 | v2<<3":                    true,
		"!(v0 == v1)":                           true,
		"v0 / v1":                               false,
		"v0 % 3":                                false,
		"v0 << v1":                              false,
		"m.g0":                                  false,
		"*m.g0":                                 false,
		"f(v0)":                                 false,
		"load32(m.memory, 0)":                   false,
		"(*m.memory)[v0]":                       false,
		"i32_div_s(v0, v1)":                     false,
		"math.Float64frombits(v0)":              true,
		"math.Float32frombits(0x3f800000) + v0": true,
		"math.Sqrt(v0)":                         false,
		"m.Float64frombits(v0)":                 false,
	}
	for src, want := range tests {
		if got := TrapFree(mustParseExpr(src)); got != want {
			t.Errorf("TrapFree(%s) = %v, want %v", src, got, want)
		}
	}
}

func formatExpr(e ast.Expr) string {
	var sb strings.Builder
	if err := format.Node(&sb, token.NewFileSet(), e); err != nil {
		panic(err)
	}
	return sb.String()
}
