// The macros of musl's internal features.h that its libm sources (libm/)
// use. See libm/COPYRIGHT.

#pragma once

#define hidden __attribute__((__visibility__("hidden")))
#define weak_alias(old, new) \
	extern __typeof(old) new __attribute__((__weak__, __alias__(#old)))
