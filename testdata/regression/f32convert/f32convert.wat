;; 64-bit integers converted to float32 round once, to nearest even.
(module
  (func (export "s") (param i64) (result i32)
    (i32.reinterpret_f32 (f32.convert_i64_s (local.get 0))))
  (func (export "u") (param i64) (result i32)
    (i32.reinterpret_f32 (f32.convert_i64_u (local.get 0))))
)
