;; tail-call.wat built without tail calls: a call, then a return.
(module
  (global $sp (export "__stack_pointer") (mut i32) (i32.const 4096))
  (func $f (export "f") (param i32) (result i32) (return (call $f (local.get 0)))))
