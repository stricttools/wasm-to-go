package passes

import (
	"go/ast"
	"reflect"
)

// Clone returns a deep copy of n: every node reachable from n is copied,
// and a node reachable twice (the translator's trees share nodes) is
// copied twice, so no node of the copy is shared, with n or within it,
// except identifiers and basic literals. Those are leaves the translator
// shares by the thousand (every use of a local variable is one *ast.Ident,
// newID's), and no pass changes a leaf in place once a function is
// cleaned up: a pass replaces a leaf in its parent, which is copied.
// Copying them made a copy of QuickJS's code a third larger than the code.
// Positions and comments are copied as they are.
func Clone[N ast.Node](n N) N {
	return clone(reflect.ValueOf(n)).Interface().(N)
}

func clone(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		// The parser's objects and scopes link declarations to their uses,
		// in cycles; the copy goes without them (as the translator's own
		// trees do).
		if t := v.Type(); t == objectType || t == scopeType {
			return reflect.Zero(t)
		} else if t == identType || t == basicLitType {
			return v
		}
		c := reflect.New(v.Type().Elem())
		c.Elem().Set(clone(v.Elem()))
		return c
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		c := reflect.New(v.Type()).Elem()
		c.Set(clone(v.Elem()))
		return c
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		c := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := range v.Len() {
			c.Index(i).Set(clone(v.Index(i)))
		}
		return c
	case reflect.Struct:
		c := reflect.New(v.Type()).Elem()
		for i := range v.NumField() {
			if f := c.Field(i); f.CanSet() {
				f.Set(clone(v.Field(i)))
			}
		}
		return c
	}
	return v
}

var (
	objectType = reflect.TypeFor[*ast.Object]()
	scopeType  = reflect.TypeFor[*ast.Scope]()

	identType    = reflect.TypeFor[*ast.Ident]()
	basicLitType = reflect.TypeFor[*ast.BasicLit]()
)
