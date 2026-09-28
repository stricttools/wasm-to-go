// Exports every libc-gen math function with its operands and results as raw
// bits, so that NaN payloads and signs reach the test unchanged.

#include <math.h>
#include <stdint.h>

#define D(b) __builtin_bit_cast(double, (int64_t)(b))
#define B(x) __builtin_bit_cast(int64_t, (double)(x))
#define EXPORT(name) __attribute__((export_name(#name))) int64_t name

#define F1(f) EXPORT(f##_)(int64_t a) { return B((f)(D(a))); }
#define F2(f) EXPORT(f##_)(int64_t a, int64_t b) { return B((f)(D(a), D(b))); }

F1(acos) F1(acosh) F1(asin) F1(asinh) F1(atan) F1(atanh) F1(cbrt) F1(ceil)
F1(cos) F1(cosh) F1(erf) F1(erfc) F1(exp) F1(exp2) F1(expm1) F1(fabs)
F1(floor) F1(j0) F1(j1) F1(lgamma) F1(log) F1(log10) F1(log1p) F1(log2)
F1(logb) F1(rint) F1(round) F1(roundeven) F1(sin) F1(sinh) F1(sqrt) F1(tan)
F1(tanh) F1(tgamma) F1(trunc) F1(y0) F1(y1)

F2(atan2) F2(copysign) F2(fdim) F2(fmax) F2(fmin) F2(fmod) F2(hypot)
F2(nextafter) F2(pow) F2(remainder)

EXPORT(fma_)(int64_t a, int64_t b, int64_t c) { return B(fma(D(a), D(b), D(c))); }
EXPORT(ilogb_)(int64_t a) { return ilogb(D(a)); }
EXPORT(ldexp_)(int64_t a, int64_t n) { return B(ldexp(D(a), (int)n)); }
EXPORT(jn_)(int64_t n, int64_t a) { return B(jn((int)n, D(a))); }
EXPORT(yn_)(int64_t n, int64_t a) { return B(yn((int)n, D(a))); }
EXPORT(lrint_)(int64_t a) { return lrint(D(a)); }
EXPORT(llrint_)(int64_t a) { return llrint(D(a)); }

// frexp, modf, and lgamma_r have a second result, which the _2 functions
// return (frexp leaves it unset for infinities and NaNs, so it starts at 0).
EXPORT(frexp_)(int64_t a) { int e = 0; return B(frexp(D(a), &e)); }
EXPORT(frexp_2)(int64_t a) { int e = 0; frexp(D(a), &e); return e; }
EXPORT(modf_)(int64_t a) { double i = 0; return B(modf(D(a), &i)); }
EXPORT(modf_2)(int64_t a) { double i = 0; modf(D(a), &i); return B(i); }
EXPORT(lgamma_r_)(int64_t a) { int s = 0; return B(lgamma_r(D(a), &s)); }
EXPORT(lgamma_r_2)(int64_t a) { int s = 0; lgamma_r(D(a), &s); return s; }
