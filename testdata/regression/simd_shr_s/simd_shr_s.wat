;; The arithmetic right shifts of each lane width by a shift count the code
;; does not know, so they are shifts by a variable in the translation.
(module
  (func (export "i8x16") (param $v v128) (param $s i32) (result v128)
    (i8x16.shr_s (local.get $v) (local.get $s)))
  (func (export "i16x8") (param $v v128) (param $s i32) (result v128)
    (i16x8.shr_s (local.get $v) (local.get $s)))
  (func (export "i32x4") (param $v v128) (param $s i32) (result v128)
    (i32x4.shr_s (local.get $v) (local.get $s)))
  (func (export "i64x2") (param $v v128) (param $s i32) (result v128)
    (i64x2.shr_s (local.get $v) (local.get $s)))
)
