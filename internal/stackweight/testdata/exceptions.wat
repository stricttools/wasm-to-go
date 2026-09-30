;; Recursion inside an exception handler.
(module
  (tag $e)
  (global $sp (export "__stack_pointer") (mut i32) (i32.const 4096))
  (func $f (export "f") (param i32) (result i32)
    (try (result i32)
      (do (call $f (local.get 0)))
      (catch $e (i32.const 0)))))
