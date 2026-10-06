;; Indirect calls through four tables: a closed one (defined, not
;; exported, never mutated), whose calls the dispatch pass rewrites; a
;; closed one whose calls reach more functions than the pass rewrites
;; (maxDispatchTargets); and two that are not closed: one exported, one
;; mutated by table.set.
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

  ;; Slots 0 to 16 hold 17 functions of one type, each adding 100 times
  ;; its slot; slot 17 is null.
  (type $i2l (func (param i32) (result i64)))
  (func $k0 (type $i2l) (i64.add (i64.extend_i32_u (local.get 0)) (i64.const 0)))
  (func $k1 (type $i2l) (i64.add (i64.extend_i32_u (local.get 0)) (i64.const 100)))
  (func $k2 (type $i2l) (i64.add (i64.extend_i32_u (local.get 0)) (i64.const 200)))
  (func $k3 (type $i2l) (i64.add (i64.extend_i32_u (local.get 0)) (i64.const 300)))
  (func $k4 (type $i2l) (i64.add (i64.extend_i32_u (local.get 0)) (i64.const 400)))
  (func $k5 (type $i2l) (i64.add (i64.extend_i32_u (local.get 0)) (i64.const 500)))
  (func $k6 (type $i2l) (i64.add (i64.extend_i32_u (local.get 0)) (i64.const 600)))
  (func $k7 (type $i2l) (i64.add (i64.extend_i32_u (local.get 0)) (i64.const 700)))
  (func $k8 (type $i2l) (i64.add (i64.extend_i32_u (local.get 0)) (i64.const 800)))
  (func $k9 (type $i2l) (i64.add (i64.extend_i32_u (local.get 0)) (i64.const 900)))
  (func $k10 (type $i2l) (i64.add (i64.extend_i32_u (local.get 0)) (i64.const 1000)))
  (func $k11 (type $i2l) (i64.add (i64.extend_i32_u (local.get 0)) (i64.const 1100)))
  (func $k12 (type $i2l) (i64.add (i64.extend_i32_u (local.get 0)) (i64.const 1200)))
  (func $k13 (type $i2l) (i64.add (i64.extend_i32_u (local.get 0)) (i64.const 1300)))
  (func $k14 (type $i2l) (i64.add (i64.extend_i32_u (local.get 0)) (i64.const 1400)))
  (func $k15 (type $i2l) (i64.add (i64.extend_i32_u (local.get 0)) (i64.const 1500)))
  (func $k16 (type $i2l) (i64.add (i64.extend_i32_u (local.get 0)) (i64.const 1600)))
  (table $many 18 funcref)
  (elem (table $many) (i32.const 0) func $k0 $k1 $k2 $k3 $k4 $k5 $k6 $k7 $k8 $k9 $k10 $k11 $k12 $k13 $k14 $k15 $k16)
  (func (export "callMany") (param $slot i32) (param $x i32) (result i64)
    (call_indirect $many (type $i2l) (local.get $x) (local.get $slot)))

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
