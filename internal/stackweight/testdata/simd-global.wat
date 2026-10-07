;; A module with a v128 global initialized by v128.const: the pass skips
;; the constant expression and weighs the module.
(module
  (global $sp (export "__stack_pointer") (mut i32) (i32.const 4096))
  (global $g (export "g") (mut v128) (v128.const i32x4 1 2 3 4))
  (memory 1)
  (func $f (export "f") (param $n i32) (result v128)
    (if (result v128) (local.get $n)
      (then (call $f (i32.sub (local.get $n) (i32.const 1))))
      (else (global.get $g)))))
