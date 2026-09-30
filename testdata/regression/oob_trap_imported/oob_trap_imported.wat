;; Regression: linear-memory accesses must trap at the memory boundary of an
;; imported memory whose slice, as the host holds it, has spare capacity past
;; its length, before and after memory.grow: slicing a Go slice checks its
;; bounds against the capacity, so the translation cuts such a memory to its
;; length before slicing an access out of it.
(module
  (import "env" "memory" (memory 1 10))
  (func (export "ld16") (param i32) (result i32)
    local.get 0 i32.load16_u)
  (func (export "ld32") (param i32) (result i32)
    local.get 0 i32.load)
  (func (export "ld64") (param i32) (result i64)
    local.get 0 i64.load)
  (func (export "ld32o") (param i32) (result i32)
    local.get 0 i32.load offset=0xffffffff)
  (func (export "st32") (param i32 i32)
    local.get 0 local.get 1 i32.store)
  (func (export "st64") (param i32 i64)
    local.get 0 local.get 1 i64.store)
  (func (export "grow") (param i32) (result i32)
    local.get 0 memory.grow)
)
