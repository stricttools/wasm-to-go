;; Every float instruction, and a few chains of them, as exports opN taking two
;; operands and returning a result as raw bits (see cases.go, which lists the
;; operations in order with their operands).
(module
  ;; f64.nearest
  (func (export "op0") (param i64 i64) (result i64) (i64.reinterpret_f64 (f64.nearest (f64.reinterpret_i64 (local.get 0)))))
  ;; f64.floor
  (func (export "op1") (param i64 i64) (result i64) (i64.reinterpret_f64 (f64.floor (f64.reinterpret_i64 (local.get 0)))))
  ;; f64.ceil
  (func (export "op2") (param i64 i64) (result i64) (i64.reinterpret_f64 (f64.ceil (f64.reinterpret_i64 (local.get 0)))))
  ;; f64.trunc
  (func (export "op3") (param i64 i64) (result i64) (i64.reinterpret_f64 (f64.trunc (f64.reinterpret_i64 (local.get 0)))))
  ;; f64.sqrt
  (func (export "op4") (param i64 i64) (result i64) (i64.reinterpret_f64 (f64.sqrt (f64.reinterpret_i64 (local.get 0)))))
  ;; f64.abs
  (func (export "op5") (param i64 i64) (result i64) (i64.reinterpret_f64 (f64.abs (f64.reinterpret_i64 (local.get 0)))))
  ;; f64.neg
  (func (export "op6") (param i64 i64) (result i64) (i64.reinterpret_f64 (f64.neg (f64.reinterpret_i64 (local.get 0)))))
  ;; f32.nearest
  (func (export "op7") (param i64 i64) (result i64) (i64.extend_i32_u (i32.reinterpret_f32 (f32.nearest (f32.reinterpret_i32 (i32.wrap_i64 (local.get 0)))))))
  ;; f32.floor
  (func (export "op8") (param i64 i64) (result i64) (i64.extend_i32_u (i32.reinterpret_f32 (f32.floor (f32.reinterpret_i32 (i32.wrap_i64 (local.get 0)))))))
  ;; f32.ceil
  (func (export "op9") (param i64 i64) (result i64) (i64.extend_i32_u (i32.reinterpret_f32 (f32.ceil (f32.reinterpret_i32 (i32.wrap_i64 (local.get 0)))))))
  ;; f32.trunc
  (func (export "op10") (param i64 i64) (result i64) (i64.extend_i32_u (i32.reinterpret_f32 (f32.trunc (f32.reinterpret_i32 (i32.wrap_i64 (local.get 0)))))))
  ;; f32.sqrt
  (func (export "op11") (param i64 i64) (result i64) (i64.extend_i32_u (i32.reinterpret_f32 (f32.sqrt (f32.reinterpret_i32 (i32.wrap_i64 (local.get 0)))))))
  ;; f32.abs
  (func (export "op12") (param i64 i64) (result i64) (i64.extend_i32_u (i32.reinterpret_f32 (f32.abs (f32.reinterpret_i32 (i32.wrap_i64 (local.get 0)))))))
  ;; f32.neg
  (func (export "op13") (param i64 i64) (result i64) (i64.extend_i32_u (i32.reinterpret_f32 (f32.neg (f32.reinterpret_i32 (i32.wrap_i64 (local.get 0)))))))
  ;; f32.demote_f64
  (func (export "op14") (param i64 i64) (result i64) (i64.extend_i32_u (i32.reinterpret_f32 (f32.demote_f64 (f64.reinterpret_i64 (local.get 0))))))
  ;; f64.promote_f32
  (func (export "op15") (param i64 i64) (result i64) (i64.reinterpret_f64 (f64.promote_f32 (f32.reinterpret_i32 (i32.wrap_i64 (local.get 0))))))
  ;; f64.add
  (func (export "op16") (param i64 i64) (result i64) (i64.reinterpret_f64 (f64.add (f64.reinterpret_i64 (local.get 0)) (f64.reinterpret_i64 (local.get 1)))))
  ;; f64.sub
  (func (export "op17") (param i64 i64) (result i64) (i64.reinterpret_f64 (f64.sub (f64.reinterpret_i64 (local.get 0)) (f64.reinterpret_i64 (local.get 1)))))
  ;; f64.mul
  (func (export "op18") (param i64 i64) (result i64) (i64.reinterpret_f64 (f64.mul (f64.reinterpret_i64 (local.get 0)) (f64.reinterpret_i64 (local.get 1)))))
  ;; f64.div
  (func (export "op19") (param i64 i64) (result i64) (i64.reinterpret_f64 (f64.div (f64.reinterpret_i64 (local.get 0)) (f64.reinterpret_i64 (local.get 1)))))
  ;; f64.min
  (func (export "op20") (param i64 i64) (result i64) (i64.reinterpret_f64 (f64.min (f64.reinterpret_i64 (local.get 0)) (f64.reinterpret_i64 (local.get 1)))))
  ;; f64.max
  (func (export "op21") (param i64 i64) (result i64) (i64.reinterpret_f64 (f64.max (f64.reinterpret_i64 (local.get 0)) (f64.reinterpret_i64 (local.get 1)))))
  ;; f64.copysign
  (func (export "op22") (param i64 i64) (result i64) (i64.reinterpret_f64 (f64.copysign (f64.reinterpret_i64 (local.get 0)) (f64.reinterpret_i64 (local.get 1)))))
  ;; f32.add
  (func (export "op23") (param i64 i64) (result i64) (i64.extend_i32_u (i32.reinterpret_f32 (f32.add (f32.reinterpret_i32 (i32.wrap_i64 (local.get 0))) (f32.reinterpret_i32 (i32.wrap_i64 (local.get 1)))))))
  ;; f32.sub
  (func (export "op24") (param i64 i64) (result i64) (i64.extend_i32_u (i32.reinterpret_f32 (f32.sub (f32.reinterpret_i32 (i32.wrap_i64 (local.get 0))) (f32.reinterpret_i32 (i32.wrap_i64 (local.get 1)))))))
  ;; f32.mul
  (func (export "op25") (param i64 i64) (result i64) (i64.extend_i32_u (i32.reinterpret_f32 (f32.mul (f32.reinterpret_i32 (i32.wrap_i64 (local.get 0))) (f32.reinterpret_i32 (i32.wrap_i64 (local.get 1)))))))
  ;; f32.div
  (func (export "op26") (param i64 i64) (result i64) (i64.extend_i32_u (i32.reinterpret_f32 (f32.div (f32.reinterpret_i32 (i32.wrap_i64 (local.get 0))) (f32.reinterpret_i32 (i32.wrap_i64 (local.get 1)))))))
  ;; f32.min
  (func (export "op27") (param i64 i64) (result i64) (i64.extend_i32_u (i32.reinterpret_f32 (f32.min (f32.reinterpret_i32 (i32.wrap_i64 (local.get 0))) (f32.reinterpret_i32 (i32.wrap_i64 (local.get 1)))))))
  ;; f32.max
  (func (export "op28") (param i64 i64) (result i64) (i64.extend_i32_u (i32.reinterpret_f32 (f32.max (f32.reinterpret_i32 (i32.wrap_i64 (local.get 0))) (f32.reinterpret_i32 (i32.wrap_i64 (local.get 1)))))))
  ;; f32.copysign
  (func (export "op29") (param i64 i64) (result i64) (i64.extend_i32_u (i32.reinterpret_f32 (f32.copysign (f32.reinterpret_i32 (i32.wrap_i64 (local.get 0))) (f32.reinterpret_i32 (i32.wrap_i64 (local.get 1)))))))
  ;; i32.trunc_f32_s
  (func (export "op30") (param i64 i64) (result i64) (i64.extend_i32_u (i32.trunc_f32_s (f32.reinterpret_i32 (i32.wrap_i64 (local.get 0))))))
  ;; i32.trunc_f32_u
  (func (export "op31") (param i64 i64) (result i64) (i64.extend_i32_u (i32.trunc_f32_u (f32.reinterpret_i32 (i32.wrap_i64 (local.get 0))))))
  ;; i32.trunc_f64_s
  (func (export "op32") (param i64 i64) (result i64) (i64.extend_i32_u (i32.trunc_f64_s (f64.reinterpret_i64 (local.get 0)))))
  ;; i32.trunc_f64_u
  (func (export "op33") (param i64 i64) (result i64) (i64.extend_i32_u (i32.trunc_f64_u (f64.reinterpret_i64 (local.get 0)))))
  ;; i64.trunc_f32_s
  (func (export "op34") (param i64 i64) (result i64) (i64.trunc_f32_s (f32.reinterpret_i32 (i32.wrap_i64 (local.get 0)))))
  ;; i64.trunc_f32_u
  (func (export "op35") (param i64 i64) (result i64) (i64.trunc_f32_u (f32.reinterpret_i32 (i32.wrap_i64 (local.get 0)))))
  ;; i64.trunc_f64_s
  (func (export "op36") (param i64 i64) (result i64) (i64.trunc_f64_s (f64.reinterpret_i64 (local.get 0))))
  ;; i64.trunc_f64_u
  (func (export "op37") (param i64 i64) (result i64) (i64.trunc_f64_u (f64.reinterpret_i64 (local.get 0))))
  ;; i32.trunc_sat_f32_s
  (func (export "op38") (param i64 i64) (result i64) (i64.extend_i32_u (i32.trunc_sat_f32_s (f32.reinterpret_i32 (i32.wrap_i64 (local.get 0))))))
  ;; i32.trunc_sat_f32_u
  (func (export "op39") (param i64 i64) (result i64) (i64.extend_i32_u (i32.trunc_sat_f32_u (f32.reinterpret_i32 (i32.wrap_i64 (local.get 0))))))
  ;; i32.trunc_sat_f64_s
  (func (export "op40") (param i64 i64) (result i64) (i64.extend_i32_u (i32.trunc_sat_f64_s (f64.reinterpret_i64 (local.get 0)))))
  ;; i32.trunc_sat_f64_u
  (func (export "op41") (param i64 i64) (result i64) (i64.extend_i32_u (i32.trunc_sat_f64_u (f64.reinterpret_i64 (local.get 0)))))
  ;; i64.trunc_sat_f32_s
  (func (export "op42") (param i64 i64) (result i64) (i64.trunc_sat_f32_s (f32.reinterpret_i32 (i32.wrap_i64 (local.get 0)))))
  ;; i64.trunc_sat_f32_u
  (func (export "op43") (param i64 i64) (result i64) (i64.trunc_sat_f32_u (f32.reinterpret_i32 (i32.wrap_i64 (local.get 0)))))
  ;; i64.trunc_sat_f64_s
  (func (export "op44") (param i64 i64) (result i64) (i64.trunc_sat_f64_s (f64.reinterpret_i64 (local.get 0))))
  ;; i64.trunc_sat_f64_u
  (func (export "op45") (param i64 i64) (result i64) (i64.trunc_sat_f64_u (f64.reinterpret_i64 (local.get 0))))
  ;; f32.convert_i32_s
  (func (export "op46") (param i64 i64) (result i64) (i64.extend_i32_u (i32.reinterpret_f32 (f32.convert_i32_s (i32.wrap_i64 (local.get 0))))))
  ;; f32.convert_i32_u
  (func (export "op47") (param i64 i64) (result i64) (i64.extend_i32_u (i32.reinterpret_f32 (f32.convert_i32_u (i32.wrap_i64 (local.get 0))))))
  ;; f32.convert_i64_s
  (func (export "op48") (param i64 i64) (result i64) (i64.extend_i32_u (i32.reinterpret_f32 (f32.convert_i64_s (local.get 0)))))
  ;; f32.convert_i64_u
  (func (export "op49") (param i64 i64) (result i64) (i64.extend_i32_u (i32.reinterpret_f32 (f32.convert_i64_u (local.get 0)))))
  ;; f64.convert_i32_s
  (func (export "op50") (param i64 i64) (result i64) (i64.reinterpret_f64 (f64.convert_i32_s (i32.wrap_i64 (local.get 0)))))
  ;; f64.convert_i32_u
  (func (export "op51") (param i64 i64) (result i64) (i64.reinterpret_f64 (f64.convert_i32_u (i32.wrap_i64 (local.get 0)))))
  ;; f64.convert_i64_s
  (func (export "op52") (param i64 i64) (result i64) (i64.reinterpret_f64 (f64.convert_i64_s (local.get 0))))
  ;; f64.convert_i64_u
  (func (export "op53") (param i64 i64) (result i64) (i64.reinterpret_f64 (f64.convert_i64_u (local.get 0))))
  ;; f64.chain_mul_add
  (func (export "op54") (param i64 i64) (result i64) (i64.reinterpret_f64 (f64.add (f64.mul (f64.reinterpret_i64 (local.get 0)) (f64.reinterpret_i64 (local.get 1))) (f64.const -1))))
  ;; f64.chain_mul_sub
  (func (export "op55") (param i64 i64) (result i64) (i64.reinterpret_f64 (f64.sub (f64.const 1) (f64.mul (f64.reinterpret_i64 (local.get 0)) (f64.reinterpret_i64 (local.get 1))))))
  ;; f64.chain_neg_mul_add
  (func (export "op56") (param i64 i64) (result i64) (i64.reinterpret_f64 (f64.add (f64.neg (f64.mul (f64.reinterpret_i64 (local.get 0)) (f64.reinterpret_i64 (local.get 1)))) (f64.const 1))))
  ;; f64.chain_div_sqrt
  (func (export "op57") (param i64 i64) (result i64) (i64.reinterpret_f64 (f64.sqrt (f64.div (f64.reinterpret_i64 (local.get 0)) (f64.reinterpret_i64 (local.get 1))))))
  ;; f64.chain_min_add
  (func (export "op58") (param i64 i64) (result i64) (i64.reinterpret_f64 (f64.min (f64.add (f64.reinterpret_i64 (local.get 0)) (f64.reinterpret_i64 (local.get 1))) (f64.const 0))))
  ;; f64.chain_add_neg
  (func (export "op59") (param i64 i64) (result i64) (i64.reinterpret_f64 (f64.neg (f64.add (f64.reinterpret_i64 (local.get 0)) (f64.reinterpret_i64 (local.get 1))))))
  ;; f64.chain_add_abs
  (func (export "op60") (param i64 i64) (result i64) (i64.reinterpret_f64 (f64.abs (f64.add (f64.reinterpret_i64 (local.get 0)) (f64.reinterpret_i64 (local.get 1))))))
  ;; f64.chain_add_copysign
  (func (export "op61") (param i64 i64) (result i64) (i64.reinterpret_f64 (f64.copysign (f64.add (f64.reinterpret_i64 (local.get 0)) (f64.reinterpret_i64 (local.get 1))) (f64.reinterpret_i64 (local.get 1)))))
  ;; f64.chain_add_floor
  (func (export "op62") (param i64 i64) (result i64) (i64.reinterpret_f64 (f64.floor (f64.add (f64.reinterpret_i64 (local.get 0)) (f64.reinterpret_i64 (local.get 1))))))
  ;; f32.chain_mul_add
  (func (export "op63") (param i64 i64) (result i64) (i64.extend_i32_u (i32.reinterpret_f32 (f32.add (f32.mul (f32.reinterpret_i32 (i32.wrap_i64 (local.get 0))) (f32.reinterpret_i32 (i32.wrap_i64 (local.get 1)))) (f32.const -1)))))
  ;; f32.chain_mul_sub
  (func (export "op64") (param i64 i64) (result i64) (i64.extend_i32_u (i32.reinterpret_f32 (f32.sub (f32.const 1) (f32.mul (f32.reinterpret_i32 (i32.wrap_i64 (local.get 0))) (f32.reinterpret_i32 (i32.wrap_i64 (local.get 1))))))))
  ;; f32.chain_add_neg
  (func (export "op65") (param i64 i64) (result i64) (i64.extend_i32_u (i32.reinterpret_f32 (f32.neg (f32.add (f32.reinterpret_i32 (i32.wrap_i64 (local.get 0))) (f32.reinterpret_i32 (i32.wrap_i64 (local.get 1))))))))
  ;; f32.chain_add_abs
  (func (export "op66") (param i64 i64) (result i64) (i64.extend_i32_u (i32.reinterpret_f32 (f32.abs (f32.add (f32.reinterpret_i32 (i32.wrap_i64 (local.get 0))) (f32.reinterpret_i32 (i32.wrap_i64 (local.get 1))))))))
  ;; f32.chain_demote_add
  (func (export "op67") (param i64 i64) (result i64) (i64.extend_i32_u (i32.reinterpret_f32 (f32.add (f32.demote_f64 (f64.mul (f64.reinterpret_i64 (local.get 0)) (f64.reinterpret_i64 (local.get 1)))) (f32.const 0)))))
  ;; f64.chain_promote_add
  (func (export "op68") (param i64 i64) (result i64) (i64.reinterpret_f64 (f64.add (f64.promote_f32 (f32.mul (f32.reinterpret_i32 (i32.wrap_i64 (local.get 0))) (f32.reinterpret_i32 (i32.wrap_i64 (local.get 1))))) (f64.const -1))))
)
