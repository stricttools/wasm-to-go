// The one function of musl's src/internal/atomic.h that fma.c uses.
// See COPYRIGHT.

#pragma once

#include <stdint.h>

static inline int a_clz_64(uint64_t x)
{
	return __builtin_clzll(x);
}
