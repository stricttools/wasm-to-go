;; Bounds checks an earlier check covers (removed with -unsafe), and checks
;; nothing earlier covers: after the address changes, in a loop, or only
;; conditionally.
(module
  (memory (export "memory") 1)

  ;; The first check (p+8+4) covers the other two.
  (func (export "sum") (param $p i32) (result i32)
    (i32.add
      (i32.add (i32.load offset=8 (local.get $p)) (i32.load offset=4 (local.get $p)))
      (i32.load (local.get $p))))

  ;; The store is covered by the load before it.
  (func (export "inc") (param $p i32)
    (i32.store (local.get $p) (i32.add (i32.load (local.get $p)) (i32.const 1))))

  ;; A 2-byte check does not cover an 8-byte access.
  (func (export "widen") (param $p i32) (result i64)
    (drop (i32.load16_u (local.get $p)))
    (i64.load (local.get $p)))

  ;; The address changes on every iteration: nothing is covered.
  (func (export "walk") (param $p i32) (param $n i32) (result i32) (local $s i32)
    (loop $l
      (local.set $s (i32.add (local.get $s) (i32.load (local.get $p))))
      (local.set $p (i32.add (local.get $p) (i32.const 4)))
      (br_if $l (local.tee $n (i32.sub (local.get $n) (i32.const 1)))))
    (local.get $s))

  ;; A check made only on one branch covers nothing after the if.
  (func (export "branch") (param $p i32) (param $c i32) (result i32)
    (if (local.get $c) (then (drop (i32.load offset=4 (local.get $p)))))
    (i32.load offset=4 (local.get $p)))

  ;; A copy of the address is the same address.
  (func (export "copy") (param $p i32) (result i32) (local $q i32)
    (drop (i32.load offset=4 (local.get $p)))
    (local.set $q (local.get $p))
    (i32.load (local.get $q)))
)
