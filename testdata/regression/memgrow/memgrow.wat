;; Calls that can grow memory, each followed by an access to the page the
;; call added (the call returns the old size, in pages): a function that
;; caches the memory must reload it after each of them. The calls grow
;; memory directly, through a closed table, through an exported table the
;; host changes, through the host, and through a provided function; a
;; provided function that only reads memory needs no reload.
(module
  (type $v2i (func (result i32)))
  (import "env" "grow" (func $hostgrow (result i32)))
  (import "env" "pgrow" (func $pgrow (result i32)))
  (import "env" "ppeek" (func $ppeek (param i32) (result i32)))
  (memory (export "memory") 1)

  (func $grow (type $v2i) (memory.grow (i32.const 1)))
  (func $nogrow (type $v2i) (i32.const 0))
  (func $growIndirectly (type $v2i) (call $grow))

  (table $closed 3 funcref)
  (elem (table $closed) (i32.const 0) func $grow $nogrow $growIndirectly)
  (table $open 1 funcref)
  (elem (table $open) (i32.const 0) func $nogrow)
  (export "table" (table $open))

  (func (export "direct") (result i32) (local $old i32)
    (i32.store (i32.const 0) (i32.const 1))
    (local.set $old (call $growIndirectly))
    (i32.store (i32.mul (local.get $old) (i32.const 65536)) (i32.const 99))
    (i32.load (i32.mul (local.get $old) (i32.const 65536))))

  (func (export "closed") (param $slot i32) (result i32) (local $old i32)
    (i32.store (i32.const 0) (i32.const 1))
    (local.set $old (call_indirect $closed (type $v2i) (local.get $slot)))
    (i32.store (i32.mul (local.get $old) (i32.const 65536)) (i32.const 99))
    (i32.load (i32.mul (local.get $old) (i32.const 65536))))

  (func (export "open") (result i32) (local $old i32)
    (i32.store (i32.const 0) (i32.const 1))
    (local.set $old (call_indirect $open (type $v2i) (i32.const 0)))
    (i32.store (i32.mul (local.get $old) (i32.const 65536)) (i32.const 99))
    (i32.load (i32.mul (local.get $old) (i32.const 65536))))

  (func (export "host") (result i32) (local $old i32)
    (i32.store (i32.const 0) (i32.const 1))
    (local.set $old (call $hostgrow))
    (i32.store (i32.mul (local.get $old) (i32.const 65536)) (i32.const 99))
    (i32.load (i32.mul (local.get $old) (i32.const 65536))))

  (func (export "provided") (result i32) (local $old i32)
    (i32.store (i32.const 0) (i32.const 1))
    (local.set $old (call $pgrow))
    (i32.store (i32.mul (local.get $old) (i32.const 65536)) (i32.const 99))
    (i32.load (i32.mul (local.get $old) (i32.const 65536))))

  (func (export "peek") (param $addr i32) (result i32)
    (i32.store (i32.const 0) (i32.const 1))
    (i32.add (call $ppeek (local.get $addr)) (i32.load (i32.const 0))))
)
