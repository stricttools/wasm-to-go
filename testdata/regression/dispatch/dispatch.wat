;; Indirect calls through three tables: a closed one (defined, not
;; exported, never mutated), whose calls the dispatch pass rewrites, and two
;; that are not closed: one exported, one mutated by table.set.
(module
  (type $i2i (func (param i32) (result i32)))
  (type $v2i (func (result i32)))

  (func $double (type $i2i) (i32.add (local.get 0) (local.get 0)))
  (func $triple (type $i2i) (i32.mul (local.get 0) (i32.const 3)))
  (func $answer (type $v2i) (i32.const 42))

  ;; Slot 0 is null, slots 1 and 4 hold $double, slot 2 $triple,
  ;; slot 3 $answer (another type), slots 5 to 7 are null.
  (table $closed 8 funcref)
  (elem (table $closed) (i32.const 1) func $double $triple $answer $double)

  (table $exported 4 funcref)
  (elem (table $exported) (i32.const 1) func $double)
  (export "exported" (table $exported))

  (table $mutated 4 funcref)
  (elem (table $mutated) (i32.const 1) func $double)

  (func (export "call") (param $slot i32) (param $x i32) (result i32)
    (call_indirect $closed (type $i2i) (local.get $x) (local.get $slot)))
  (func (export "call0") (param $slot i32) (result i32)
    (call_indirect $closed (type $v2i) (local.get $slot)))

  (func (export "callExported") (param $slot i32) (param $x i32) (result i32)
    (call_indirect $exported (type $i2i) (local.get $x) (local.get $slot)))

  (func (export "callMutated") (param $slot i32) (param $x i32) (result i32)
    (call_indirect $mutated (type $i2i) (local.get $x) (local.get $slot)))
  (func (export "setMutated") (param $slot i32)
    (table.set $mutated (local.get $slot) (ref.func $triple)))
)
