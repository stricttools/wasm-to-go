;; Recursion over a C stack whose pointer the module does not export.
(module
  (global $sp (mut i32) (i32.const 4096))
  (func $f (export "f") (param i32) (result i32) (call $f (local.get 0))))
