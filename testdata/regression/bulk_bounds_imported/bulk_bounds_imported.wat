;; Regression: bulk memory operations must trap when they reach past the
;; memory's size, for an imported memory whose slice, as the host holds it,
;; has spare capacity past its length (the helpers sliced the memory up to
;; its capacity, so memory.fill and memory.copy past the end wrote into the
;; spare capacity instead of trapping).
(module
  (import "env" "memory" (memory 1 10))
  (data $d "0123456789abcdef")
  (func (export "grow") (param i32) (result i32)
    (memory.grow (local.get 0)))
  (func (export "fill") (param $dest i32) (param $n i32)
    (memory.fill (local.get $dest) (i32.const 7) (local.get $n)))
  (func (export "zero") (param $dest i32) (param $n i32)
    (memory.fill (local.get $dest) (i32.const 0) (local.get $n)))
  (func (export "copy") (param $dest i32) (param $src i32) (param $n i32)
    (memory.copy (local.get $dest) (local.get $src) (local.get $n)))
  (func (export "init") (param $dest i32) (param $n i32)
    (memory.init $d (local.get $dest) (i32.const 0) (local.get $n)))
)
