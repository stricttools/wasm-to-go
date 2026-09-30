package passes

import (
	"go/ast"
	"reflect"
)

// Clone returns a deep copy of n: every node reachable from n is copied,
// once, so the copy shares nothing with n (nodes n shares stay shared in
// the copy). Positions and comments are copied as they are.
func Clone[N ast.Node](n N) N {
	seen := map[any]reflect.Value{}
	return clone(reflect.ValueOf(n), seen).Interface().(N)
}

func clone(v reflect.Value, seen map[any]reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		if c, ok := seen[v.Interface()]; ok {
			return c
		}
		c := reflect.New(v.Type().Elem())
		seen[v.Interface()] = c
		c.Elem().Set(clone(v.Elem(), seen))
		return c
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		c := reflect.New(v.Type()).Elem()
		c.Set(clone(v.Elem(), seen))
		return c
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		c := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := range v.Len() {
			c.Index(i).Set(clone(v.Index(i), seen))
		}
		return c
	case reflect.Struct:
		c := reflect.New(v.Type()).Elem()
		for i := range v.NumField() {
			if f := c.Field(i); f.CanSet() {
				f.Set(clone(v.Field(i), seen))
			}
		}
		return c
	case reflect.Map:
		// ast.Scope and ast.Object, which the translator's trees do not use.
		return v
	}
	return v
}
