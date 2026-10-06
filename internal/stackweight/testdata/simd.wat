;; A recursive function with SIMD instructions, a v128 parameter, and v128
;; locals: weighed, with each v128 taking two slots of the estimate.
(module
  (global $sp (export "__stack_pointer") (mut i32) (i32.const 4096))
  (memory 1)
  (func $f (export "f") (param $v v128) (param $n i32) (result v128)
    (local $a v128) (local $b v128) (local $i i32)
    (local.set $a (f32x4.add (local.get $v) (v128.load offset=16 (local.get $n))))
    (local.set $b (i8x16.shuffle 0 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 (local.get $a) (local.get $v)))
    (v128.store (i32.const 32) (local.get $b))
    (if (result v128) (local.get $n)
      (then (call $f (local.get $b) (i32.sub (local.get $n) (i32.const 1))))
      (else (local.get $a)))))
