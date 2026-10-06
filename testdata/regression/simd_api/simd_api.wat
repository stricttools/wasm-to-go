;; v128 values where the translation meets its host and across its own
;; constructs: exported and imported functions and globals of v128, an
;; indirect call of a v128 signature through a closed table, select,
;; block parameters and results, a loop carrying a vector, and memory.
(module
  (import "env" "double" (func $double (param v128) (result v128)))
  (import "env" "g" (global $imported (mut v128)))
  (global $g (export "g") (mut v128) (v128.const i32x4 1 2 3 4))
  (memory (export "memory") 1)
  (type $vv (func (param v128) (result v128)))
  (func $inc (type $vv) (i32x4.add (local.get 0) (v128.const i32x4 1 1 1 1)))
  (func $neg (type $vv) (i32x4.neg (local.get 0)))
  (table 2 funcref)
  (elem (i32.const 0) func $inc $neg)

  ;; The sum of v and the global, through the host's double.
  (func (export "add") (param $v v128) (result v128)
    (call $double (i32x4.add (local.get $v) (global.get $g))))
  ;; Sets the global and the imported global to v.
  (func (export "set") (param $v v128)
    (global.set $g (local.get $v))
    (global.set $imported (local.get $v)))
  ;; The imported global's lane 2.
  (func (export "importedLane") (result i32)
    (i32x4.extract_lane 2 (global.get $imported)))
  ;; v through table slot i.
  (func (export "dispatch") (param $i i32) (param $v v128) (result v128)
    (call_indirect (type $vv) (local.get $v) (local.get $i)))
  ;; a if c, else b; through a block with a vector parameter and result.
  (func (export "choose") (param $c i32) (param $a v128) (param $b v128) (result v128)
    (local.get $a)
    (block (param v128) (result v128)
      (select (local.get $b) (local.get $c))
      (i32x4.add (v128.const i32x4 0 0 0 0))))
  ;; The sum of the i32 lanes of n vectors of memory from address 0.
  (func (export "sum") (param $n i32) (result i32)
    (local $acc v128) (local $p i32)
    (block $done
      (loop $l
        (br_if $done (i32.eqz (local.get $n)))
        (local.set $acc (i32x4.add (local.get $acc) (v128.load (local.get $p))))
        (local.set $p (i32.add (local.get $p) (i32.const 16)))
        (local.set $n (i32.sub (local.get $n) (i32.const 1)))
        (br $l)))
    (i32.add
      (i32.add (i32x4.extract_lane 0 (local.get $acc)) (i32x4.extract_lane 1 (local.get $acc)))
      (i32.add (i32x4.extract_lane 2 (local.get $acc)) (i32x4.extract_lane 3 (local.get $acc)))))
)
