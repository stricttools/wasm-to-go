# Bugs found in software we do not own

Drafts to report upstream; nothing here is filed until the owner says so.

- [go-archsimd-min-commutative.md](go-archsimd-min-commutative.md): cmd/compile treats archsimd's Float32x4.Min and Max as commutative on amd64, though VMINPS returns its second operand for equal zeros and NaNs.
