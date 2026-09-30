;; The stack-weight pass's module: recursion in each form the pass rewrites.
;; stack_weight.unweighted.wasm is this file assembled; stack_weight.wasm is
;; the pass's output for it (translate_test.go writes it), which the
;; translator's tests translate and run.
(module
  (type $i2i (func (param i32) (result i32)))
  (type $i2ii (func (param i32) (result i32 i32)))
  (memory (export "memory") 1)
  (global $sp (export "__stack_pointer") (mut i32) (i32.const 65536))
  ;; How many levels the last recursion entered.
  (global $count (export "count") (mut i32) (i32.const 0))
  (table 2 funcref)
  (elem (i32.const 0) $viaTable $leaf)

  ;; Calls nothing: not on a cycle, not charged.
  (func $leaf (param i32) (result i32) (local.get 0))
  (func $sp (export "sp") (result i32) (global.get $sp))

  ;; Calls, but on no cycle: not charged.
  (func (export "callsLeaf") (param i32) (result i32)
    (drop (call $leaf (local.get 0)))
    (global.get $sp))

  ;; Recursion to depth $n that returns, at the bottom, the stack pointer
  ;; there: from a return inside a block at the other levels.
  (func $down (export "down") (param $n i32) (result i32)
    (block
      (br_if 0 (i32.eqz (local.get $n)))
      (return (call $down (i32.sub (local.get $n) (i32.const 1)))))
    (global.get $sp))

  ;; The same through a table.
  (func $viaTable (export "viaTable") (param $n i32) (result i32)
    (if (result i32) (i32.eqz (local.get $n))
      (then (global.get $sp))
      (else (call_indirect (type $i2i) (i32.sub (local.get $n) (i32.const 1)) (i32.const 0)))))

  ;; Two results, and a branch to the function's own label from a br_table.
  (func $pair (export "pair") (param $n i32) (result i32 i32)
    (local $a i32) (local $b i32)
    (if (i32.eqz (local.get $n))
      (then (return (global.get $sp) (local.get $n))))
    (call $pair (i32.sub (local.get $n) (i32.const 1)))
    (local.set $b)
    (local.set $a)
    (local.get $a)
    (i32.add (local.get $b) (i32.const 1))
    (br_table 0 0 (local.get $n)))

  ;; No result; ends by falling off its end.
  (func $walk (param $n i32)
    (global.set $count (i32.add (global.get $count) (i32.const 1)))
    (if (i32.eqz (local.get $n)) (then (return)))
    (call $walk (i32.sub (local.get $n) (i32.const 1))))
  (func (export "walk") (param $n i32) (result i32)
    (global.set $count (i32.const 0))
    (call $walk (local.get $n))
    (global.get $sp))

  ;; Recursion without end, touching no memory: only the pass's own bound
  ;; stops it.
  (func $forever (param $n i32) (result i32)
    (global.set $count (i32.add (global.get $count) (i32.const 1)))
    (i32.add (call $forever (local.get $n)) (i32.const 1)))
  (func (export "forever") (result i32)
    (global.set $count (i32.const 0))
    (call $forever (i32.const 0)))
)
