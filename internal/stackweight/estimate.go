package stackweight

// EstimateVersion names the frame estimate; the custom section records it,
// so a module says which estimate its charges came from.
const EstimateVersion = "max(272 + 2 per slot, 48 + 8 per slot) + 8 per stack parameter"

// estimate is a function's estimated native frame in bytes, from its slots
// (parameters and declared locals): the larger of 272 plus 2 per slot and
// 48 plus 8 per slot, plus 8 for each parameter an x86-64 engine passes on
// the stack (V8 passes five integer and six floating-point parameters in
// registers).
//
// Over every function on a cycle of QuickJS-ng's call graph, the frame of
// either of V8's compilers (Liftoff and TurboFan, in Node 22's V8 12.4) is at
// most 0.975 of the estimate. Among the estimates that bound every frame, a
// large constant per frame lets recursion whose frames are small and keep no
// shadow stack (deeply nested JSON, arrays walked by flat) and recursion
// through the interpreter (whose frames also take shadow stack) reach
// similar native stacks at the module's limit, which lets the second go
// deepest for a given worst case; 8 per slot keeps the estimate growing
// with a frame's slots past the functions it was fitted to, as compiled
// frames do. The README's section on the stack-weight pass gives the
// measurements.
func estimate(ft funcType, locals []byte) int64 {
	gp, fp := 0, 0
	for _, t := range ft.params {
		switch t {
		case f32, f64:
			fp++
		default:
			gp++
		}
	}
	stackParams := int64(max(gp-5, 0) + max(fp-6, 0))
	n := int64(len(ft.params) + len(locals))
	return max(272+2*n, 48+8*n) + 8*stackParams
}
