;; Locals reused for unrelated values, which the translator gives a
;; variable per web (splitLocals), and locals whose values meet at joins,
;; which keep one variable.
(module
  ;; $t holds two unrelated sums, one after the other: two webs.
  (func (export "twoSums") (param $a i32) (param $b i32) (result i32)
    (local $t i32)
    (local.set $t (i32.add (local.get $a) (i32.const 1)))
    (local.set $a (i32.mul (local.get $t) (i32.const 3)))
    (local.set $t (i32.add (local.get $b) (i32.const 2)))
    (i32.add (local.get $a) (i32.mul (local.get $t) (i32.const 5))))

  ;; $x carries a value around the loop, and joins its entry value:
  ;; one web. $tmp is defined before every read in the body: a web of
  ;; its own, without the entry value, and another after the loop.
  (func (export "loop") (param $n i32) (result i32)
    (local $x i32) (local $tmp i32)
    (loop $l
      (local.set $tmp (i32.mul (local.get $n) (local.get $n)))
      (local.set $x (i32.add (local.get $x) (local.get $tmp)))
      (local.set $n (i32.sub (local.get $n) (i32.const 1)))
      (br_if $l (i32.gt_s (local.get $n) (i32.const 0))))
    (local.set $tmp (i32.const 1000))
    (i32.add (local.get $x) (local.get $tmp)))

  ;; A parameter redefined on one arm of an if: its entry value and the
  ;; definition meet after the if, one web. $f is reused across the arms
  ;; for unrelated floats: a web per arm, each read in its arm.
  (func (export "branch") (param $c i32) (param $v f64) (result f64)
    (local $f f64)
    (if (local.get $c)
      (then
        (local.set $f (f64.mul (local.get $v) (f64.const 2)))
        (local.set $v (f64.add (local.get $f) (f64.const 1))))
      (else
        (local.set $f (f64.sub (local.get $v) (f64.const 3)))
        (drop (local.get $f))))
    (local.get $v))
)
