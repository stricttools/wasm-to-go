package passes

import (
	"go/ast"
	"reflect"
)

// Clone returns a deep copy of n: every node reachable from n is copied,
// and a node reachable twice (the translator's trees share nodes) is
// copied twice, so no node of the copy is shared, with n or within it.
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
)
