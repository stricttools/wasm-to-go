;; A memory that grows one page at a time, filling each new page and
;; writing its index into its first word: UseMemory must keep the contents and the
;; results of memory.grow, and growth within the host's backing array must
;; not move the memory.
(module
  (memory (export "memory") 1 2048)
  (func (export "grow_to") (param $pages i32) (result i32) (local $old i32)
    (block $done
      (loop $more
        (br_if $done (i32.ge_u (memory.size) (local.get $pages)))
        (local.set $old (memory.grow (i32.const 1)))
        (br_if $done (i32.eq (local.get $old) (i32.const -1)))
        (memory.fill (i32.shl (local.get $old) (i32.const 16)) (i32.const 0xab) (i32.const 65536))
        (i32.store (i32.shl (local.get $old) (i32.const 16)) (local.get $old))
        (br $more)))
    (memory.size))
  (func (export "grow") (param i32) (result i32)
    (memory.grow (local.get 0)))
  ;; The sum of every page's first word.
  (func (export "sum") (result i32) (local $p i32) (local $s i32)
    (block $done
      (loop $more
        (br_if $done (i32.ge_u (local.get $p) (memory.size)))
        (local.set $s (i32.add (local.get $s) (i32.load (i32.shl (local.get $p) (i32.const 16)))))
        (local.set $p (i32.add (local.get $p) (i32.const 1)))
        (br $more)))
    (local.get $s))
)
