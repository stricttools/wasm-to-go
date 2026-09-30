;; Recursion the Go stack bound must stop with a trap, and recursion it must
;; let through. None of these functions touch the linear memory, so no shadow
;; stack limits them: only the translated code's own bound does.
(module
  (type $i2i (func (param i32) (result i32)))

  ;; Unbounded self recursion; the result is used, so the call is not in tail
  ;; position.
  (func $down (export "down") (param $n i32) (result i32)
    (i32.add (call $down (i32.add (local.get $n) (i32.const 1))) (i32.const 1)))

  ;; Recursion to depth $n, returning $n: the bound must give back what each
  ;; call took, or repeated calls would add up to a trap.
  (func $depth (export "depth") (param $n i32) (result i32)
    (if (result i32) (i32.eqz (local.get $n))
      (then (i32.const 0))
      (else (i32.add (call $depth (i32.sub (local.get $n) (i32.const 1))) (i32.const 1)))))

  ;; The same, with no result: the frame is given back at the end of the body.
  (global $count (mut i32) (i32.const 0))
  (func $walk (param $n i32)
    (global.set $count (i32.add (global.get $count) (i32.const 1)))
    (if (i32.eqz (local.get $n)) (then (return)))
    (call $walk (i32.sub (local.get $n) (i32.const 1))))
  (func (export "walk") (param $n i32) (result i32)
    (global.set $count (i32.const 0))
    (call $walk (local.get $n))
    (global.get $count))

  ;; Mutual recursion through a closed table: $ping calls $pong indirectly,
  ;; $pong calls $ping directly.
  (table 2 funcref)
  (elem (i32.const 0) func $ping $pong)
  (func $ping (type $i2i)
    (i32.add (call_indirect (type $i2i) (local.get 0) (i32.const 1)) (i32.const 1)))
  (func $pong (type $i2i)
    (call $ping (local.get 0)))
  (func (export "pingpong") (param i32) (result i32)
    (call $ping (local.get 0)))

  ;; Recursion that never uses its receiver (no globals, no memory, no
  ;; table): the translator keeps the Module for the bound.
  (func $pure (param $n i32) (result i32)
    (i32.mul (call $pure (i32.add (local.get $n) (i32.const 1))) (i32.const 3)))
  (func (export "pure") (param i32) (result i32)
    (call $pure (local.get 0)))
)
